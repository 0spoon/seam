package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT-related errors.
var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

// Claims represents the JWT claims for an access token.
type Claims struct {
	jwt.RegisteredClaims
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// JWTManager handles JWT creation and verification.
type JWTManager struct {
	secret         []byte
	accessTokenTTL time.Duration
}

// NewJWTManager creates a new JWTManager.
func NewJWTManager(secret string, accessTokenTTL time.Duration) *JWTManager {
	return &JWTManager{
		secret:         []byte(secret),
		accessTokenTTL: accessTokenTTL,
	}
}

// GenerateAccessToken creates a signed JWT access token for the given user.
func (m *JWTManager) GenerateAccessToken(userID, username string) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "seam",
		},
		UserID:   userID,
		Username: username,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("auth.JWTManager.GenerateAccessToken: %w", err)
	}
	return signed, nil
}

// VerifyAccessToken parses and validates a JWT access token, returning the claims.
func (m *JWTManager) VerifyAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("auth.JWTManager.VerifyAccessToken: %w", ErrTokenExpired)
		}
		return nil, fmt.Errorf("auth.JWTManager.VerifyAccessToken: %w", ErrTokenInvalid)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("auth.JWTManager.VerifyAccessToken: %w", ErrTokenInvalid)
	}

	return claims, nil
}
