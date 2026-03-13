package auth_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/testutil"
	"github.com/katata/seam/internal/userdb"
)

func newTestService(t *testing.T) *auth.Service {
	t.Helper()
	db := testutil.TestServerDB(t)
	store := auth.NewSQLStore(db)
	jwtMgr := auth.NewJWTManager("test-secret-key", 15*time.Minute)
	dataDir := testutil.TestDataDir(t)
	userDBMgr := userdb.NewSQLManager(dataDir, 30*time.Minute, slog.Default())
	t.Cleanup(func() { userDBMgr.CloseAll() })
	// Use bcrypt cost 4 for fast tests.
	return auth.NewService(store, jwtMgr, userDBMgr, 168*time.Hour, 4, slog.Default())
}

func TestService_Register_Success(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Register(ctx, auth.RegisterReq{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "alice", resp.User.Username)
	require.Equal(t, "alice@example.com", resp.User.Email)
	require.NotEmpty(t, resp.User.ID)
	require.NotEmpty(t, resp.Tokens.AccessToken)
	require.NotEmpty(t, resp.Tokens.RefreshToken)
}

func TestService_Register_DuplicateUsername(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	require.NoError(t, err)

	_, err = svc.Register(ctx, auth.RegisterReq{
		Username: "alice", Email: "alice2@example.com", Password: "password123",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrUserExists)
}

func TestService_Register_EmptyFields(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, auth.RegisterReq{Username: "", Email: "a@b.com", Password: "password123"})
	require.Error(t, err)

	_, err = svc.Register(ctx, auth.RegisterReq{Username: "alice", Email: "", Password: "password123"})
	require.Error(t, err)

	_, err = svc.Register(ctx, auth.RegisterReq{Username: "alice", Email: "a@b.com", Password: ""})
	require.Error(t, err)
}

func TestService_Register_ShortPassword(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, auth.RegisterReq{
		Username: "alice", Email: "a@b.com", Password: "short",
	})
	require.Error(t, err)
}

func TestService_Login_Success(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	require.NoError(t, err)

	resp, err := svc.Login(ctx, auth.LoginReq{
		Username: "alice", Password: "password123",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "alice", resp.User.Username)
	require.NotEmpty(t, resp.Tokens.AccessToken)
}

func TestService_Login_WrongPassword(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	require.NoError(t, err)

	_, err = svc.Login(ctx, auth.LoginReq{
		Username: "alice", Password: "wrongpassword",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

func TestService_Login_NonexistentUser(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Login(ctx, auth.LoginReq{
		Username: "nonexistent", Password: "password123",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

func TestService_Refresh_Success(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Register(ctx, auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	require.NoError(t, err)

	tokens, err := svc.Refresh(ctx, resp.Tokens.RefreshToken)
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)
	// Refresh token should be rotated (different from the original).
	require.NotEqual(t, resp.Tokens.RefreshToken, tokens.RefreshToken)
	require.NotEmpty(t, tokens.RefreshToken)

	// Old refresh token should no longer work.
	_, err = svc.Refresh(ctx, resp.Tokens.RefreshToken)
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)

	// New refresh token should work.
	tokens2, err := svc.Refresh(ctx, tokens.RefreshToken)
	require.NoError(t, err)
	require.NotEmpty(t, tokens2.AccessToken)
}

func TestService_Refresh_InvalidToken(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Refresh(ctx, "invalid-token")
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

func TestService_Logout_Success(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Register(ctx, auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	require.NoError(t, err)

	err = svc.Logout(ctx, resp.Tokens.RefreshToken)
	require.NoError(t, err)

	// Refresh should now fail.
	_, err = svc.Refresh(ctx, resp.Tokens.RefreshToken)
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)
}
