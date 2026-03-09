package template

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

func setupHandlerTest(t *testing.T) (*Handler, *chi.Mux) {
	t.Helper()
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)
	require.NoError(t, svc.EnsureDefaults())

	handler := NewHandler(svc, nil)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Route("/api/templates", func(r chi.Router) {
		r.Mount("/", handler.Routes())
	})
	return handler, r
}

func TestHandler_List(t *testing.T) {
	_, router := setupHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/templates/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var templates []TemplateMeta
	err := json.NewDecoder(w.Body).Decode(&templates)
	require.NoError(t, err)
	require.Len(t, templates, len(defaultTemplates))
}

func TestHandler_Get(t *testing.T) {
	_, router := setupHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/templates/daily-log", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var tmpl Template
	err := json.NewDecoder(w.Body).Decode(&tmpl)
	require.NoError(t, err)
	require.Equal(t, "daily-log", tmpl.Name)
	require.Contains(t, tmpl.Body, "Daily Log")
}

func TestHandler_Get_NotFound(t *testing.T) {
	_, router := setupHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/templates/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Apply(t *testing.T) {
	_, router := setupHandlerTest(t)

	body := `{"vars":{"project":"Seam"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/templates/project-kickoff/apply", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	require.Contains(t, resp["body"], "Seam - Project Kickoff")
}

func TestHandler_Apply_NotFound(t *testing.T) {
	_, router := setupHandlerTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/templates/nonexistent/apply", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_List_Unauthorized(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)
	handler := NewHandler(svc, nil)

	r := chi.NewRouter()
	// No user ID middleware.
	r.Get("/api/templates/", handler.list)

	req := httptest.NewRequest(http.MethodGet, "/api/templates/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Get_UserOverride(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)
	require.NoError(t, svc.EnsureDefaults())

	// Create per-user override.
	userTmplDir := filepath.Join(dir, "users", "test-user", "templates")
	require.NoError(t, os.MkdirAll(userTmplDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userTmplDir, "daily-log.md"), []byte("Custom daily log body"), 0o644))

	handler := NewHandler(svc, nil)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Route("/api/templates", func(r chi.Router) {
		r.Mount("/", handler.Routes())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/templates/daily-log", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var tmpl Template
	json.NewDecoder(w.Body).Decode(&tmpl)
	require.Equal(t, "Custom daily log body", tmpl.Body)
}
