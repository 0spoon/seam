package server_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/server"
	"github.com/katata/seam/internal/testutil"
)

// stubUserDBManager satisfies userdb.Manager for test setup without
// touching the filesystem. None of the methods are called during
// route mounting; they exist only to satisfy the auth.NewService
// constructor.
type stubUserDBManager struct{}

func (s *stubUserDBManager) Open(_ context.Context, _ string) (*sql.DB, error) {
	return nil, nil
}
func (s *stubUserDBManager) Close(_ string) error         { return nil }
func (s *stubUserDBManager) CloseAll() error              { return nil }
func (s *stubUserDBManager) UserNotesDir(_ string) string { return "" }
func (s *stubUserDBManager) UserDataDir(_ string) string  { return "" }
func (s *stubUserDBManager) ListUsers(_ context.Context) ([]string, error) {
	return nil, nil
}
func (s *stubUserDBManager) EnsureUserDirs(_ string) error { return nil }

// minimalConfig returns a server.Config with the minimum dependencies
// needed to construct a Server without panicking. The auth handler is
// wired with an in-memory SQLite database so route mounting succeeds.
func minimalConfig(t *testing.T) server.Config {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jwtMgr := auth.NewJWTManager("test-secret-key-at-least-32-chars", 15*time.Minute)

	db := testutil.TestServerDB(t)
	authStore := auth.NewSQLStore(db)
	authSvc := auth.NewService(authStore, jwtMgr, &stubUserDBManager{}, 24*time.Hour, 4, logger)
	authHandler := auth.NewHandler(authSvc, logger)

	return server.Config{
		Listen:      ":0",
		Logger:      logger,
		JWTManager:  jwtMgr,
		AuthHandler: authHandler,
		CORSOrigins: []string{"http://localhost:5173"},
	}
}

// mcpEchoHandler is a simple handler that writes a marker response so tests
// can verify that the MCP endpoint was reached.
func mcpEchoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("mcp-ok"))
	})
}

func TestNew_MCPHandlerMounted_RoutesExist(t *testing.T) {
	cfg := minimalConfig(t)
	cfg.MCPHandler = mcpEchoHandler()

	srv := server.New(cfg)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/mcp", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "mcp-ok", string(body))
}

func TestNew_MCPHandlerNil_NoRoute(t *testing.T) {
	cfg := minimalConfig(t)
	// MCPHandler intentionally left nil.

	srv := server.New(cfg)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// POST to /api/mcp should not match any route. Without the SPA
	// fallback (WebDistDir is empty), chi returns 405 Method Not Allowed
	// for unmatched methods or 404 Not Found for unmatched paths.
	resp, err := http.Post(ts.URL+"/api/mcp", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Either 404 or 405 is acceptable; the key point is the handler
	// is NOT mounted so we should not get 200.
	require.True(t, resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed,
		"expected 404 or 405, got %d", resp.StatusCode)
}

func TestNew_CORSHeaders_IncludeMcpSessionId(t *testing.T) {
	cfg := minimalConfig(t)
	cfg.MCPHandler = mcpEchoHandler()

	srv := server.New(cfg)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	t.Run("preflight allows Mcp-Session-Id header", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/mcp", nil)
		require.NoError(t, err)
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Mcp-Session-Id")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
		require.NotEmpty(t, allowHeaders, "expected Access-Control-Allow-Headers in preflight response")
		require.True(t, containsHeaderValue(allowHeaders, "Mcp-Session-Id"),
			"Access-Control-Allow-Headers should contain Mcp-Session-Id, got: %s", allowHeaders)
	})

	t.Run("regular response exposes Mcp-Session-Id header", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/mcp", nil)
		require.NoError(t, err)
		req.Header.Set("Origin", "http://localhost:5173")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		exposeHeaders := resp.Header.Get("Access-Control-Expose-Headers")
		require.NotEmpty(t, exposeHeaders, "expected Access-Control-Expose-Headers in response")
		require.True(t, containsHeaderValue(exposeHeaders, "Mcp-Session-Id"),
			"Access-Control-Expose-Headers should contain Mcp-Session-Id, got: %s", exposeHeaders)
	})
}

func TestNew_MCPHandlerNotBehindAuthMiddleware_NoAuthRequired(t *testing.T) {
	cfg := minimalConfig(t)
	cfg.MCPHandler = mcpEchoHandler()

	srv := server.New(cfg)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Send POST without Authorization header. The MCP handler should
	// still be reached because it sits outside the auth middleware group.
	resp, err := http.Post(ts.URL+"/api/mcp", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"MCP handler should be reachable without auth; got status %d", resp.StatusCode)
	require.Equal(t, "mcp-ok", string(body))
}

func TestNew_HealthEndpoint_StillWorks(t *testing.T) {
	cfg := minimalConfig(t)
	cfg.MCPHandler = mcpEchoHandler()

	srv := server.New(cfg)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), `"status":"ok"`)
}

func TestNew_MCPHandlerReceivesAllMethods_PostGetDelete(t *testing.T) {
	// The MCP Streamable HTTP transport requires POST, GET, and DELETE
	// on the same path. Verify that chi's Handle (which mounts all methods)
	// routes each method to the handler.
	cfg := minimalConfig(t)

	var receivedMethods []string
	cfg.MCPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethods = append(receivedMethods, r.Method)
		w.WriteHeader(http.StatusOK)
	})

	srv := server.New(cfg)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	methods := []string{http.MethodPost, http.MethodGet, http.MethodDelete}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequest(method, ts.URL+"/api/mcp", nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode,
				"%s /api/mcp should reach the handler", method)
		})
	}

	require.Equal(t, methods, receivedMethods,
		"handler should have received POST, GET, and DELETE in order")
}

// containsHeaderValue checks whether a comma-separated header value list
// contains the target value (case-insensitive comparison).
func containsHeaderValue(header, target string) bool {
	for _, v := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(v), target) {
			return true
		}
	}
	return false
}
