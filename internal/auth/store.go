// Package auth handles user accounts, JWT tokens, and authentication.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"

	"github.com/katata/seam/migrations"
)

// Domain errors.
var (
	ErrUserExists         = errors.New("user already exists")
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// User represents a registered user.
type User struct {
	ID        string
	Username  string
	Email     string
	Password  string // bcrypt hash
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store defines data access methods for users and refresh tokens
// against server.db.
type Store interface {
	CreateUser(ctx context.Context, u *User) error
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)

	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	GetRefreshToken(ctx context.Context, tokenHash string) (userID string, expiresAt time.Time, err error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	DeleteRefreshTokensByUser(ctx context.Context, userID string) error
}

// SQLStore implements Store against a SQLite database.
type SQLStore struct {
	db *sql.DB
}

// NewSQLStore creates a new SQLStore backed by the given database.
func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db}
}

// CreateUser inserts a new user. Returns ErrUserExists if the username
// or email already exists.
func (s *SQLStore) CreateUser(ctx context.Context, u *User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.Email, u.Password,
		u.CreatedAt.Format(time.RFC3339), u.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("auth.Store.CreateUser: %w", ErrUserExists)
		}
		return fmt.Errorf("auth.Store.CreateUser: %w", err)
	}
	return nil
}

// GetUserByUsername retrieves a user by username. Returns ErrNotFound if
// no user matches.
func (s *SQLStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, created_at, updated_at
		 FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Password, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("auth.Store.GetUserByUsername: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("auth.Store.GetUserByUsername: %w", err)
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return u, nil
}

// GetUserByID retrieves a user by ID. Returns ErrNotFound if no user matches.
func (s *SQLStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Password, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("auth.Store.GetUserByID: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("auth.Store.GetUserByID: %w", err)
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return u, nil
}

// CreateRefreshToken stores a hashed refresh token.
func (s *SQLStore) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, userID, tokenHash,
		expiresAt.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("auth.Store.CreateRefreshToken: %w", err)
	}
	return nil
}

// GetRefreshToken retrieves the user ID and expiration for a refresh token hash.
// Returns ErrNotFound if the token does not exist.
func (s *SQLStore) GetRefreshToken(ctx context.Context, tokenHash string) (string, time.Time, error) {
	var userID, expiresAtStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&userID, &expiresAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", time.Time{}, fmt.Errorf("auth.Store.GetRefreshToken: %w", ErrNotFound)
		}
		return "", time.Time{}, fmt.Errorf("auth.Store.GetRefreshToken: %w", err)
	}
	expiresAt, _ := time.Parse(time.RFC3339, expiresAtStr)
	return userID, expiresAt, nil
}

// DeleteRefreshToken removes a single refresh token by its hash.
func (s *SQLStore) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE token_hash = ?`, tokenHash,
	)
	if err != nil {
		return fmt.Errorf("auth.Store.DeleteRefreshToken: %w", err)
	}
	return nil
}

// DeleteRefreshTokensByUser removes all refresh tokens for a user (logout all sessions).
func (s *SQLStore) DeleteRefreshTokensByUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = ?`, userID,
	)
	if err != nil {
		return fmt.Errorf("auth.Store.DeleteRefreshTokensByUser: %w", err)
	}
	return nil
}

// OpenServerDB opens (or creates) the server.db at the given path,
// sets WAL mode and foreign keys, and runs migrations.
func OpenServerDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("auth.OpenServerDB: open: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth.OpenServerDB: set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth.OpenServerDB: foreign keys: %w", err)
	}

	if _, err := db.Exec(migrations.ServerSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth.OpenServerDB: migrations: %w", err)
	}

	return db, nil
}

// isUniqueConstraintError checks if a SQLite error is a UNIQUE constraint violation.
func isUniqueConstraintError(err error) bool {
	// modernc.org/sqlite returns errors containing "UNIQUE constraint failed"
	if err == nil {
		return false
	}
	msg := err.Error()
	for i := 0; i <= len(msg)-len("UNIQUE constraint failed"); i++ {
		if msg[i:i+len("UNIQUE constraint failed")] == "UNIQUE constraint failed" {
			return true
		}
	}
	return false
}
