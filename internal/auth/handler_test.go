package auth_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/testutil"
	"github.com/katata/seam/internal/userdb"
)

func newTestHandler(t *testing.T) (*auth.Handler, *auth.JWTManager) {
	t.Helper()
	db := testutil.TestServerDB(t)
	store := auth.NewSQLStore(db)
	jwtMgr := auth.NewJWTManager("test-secret-key", 15*time.Minute)
	dataDir := testutil.TestDataDir(t)
	userDBMgr := userdb.NewSQLManager(dataDir, 30*time.Minute, slog.Default())
	t.Cleanup(func() { userDBMgr.CloseAll() })
	svc := auth.NewService(store, jwtMgr, userDBMgr, 168*time.Hour, 4, slog.Default())
	handler := auth.NewHandler(svc, slog.Default())
	return handler, jwtMgr
}

func setupRouter(handler *auth.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Mount("/api/auth", handler.Routes())
	return r
}

func TestHandler_Register_Success(t *testing.T) {
	handler, _ := newTestHandler(t)
	r := setupRouter(handler)

	body, _ := json.Marshal(auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp auth.AuthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	require.Equal(t, "alice", resp.User.Username)
	require.NotEmpty(t, resp.Tokens.AccessToken)
	require.NotEmpty(t, resp.Tokens.RefreshToken)
}

func TestHandler_Register_DuplicateReturns409(t *testing.T) {
	handler, _ := newTestHandler(t)
	r := setupRouter(handler)

	body, _ := json.Marshal(auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})

	// First registration.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Second registration with same username.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestHandler_Login_Success(t *testing.T) {
	handler, _ := newTestHandler(t)
	r := setupRouter(handler)

	// Register first.
	regBody, _ := json.Marshal(auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Login.
	loginBody, _ := json.Marshal(auth.LoginReq{
		Username: "alice", Password: "password123",
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp auth.AuthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	require.Equal(t, "alice", resp.User.Username)
}

func TestHandler_Login_InvalidCredentials(t *testing.T) {
	handler, _ := newTestHandler(t)
	r := setupRouter(handler)

	body, _ := json.Marshal(auth.LoginReq{
		Username: "nonexistent", Password: "password123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Refresh_Success(t *testing.T) {
	handler, _ := newTestHandler(t)
	r := setupRouter(handler)

	// Register.
	regBody, _ := json.Marshal(auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var regResp auth.AuthResponse
	json.NewDecoder(w.Body).Decode(&regResp)

	// Refresh.
	refreshBody, _ := json.Marshal(auth.RefreshReq{RefreshToken: regResp.Tokens.RefreshToken})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewReader(refreshBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var tokens auth.TokenPair
	json.NewDecoder(w.Body).Decode(&tokens)
	require.NotEmpty(t, tokens.AccessToken)
}

func TestHandler_Logout_Success(t *testing.T) {
	handler, _ := newTestHandler(t)
	r := setupRouter(handler)

	// Register.
	regBody, _ := json.Marshal(auth.RegisterReq{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var regResp auth.AuthResponse
	json.NewDecoder(w.Body).Decode(&regResp)

	// Logout.
	logoutBody, _ := json.Marshal(auth.LogoutReq{RefreshToken: regResp.Tokens.RefreshToken})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/auth/logout", bytes.NewReader(logoutBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandler_InvalidJSON(t *testing.T) {
	handler, _ := newTestHandler(t)
	r := setupRouter(handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
