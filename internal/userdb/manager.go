// Package userdb manages per-user SQLite database lifecycle: open, cache,
// migrate, and evict idle databases.
package userdb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/katata/seam/internal/validate"
	"github.com/katata/seam/migrations"
	_ "modernc.org/sqlite"
)

// Manager defines the interface for managing per-user SQLite databases.
type Manager interface {
	// Open returns a *sql.DB for the given user, creating the DB
	// and running migrations if it does not exist. Caches open handles.
	Open(ctx context.Context, userID string) (*sql.DB, error)

	// Close closes the DB for a specific user.
	Close(userID string) error

	// CloseAll closes all open databases (graceful shutdown).
	CloseAll() error

	// UserNotesDir returns the absolute path to a user's notes/ directory.
	UserNotesDir(userID string) string

	// UserDataDir returns the absolute path to a user's data directory.
	UserDataDir(userID string) string

	// ListUsers returns the IDs of all users who have a data directory.
	ListUsers(ctx context.Context) ([]string, error)

	// EnsureUserDirs creates the directory tree for a user if it does not exist.
	EnsureUserDirs(userID string) error
}

type dbEntry struct {
	db       *sql.DB
	lastUsed time.Time
}

// SQLManager implements Manager using the filesystem and SQLite.
type SQLManager struct {
	dataDir         string
	evictionTimeout time.Duration
	logger          *slog.Logger

	mu      sync.Mutex
	dbs     map[string]*dbEntry
	closeCh chan struct{}
}

// NewSQLManager creates a new SQLManager.
// Call Run() in a goroutine to block until shutdown.
func NewSQLManager(dataDir string, evictionTimeout time.Duration, logger *slog.Logger) *SQLManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLManager{
		dataDir:         dataDir,
		evictionTimeout: evictionTimeout,
		logger:          logger,
		dbs:             make(map[string]*dbEntry),
		closeCh:         make(chan struct{}),
	}
}

// Open returns a cached or newly created *sql.DB for the given user.
// A-2: Validates userID to prevent path traversal via crafted IDs.
func (m *SQLManager) Open(ctx context.Context, userID string) (*sql.DB, error) {
	if err := validate.UserID(userID); err != nil {
		return nil, fmt.Errorf("userdb.Manager.Open: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.dbs[userID]; ok {
		entry.lastUsed = time.Now()
		return entry.db, nil
	}

	db, err := m.openDB(userID)
	if err != nil {
		return nil, err
	}

	m.dbs[userID] = &dbEntry{db: db, lastUsed: time.Now()}
	return db, nil
}

// Close closes the database for a specific user and removes it from the cache.
func (m *SQLManager) Close(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.dbs[userID]
	if !ok {
		return nil
	}
	delete(m.dbs, userID)
	return entry.db.Close()
}

// CloseAll closes all cached databases.
func (m *SQLManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Signal eviction loop to stop.
	select {
	case <-m.closeCh:
		// Already closed.
	default:
		close(m.closeCh)
	}

	var firstErr error
	for userID, entry := range m.dbs {
		if err := entry.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(m.dbs, userID)
	}
	return firstErr
}

// UserNotesDir returns the absolute path to a user's notes/ directory.
// Callers must validate userID before calling this method, or use
// ValidatedUserNotesDir which returns an error.
func (m *SQLManager) UserNotesDir(userID string) string {
	return filepath.Join(m.dataDir, "users", userID, "notes")
}

// UserDataDir returns the absolute path to a user's data directory.
// Callers must validate userID before calling this method, or use
// ValidatedUserDataDir which returns an error.
func (m *SQLManager) UserDataDir(userID string) string {
	return filepath.Join(m.dataDir, "users", userID)
}

// ListUsers returns the IDs of all users who have a data directory.
func (m *SQLManager) ListUsers(ctx context.Context) ([]string, error) {
	usersDir := filepath.Join(m.dataDir, "users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("userdb.Manager.ListUsers: %w", err)
	}

	var users []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Validate that directory names are reasonable user IDs:
		// not empty, no path traversal characters.
		if name == "" || name == "." || name == ".." ||
			strings.ContainsAny(name, "/\\\x00") {
			continue
		}
		users = append(users, name)
	}
	return users, nil
}

// EnsureUserDirs creates the directory tree for a user if it does not exist.
// A-2: Validates userID to prevent path traversal via crafted IDs.
func (m *SQLManager) EnsureUserDirs(userID string) error {
	if err := validate.UserID(userID); err != nil {
		return fmt.Errorf("userdb.Manager.EnsureUserDirs: %w", err)
	}

	notesDir := m.UserNotesDir(userID)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return fmt.Errorf("userdb.Manager.EnsureUserDirs: %w", err)
	}
	// C-3: Also create notes/inbox/ subdirectory per TEST_PLAN.md spec.
	inboxDir := filepath.Join(notesDir, "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return fmt.Errorf("userdb.Manager.EnsureUserDirs: create inbox: %w", err)
	}
	return nil
}

// Run blocks until the context is cancelled or CloseAll is called.
// Previously this ran an eviction loop, but eviction was removed to avoid
// closing a *sql.DB handle while other goroutines still hold references.
// Databases are now only closed on shutdown via CloseAll.
func (m *SQLManager) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-m.closeCh:
	}
}

// openDB creates and configures a new SQLite database for a user.
func (m *SQLManager) openDB(userID string) (*sql.DB, error) {
	if err := m.EnsureUserDirs(userID); err != nil {
		return nil, fmt.Errorf("userdb.Manager.Open: %w", err)
	}

	dbPath := filepath.Join(m.UserDataDir(userID), "seam.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("userdb.Manager.Open: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("userdb.Manager.Open: set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("userdb.Manager.Open: foreign keys: %w", err)
	}

	if err := migrations.Run(db, migrations.UserMigrations()); err != nil {
		db.Close()
		return nil, fmt.Errorf("userdb.Manager.Open: migrations: %w", err)
	}

	// C-1: Limit open connections to 1 for SQLite. SQLite only supports a
	// single writer at a time; allowing the Go connection pool to open
	// multiple connections can lead to SQLITE_BUSY errors under contention.
	db.SetMaxOpenConns(1)

	return db, nil
}
