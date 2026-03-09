package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/auth"
)

func TestJWTManager_GenerateAndVerify(t *testing.T) {
	mgr := auth.NewJWTManager("test-secret", 15*time.Minute)

	token, err := mgr.GenerateAccessToken("user123", "alice")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := mgr.VerifyAccessToken(token)
	require.NoError(t, err)
	require.Equal(t, "user123", claims.UserID)
	require.Equal(t, "alice", claims.Username)
	require.Equal(t, "seam", claims.Issuer)
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	// Token with -1 minute TTL should be expired immediately.
	mgr := auth.NewJWTManager("test-secret", -1*time.Minute)

	token, err := mgr.GenerateAccessToken("user123", "alice")
	require.NoError(t, err)

	_, err = mgr.VerifyAccessToken(token)
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrTokenExpired)
}

func TestJWTManager_InvalidToken(t *testing.T) {
	mgr := auth.NewJWTManager("test-secret", 15*time.Minute)

	_, err := mgr.VerifyAccessToken("not-a-valid-jwt")
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrTokenInvalid)
}

func TestJWTManager_WrongSecret(t *testing.T) {
	mgr1 := auth.NewJWTManager("secret-1", 15*time.Minute)
	mgr2 := auth.NewJWTManager("secret-2", 15*time.Minute)

	token, err := mgr1.GenerateAccessToken("user123", "alice")
	require.NoError(t, err)

	_, err = mgr2.VerifyAccessToken(token)
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrTokenInvalid)
}
