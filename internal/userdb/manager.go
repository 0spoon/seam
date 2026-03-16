// Package userdb manages the application SQLite database and data directory.
package userdb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/katata/seam/migrations"
	_ "modernc.org/sqlite"
)

// Manager defines the interface for managing the application database.
// The userID parameter is accepted for interface compatibility but is
// ignored in the single-user implementation -- all calls return the
// same database and data paths.
type Manager interface {
	// Open returns the application *sql.DB. The userID parameter is
	// ignored (single-user system).
	Open(ctx context.Context, userID string) (*sql.DB, error)

	// Close is a no-op in the single-user implementation.
	Close(userID string) error

	// CloseAll closes the database (graceful shutdown).
	CloseAll() error

	// UserNotesDir returns the absolute path to the notes/ directory.
	// The userID parameter is ignored.
	UserNotesDir(userID string) string

	// UserDataDir returns the absolute path to the data directory.
	// The userID parameter is ignored.
	UserDataDir(userID string) string

	// ListUsers returns a single-element slice for the single-user system.
	ListUsers(ctx context.Context) ([]string, error)

	// EnsureUserDirs creates the notes directory tree if it does not exist.
	// The userID parameter is ignored.
	EnsureUserDirs(userID string) error
}

// DefaultUserID is the constant user ID used in the single-user system.
// Domain services receive this value from JWT middleware via reqctx.
const DefaultUserID = "default"

// SQLManager implements Manager for a single SQLite database with a
// flat data directory layout.
type SQLManager struct {
	dataDir string
	logger  *slog.Logger

	mu      sync.Mutex
	db      *sql.DB
	closeCh chan struct{}
}

// NewSQLManagerWithDB creates a Manager backed by a pre-opened database.
// The dataDir is the root data directory (notes live at dataDir/notes/).
func NewSQLManagerWithDB(db *sql.DB, dataDir string, logger *slog.Logger) *SQLManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLManager{
		dataDir: dataDir,
		logger:  logger,
		db:      db,
		closeCh: make(chan struct{}),
	}
}

// NewSQLManager creates a Manager that opens the database at dataDir/seam.db.
// This constructor is used by tests and the seed command. For the server,
// prefer NewSQLManagerWithDB to share the DB handle with the auth store.
func NewSQLManager(dataDir string, logger *slog.Logger) *SQLManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLManager{
		dataDir: dataDir,
		logger:  logger,
		closeCh: make(chan struct{}),
	}
}

// Open returns the single application database. The userID parameter is
// ignored. If the database was not provided via NewSQLManagerWithDB, it
// is lazily opened on the first call.
func (m *SQLManager) Open(ctx context.Context, userID string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.db != nil {
		return m.db, nil
	}

	db, err := m.openDB()
	if err != nil {
		return nil, err
	}
	m.db = db
	return db, nil
}

// Close is a no-op in the single-user implementation. The database is
// closed via CloseAll at shutdown.
func (m *SQLManager) Close(userID string) error {
	return nil
}

// CloseAll closes the database handle.
func (m *SQLManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	select {
	case <-m.closeCh:
		// Already closed.
	default:
		close(m.closeCh)
	}

	if m.db != nil {
		err := m.db.Close()
		m.db = nil
		return err
	}
	return nil
}

// UserNotesDir returns the absolute path to the notes/ directory.
// The userID parameter is ignored.
func (m *SQLManager) UserNotesDir(userID string) string {
	return filepath.Join(m.dataDir, "notes")
}

// UserDataDir returns the absolute path to the data directory.
// The userID parameter is ignored.
func (m *SQLManager) UserDataDir(userID string) string {
	return m.dataDir
}

// ListUsers returns a single-element slice containing DefaultUserID.
// This satisfies callers that iterate over users (e.g., AI queue,
// watcher reconciliation).
func (m *SQLManager) ListUsers(ctx context.Context) ([]string, error) {
	return []string{DefaultUserID}, nil
}

// EnsureUserDirs creates the notes directory tree if it does not exist.
// The userID parameter is ignored.
func (m *SQLManager) EnsureUserDirs(userID string) error {
	notesDir := m.UserNotesDir(userID)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return fmt.Errorf("userdb.Manager.EnsureUserDirs: %w", err)
	}
	inboxDir := filepath.Join(notesDir, "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return fmt.Errorf("userdb.Manager.EnsureUserDirs: create inbox: %w", err)
	}
	return nil
}

// Run blocks until the context is cancelled or CloseAll is called.
func (m *SQLManager) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-m.closeCh:
	}
}

// openDB creates and configures a new SQLite database.
func (m *SQLManager) openDB() (*sql.DB, error) {
	if err := m.EnsureUserDirs(""); err != nil {
		return nil, fmt.Errorf("userdb.Manager.Open: %w", err)
	}

	dbPath := filepath.Join(m.dataDir, "seam.db")
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

	if err := migrations.Run(db, migrations.Migrations()); err != nil {
		db.Close()
		return nil, fmt.Errorf("userdb.Manager.Open: migrations: %w", err)
	}

	db.SetMaxOpenConns(1)

	return db, nil
}
