// Package auth handles user accounts, JWT tokens, and authentication.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	ErrValidation         = errors.New("validation error")
	// ErrRegistrationClosed is returned by Register when an owner already
	// exists. Seam is a single-user system; only the first registration
	// succeeds, after which the endpoint is closed.
	ErrRegistrationClosed = errors.New("registration is closed")
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

// Store defines data access methods for the owner account and refresh tokens.
type Store interface {
	CreateUser(ctx context.Context, u *User) error
	CountOwners(ctx context.Context) (int, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	UpdateUserPassword(ctx context.Context, id, passwordHash string) error
	UpdateUserEmail(ctx context.Context, id, email string) error

	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	GetRefreshToken(ctx context.Context, tokenHash string) (userID string, expiresAt time.Time, err error)
	ConsumeRefreshToken(ctx context.Context, tokenHash string) (userID string, expiresAt time.Time, err error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	DeleteRefreshTokensByUser(ctx context.Context, userID string) error
	DeleteOldestTokensForUser(ctx context.Context, userID string, maxTokens int) error
	DeleteExpiredTokens(ctx context.Context) error
}

// SQLStore implements Store against a SQLite database.
type SQLStore struct {
	db *sql.DB
}

// NewSQLStore creates a new SQLStore backed by the given database.
func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db}
}

// CreateUser inserts the owner record. Returns ErrUserExists if the username
// or email already exists.
func (s *SQLStore) CreateUser(ctx context.Context, u *User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO owner (id, username, email, password, created_at, updated_at)
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

// CountOwners returns the number of rows in the owner table. Used to
// gate registration in the single-user system: once an owner exists,
// new registrations are rejected.
func (s *SQLStore) CountOwners(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM owner`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("auth.Store.CountOwners: %w", err)
	}
	return n, nil
}

// GetUserByUsername retrieves a user by username. Returns ErrNotFound if
// no user matches.
func (s *SQLStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, created_at, updated_at
		 FROM owner WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Password, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("auth.Store.GetUserByUsername: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("auth.Store.GetUserByUsername: %w", err)
	}
	var parseErr error
	u.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		slog.Warn("auth.Store.GetUserByUsername: malformed created_at, using zero time",
			"username", username, "raw", createdAt, "error", parseErr)
	}
	u.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
	if parseErr != nil {
		slog.Warn("auth.Store.GetUserByUsername: malformed updated_at, using zero time",
			"username", username, "raw", updatedAt, "error", parseErr)
	}
	return u, nil
}

// GetUserByID retrieves a user by ID. Returns ErrNotFound if no user matches.
func (s *SQLStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, created_at, updated_at
		 FROM owner WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Password, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("auth.Store.GetUserByID: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("auth.Store.GetUserByID: %w", err)
	}
	var parseErr error
	u.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		slog.Warn("auth.Store.GetUserByID: malformed created_at, using zero time",
			"user_id", id, "raw", createdAt, "error", parseErr)
	}
	u.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
	if parseErr != nil {
		slog.Warn("auth.Store.GetUserByID: malformed updated_at, using zero time",
			"user_id", id, "raw", updatedAt, "error", parseErr)
	}
	return u, nil
}

// UpdateUserPassword updates the password hash for a user.
func (s *SQLStore) UpdateUserPassword(ctx context.Context, id, passwordHash string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE owner SET password = ?, updated_at = ? WHERE id = ?`,
		passwordHash, now, id,
	)
	if err != nil {
		return fmt.Errorf("auth.Store.UpdateUserPassword: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth.Store.UpdateUserPassword: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("auth.Store.UpdateUserPassword: %w", ErrNotFound)
	}
	return nil
}

// UpdateUserEmail updates the email for a user.
func (s *SQLStore) UpdateUserEmail(ctx context.Context, id, email string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE owner SET email = ?, updated_at = ? WHERE id = ?`,
		email, now, id,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("auth.Store.UpdateUserEmail: %w", ErrUserExists)
		}
		return fmt.Errorf("auth.Store.UpdateUserEmail: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth.Store.UpdateUserEmail: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("auth.Store.UpdateUserEmail: %w", ErrNotFound)
	}
	return nil
}

// CreateRefreshToken stores a hashed refresh token.
func (s *SQLStore) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	idVal, idErr := ulid.New(ulid.Now(), rand.Reader)
	if idErr != nil {
		return fmt.Errorf("auth.Store.CreateRefreshToken: generate id: %w", idErr)
	}
	id := idVal.String()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, owner_id, token_hash, expires_at, created_at)
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
		`SELECT owner_id, expires_at FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&userID, &expiresAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", time.Time{}, fmt.Errorf("auth.Store.GetRefreshToken: %w", ErrNotFound)
		}
		return "", time.Time{}, fmt.Errorf("auth.Store.GetRefreshToken: %w", err)
	}
	expiresAt, parseErr := time.Parse(time.RFC3339, expiresAtStr)
	if parseErr != nil {
		slog.Warn("auth.Store.GetRefreshToken: malformed expires_at, using zero time",
			"raw", expiresAtStr, "error", parseErr)
	}
	return userID, expiresAt, nil
}

// ConsumeRefreshToken atomically deletes a refresh token and returns its
// metadata. This prevents TOCTOU races in token rotation by combining the
// read and delete into a single operation. Returns ErrNotFound if the token
// does not exist (already consumed by a concurrent request).
func (s *SQLStore) ConsumeRefreshToken(ctx context.Context, tokenHash string) (string, time.Time, error) {
	// SQLite supports DELETE...RETURNING since 3.35.0 (2021-03-12).
	var userID, expiresAtStr string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM refresh_tokens WHERE token_hash = ? RETURNING owner_id, expires_at`,
		tokenHash,
	).Scan(&userID, &expiresAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", time.Time{}, fmt.Errorf("auth.Store.ConsumeRefreshToken: %w", ErrNotFound)
		}
		return "", time.Time{}, fmt.Errorf("auth.Store.ConsumeRefreshToken: %w", err)
	}
	expiresAt, parseErr := time.Parse(time.RFC3339, expiresAtStr)
	if parseErr != nil {
		slog.Warn("auth.Store.ConsumeRefreshToken: malformed expires_at, using zero time",
			"raw", expiresAtStr, "error", parseErr)
	}
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

// DeleteOldestTokensForUser removes the oldest refresh tokens for a user
// when the total count exceeds maxTokens.
func (s *SQLStore) DeleteOldestTokensForUser(ctx context.Context, userID string, maxTokens int) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE id IN (
			SELECT id FROM refresh_tokens
			WHERE owner_id = ?
			ORDER BY created_at DESC
			LIMIT -1 OFFSET ?
		)`, userID, maxTokens,
	)
	if err != nil {
		return fmt.Errorf("auth.Store.DeleteOldestTokensForUser: %w", err)
	}
	return nil
}

// DeleteExpiredTokens removes all refresh tokens that have passed their
// expiration time.
func (s *SQLStore) DeleteExpiredTokens(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < ?`, now,
	)
	if err != nil {
		return fmt.Errorf("auth.Store.DeleteExpiredTokens: %w", err)
	}
	return nil
}

// DeleteRefreshTokensByUser removes all refresh tokens for a user (logout all sessions).
func (s *SQLStore) DeleteRefreshTokensByUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE owner_id = ?`, userID,
	)
	if err != nil {
		return fmt.Errorf("auth.Store.DeleteRefreshTokensByUser: %w", err)
	}
	return nil
}

// OpenDB opens (or creates) the seam.db at the given path,
// sets WAL mode and foreign keys, and runs migrations.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("auth.OpenDB: open: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth.OpenDB: set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth.OpenDB: foreign keys: %w", err)
	}

	if err := migrations.Run(db, migrations.Migrations()); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth.OpenDB: migrations: %w", err)
	}

	// C-1: Limit open connections to 1 for SQLite. SQLite only supports a
	// single writer at a time; allowing the Go connection pool to open
	// multiple connections can lead to SQLITE_BUSY errors under contention.
	db.SetMaxOpenConns(1)

	return db, nil
}

// isUniqueConstraintError checks if a SQLite error is a UNIQUE constraint violation.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
