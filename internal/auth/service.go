package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/katata/seam/internal/userdb"
)

// usernameRe validates usernames: 3-64 characters, alphanumeric, underscore, and hyphen only.
var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)

// Request/response types for the auth API.
type RegisterReq struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type AuthResponse struct {
	User   UserInfo  `json:"user"`
	Tokens TokenPair `json:"tokens"`
}

type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type RefreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutReq struct {
	RefreshToken string `json:"refresh_token"`
}

// Service handles registration, login, and token lifecycle.
type Service struct {
	store           Store
	jwtManager      *JWTManager
	userDBManager   userdb.Manager
	refreshTokenTTL time.Duration
	bcryptCost      int
	logger          *slog.Logger
}

// NewService creates a new auth Service.
func NewService(store Store, jwtManager *JWTManager, userDBManager userdb.Manager,
	refreshTokenTTL time.Duration, bcryptCost int, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:           store,
		jwtManager:      jwtManager,
		userDBManager:   userDBManager,
		refreshTokenTTL: refreshTokenTTL,
		bcryptCost:      bcryptCost,
		logger:          logger,
	}
}

// Register creates a new user account and returns an AuthResponse with tokens.
func (s *Service) Register(ctx context.Context, req RegisterReq) (*AuthResponse, error) {
	if req.Username == "" {
		return nil, fmt.Errorf("auth.Service.Register: username is required: %w", ErrInvalidCredentials)
	}
	if !usernameRe.MatchString(req.Username) {
		return nil, fmt.Errorf("auth.Service.Register: username must be 3-64 characters, alphanumeric/underscore/hyphen only: %w", ErrInvalidCredentials)
	}
	if req.Email == "" {
		return nil, fmt.Errorf("auth.Service.Register: email is required: %w", ErrInvalidCredentials)
	}
	if !isValidEmail(req.Email) {
		return nil, fmt.Errorf("auth.Service.Register: invalid email format: %w", ErrInvalidCredentials)
	}
	if req.Password == "" {
		return nil, fmt.Errorf("auth.Service.Register: password is required: %w", ErrInvalidCredentials)
	}
	if len(req.Password) < 8 {
		return nil, fmt.Errorf("auth.Service.Register: password must be at least 8 characters: %w", ErrInvalidCredentials)
	}
	if len(req.Password) > 1024 {
		return nil, fmt.Errorf("auth.Service.Register: password must not exceed 1024 characters: %w", ErrInvalidCredentials)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth.Service.Register: hash password: %w", err)
	}

	now := time.Now().UTC()
	user := &User{
		ID:        ulid.MustNew(ulid.Now(), rand.Reader).String(),
		Username:  req.Username,
		Email:     req.Email,
		Password:  string(hash),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("auth.Service.Register: %w", err)
	}

	// Create user data directory.
	if err := s.userDBManager.EnsureUserDirs(user.ID); err != nil {
		s.logger.Error("failed to create user data directory", "user_id", user.ID, "error", err)
		// Do not fail registration for this -- the directory will be created on first DB access.
	}

	tokens, err := s.generateTokenPair(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("auth.Service.Register: %w", err)
	}

	s.logger.Info("user registered", "user_id", user.ID, "username", user.Username)

	return &AuthResponse{
		User:   UserInfo{ID: user.ID, Username: user.Username, Email: user.Email},
		Tokens: *tokens,
	}, nil
}

// Login verifies credentials and returns an AuthResponse with tokens.
func (s *Service) Login(ctx context.Context, req LoginReq) (*AuthResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, fmt.Errorf("auth.Service.Login: %w", ErrInvalidCredentials)
	}

	user, err := s.store.GetUserByUsername(ctx, req.Username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("auth.Service.Login: %w", ErrInvalidCredentials)
		}
		return nil, fmt.Errorf("auth.Service.Login: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("auth.Service.Login: %w", ErrInvalidCredentials)
	}

	tokens, err := s.generateTokenPair(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("auth.Service.Login: %w", err)
	}

	s.logger.Info("user logged in", "user_id", user.ID, "username", user.Username)

	return &AuthResponse{
		User:   UserInfo{ID: user.ID, Username: user.Username, Email: user.Email},
		Tokens: *tokens,
	}, nil
}

// Refresh issues a new access token using a valid refresh token.
// The refresh token itself is NOT rotated.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("auth.Service.Refresh: %w", ErrInvalidCredentials)
	}

	tokenHash := hashToken(refreshToken)
	userID, expiresAt, err := s.store.GetRefreshToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("auth.Service.Refresh: %w", ErrInvalidCredentials)
		}
		return nil, fmt.Errorf("auth.Service.Refresh: %w", err)
	}

	if time.Now().UTC().After(expiresAt) {
		// Token expired -- delete it and reject.
		_ = s.store.DeleteRefreshToken(ctx, tokenHash)
		return nil, fmt.Errorf("auth.Service.Refresh: %w", ErrTokenExpired)
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth.Service.Refresh: %w", err)
	}

	accessToken, err := s.jwtManager.GenerateAccessToken(user.ID, user.Username)
	if err != nil {
		return nil, fmt.Errorf("auth.Service.Refresh: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken, // same refresh token
	}, nil
}

// Logout revokes a refresh token.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	tokenHash := hashToken(refreshToken)
	if err := s.store.DeleteRefreshToken(ctx, tokenHash); err != nil {
		return fmt.Errorf("auth.Service.Logout: %w", err)
	}
	return nil
}

// generateTokenPair creates an access token and a refresh token for the user.
func (s *Service) generateTokenPair(ctx context.Context, user *User) (*TokenPair, error) {
	accessToken, err := s.jwtManager.GenerateAccessToken(user.ID, user.Username)
	if err != nil {
		return nil, err
	}

	// Generate a cryptographically random refresh token.
	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshToken := hex.EncodeToString(refreshBytes)

	// Store the hash of the refresh token (not the raw token).
	tokenHash := hashToken(refreshToken)
	expiresAt := time.Now().UTC().Add(s.refreshTokenTTL)
	if err := s.store.CreateRefreshToken(ctx, user.ID, tokenHash, expiresAt); err != nil {
		return nil, err
	}

	// Cap the number of active refresh tokens per user to prevent
	// unbounded accumulation from repeated logins.
	const maxRefreshTokens = 10
	if err := s.store.DeleteOldestTokensForUser(ctx, user.ID, maxRefreshTokens); err != nil {
		s.logger.Warn("auth.Service.generateTokenPair: failed to prune old tokens",
			"user_id", user.ID, "error", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// hashToken returns the SHA-256 hex digest of a token string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// isValidEmail performs basic email format validation. It checks for the
// presence of exactly one "@" with non-empty local and domain parts, and
// at least one "." in the domain portion.
func isValidEmail(email string) bool {
	at := strings.LastIndex(email, "@")
	if at <= 0 || at >= len(email)-1 {
		return false
	}
	domain := email[at+1:]
	if !strings.Contains(domain, ".") {
		return false
	}
	// Reject domains ending or starting with a dot.
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	return true
}
