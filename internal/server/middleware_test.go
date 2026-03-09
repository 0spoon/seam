package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/server"
)

func TestRequestIDMiddleware(t *testing.T) {
	handler := server.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := reqctx.RequestIDFromContext(r.Context())
		require.NotEmpty(t, reqID)
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	jwtMgr := auth.NewJWTManager("test-secret", 15*time.Minute)
	token, err := jwtMgr.GenerateAccessToken("user123", "alice")
	require.NoError(t, err)

	handler := server.AuthMiddleware(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := reqctx.UserIDFromContext(r.Context())
		require.Equal(t, "user123", userID)
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	jwtMgr := auth.NewJWTManager("test-secret", 15*time.Minute)

	handler := server.AuthMiddleware(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	jwtMgr := auth.NewJWTManager("test-secret", 15*time.Minute)

	handler := server.AuthMiddleware(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	jwtMgr := auth.NewJWTManager("test-secret", -1*time.Minute)
	token, err := jwtMgr.GenerateAccessToken("user123", "alice")
	require.NoError(t, err)

	verifyMgr := auth.NewJWTManager("test-secret", 15*time.Minute)
	handler := server.AuthMiddleware(verifyMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecoveryMiddleware(t *testing.T) {
	logger := slog.Default()
	handler := server.RecoveryMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}
