package project_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/userdb"
)

const handlerTestUserID = "01HTEST000000000000000099"

func newTestHandler(t *testing.T) *project.Handler {
	t.Helper()
	dataDir := t.TempDir()
	mgr := userdb.NewSQLManager(dataDir, 30*time.Minute, slog.Default())
	t.Cleanup(func() { mgr.CloseAll() })
	store := project.NewStore()
	svc := project.NewService(store, mgr, slog.Default())
	return project.NewHandler(svc, slog.Default())
}

// setupProjectRouter creates a chi router with auth context injection
// and project routes mounted at /api/projects.
func setupProjectRouter(handler *project.Handler) *chi.Mux {
	r := chi.NewRouter()
	// Inject user ID into context for all requests.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), reqctx.UserIDKey, handlerTestUserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/api/projects", handler.Routes())
	return r
}

func TestHandler_Create_Success(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	body, _ := json.Marshal(map[string]string{
		"name": "My Project", "description": "test desc",
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var p project.Project
	err := json.NewDecoder(w.Body).Decode(&p)
	require.NoError(t, err)
	require.Equal(t, "My Project", p.Name)
	require.Equal(t, "my-project", p.Slug)
	require.Equal(t, "test desc", p.Description)
	require.NotEmpty(t, p.ID)
}

func TestHandler_Create_MissingName(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	body, _ := json.Marshal(map[string]string{
		"description": "no name",
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Create_DuplicateSlug(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	body, _ := json.Marshal(map[string]string{"name": "Duplicate"})

	// First create.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Second create with same name.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestHandler_Create_InvalidJSON(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_List_Empty(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/projects/", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var projects []project.Project
	err := json.NewDecoder(w.Body).Decode(&projects)
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestHandler_List_WithProjects(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	// Create two projects.
	for _, name := range []string{"Alpha", "Beta"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/projects/", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var projects []project.Project
	err := json.NewDecoder(w.Body).Decode(&projects)
	require.NoError(t, err)
	require.Len(t, projects, 2)
}

func TestHandler_Get_Success(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	// Create a project.
	body, _ := json.Marshal(map[string]string{"name": "Fetchable"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var created project.Project
	json.NewDecoder(w.Body).Decode(&created)

	// Fetch by ID.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/projects/"+created.ID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var got project.Project
	err := json.NewDecoder(w.Body).Decode(&got)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "Fetchable", got.Name)
}

func TestHandler_Get_NotFound(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/projects/nonexistent", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Update_Success(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	// Create.
	body, _ := json.Marshal(map[string]string{"name": "Original", "description": "old"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var created project.Project
	json.NewDecoder(w.Body).Decode(&created)

	// Update name and description.
	updateBody, _ := json.Marshal(map[string]string{
		"name": "Updated", "description": "new",
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/api/projects/"+created.ID, bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var updated project.Project
	err := json.NewDecoder(w.Body).Decode(&updated)
	require.NoError(t, err)
	require.Equal(t, "Updated", updated.Name)
	require.Equal(t, "updated", updated.Slug)
	require.Equal(t, "new", updated.Description)
}

func TestHandler_Update_NotFound(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	body, _ := json.Marshal(map[string]string{"name": "X"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/projects/nonexistent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Update_NoFields(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/projects/some-id", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Update_EmptyName(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	body, _ := json.Marshal(map[string]string{"name": ""})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/projects/some-id", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Delete_Success(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	// Create.
	body, _ := json.Marshal(map[string]string{"name": "ToDelete"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var created project.Project
	json.NewDecoder(w.Body).Decode(&created)

	// Delete.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/projects/"+created.ID+"?cascade=inbox", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	// Confirm gone.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/projects/"+created.ID, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Delete_DefaultCascade(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	// Create.
	body, _ := json.Marshal(map[string]string{"name": "DefaultCascade"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var created project.Project
	json.NewDecoder(w.Body).Decode(&created)

	// Delete without cascade param (should default to "inbox").
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/projects/"+created.ID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandler_Delete_InvalidCascade(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/projects/some-id?cascade=invalid", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Delete_NotFound(t *testing.T) {
	handler := newTestHandler(t)
	r := setupProjectRouter(handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/projects/nonexistent?cascade=inbox", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_NoUserID(t *testing.T) {
	handler := newTestHandler(t)
	// Router WITHOUT user ID middleware.
	r := chi.NewRouter()
	r.Mount("/api/projects", handler.Routes())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/projects/", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
