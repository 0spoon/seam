package note

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/userdb"
)

const handlerTestUserID = "handler-test-user"

// setupHandler creates a Handler with a real service and returns the router and service.
func setupHandler(t *testing.T) (http.Handler, *Service) {
	t.Helper()

	dataDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mgr := userdb.NewSQLManager(dataDir, logger)
	t.Cleanup(func() { mgr.CloseAll() })

	noteStore := NewSQLStore()
	versionStore := NewVersionStore()
	projStore := project.NewStore()
	svc := NewService(noteStore, versionStore, projStore, mgr, nil, logger)
	handler := NewHandler(svc, logger)

	r := chi.NewRouter()
	// Inject user ID into context for all requests.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), reqctx.UserIDKey, handlerTestUserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/api/notes", handler.Routes())

	return r, svc
}

func TestHandler_Create(t *testing.T) {
	router, _ := setupHandler(t)

	body := `{"title":"Handler Note","body":"Created via HTTP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var n Note
	err := json.NewDecoder(rec.Body).Decode(&n)
	require.NoError(t, err)
	require.Equal(t, "Handler Note", n.Title)
	require.NotEmpty(t, n.ID)
	require.Equal(t, "handler-note.md", n.FilePath)
}

func TestHandler_Create_MissingTitle(t *testing.T) {
	router, _ := setupHandler(t)

	body := `{"body":"No title"}`
	req := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Create_InvalidJSON(t *testing.T) {
	router, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Get(t *testing.T) {
	router, svc := setupHandler(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, handlerTestUserID, CreateNoteReq{
		Title: "Get Me",
		Body:  "body",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/notes/"+n.ID, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var got Note
	err = json.NewDecoder(rec.Body).Decode(&got)
	require.NoError(t, err)
	require.Equal(t, n.ID, got.ID)
	require.Equal(t, "Get Me", got.Title)
}

func TestHandler_Get_NotFound(t *testing.T) {
	router, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/notes/NONEXISTENT", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_List(t *testing.T) {
	router, svc := setupHandler(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, handlerTestUserID, CreateNoteReq{Title: "A", Body: "a"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, handlerTestUserID, CreateNoteReq{Title: "B", Body: "b"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "2", rec.Header().Get("X-Total-Count"))

	var notes []Note
	err = json.NewDecoder(rec.Body).Decode(&notes)
	require.NoError(t, err)
	require.Len(t, notes, 2)
}

func TestHandler_List_Pagination(t *testing.T) {
	router, svc := setupHandler(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := svc.Create(ctx, handlerTestUserID, CreateNoteReq{
			Title: "Note " + string(rune('A'+i)),
			Body:  "body",
		})
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/notes?limit=2&offset=0", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "5", rec.Header().Get("X-Total-Count"))

	var notes []Note
	err := json.NewDecoder(rec.Body).Decode(&notes)
	require.NoError(t, err)
	require.Len(t, notes, 2)
}

func TestHandler_List_Inbox(t *testing.T) {
	router, _ := setupHandler(t)

	// Create an inbox note via API.
	body := `{"title":"Inbox Note","body":"in inbox"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBufferString(body))
	createReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, createReq)
	require.Equal(t, http.StatusCreated, rec.Code)

	// List inbox notes.
	listReq := httptest.NewRequest(http.MethodGet, "/api/notes?project=inbox", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	require.Equal(t, http.StatusOK, listRec.Code)
	require.Equal(t, "1", listRec.Header().Get("X-Total-Count"))
}

func TestHandler_Update(t *testing.T) {
	router, svc := setupHandler(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, handlerTestUserID, CreateNoteReq{
		Title: "To Update",
		Body:  "original",
	})
	require.NoError(t, err)

	body := `{"body":"updated via HTTP"}`
	req := httptest.NewRequest(http.MethodPut, "/api/notes/"+n.ID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var updated Note
	err = json.NewDecoder(rec.Body).Decode(&updated)
	require.NoError(t, err)
	require.Equal(t, "updated via HTTP", updated.Body)
}

func TestHandler_Update_NotFound(t *testing.T) {
	router, _ := setupHandler(t)

	body := `{"body":"update"}`
	req := httptest.NewRequest(http.MethodPut, "/api/notes/NONEXISTENT", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_Update_NoFields(t *testing.T) {
	router, _ := setupHandler(t)

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/notes/SOMEID", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Update_EmptyTitle(t *testing.T) {
	router, _ := setupHandler(t)

	body := `{"title":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/notes/SOMEID", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Delete(t *testing.T) {
	router, svc := setupHandler(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, handlerTestUserID, CreateNoteReq{
		Title: "To Delete",
		Body:  "goodbye",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/notes/"+n.ID, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)

	// Verify it is gone.
	getReq := httptest.NewRequest(http.MethodGet, "/api/notes/"+n.ID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusNotFound, getRec.Code)
}

func TestHandler_Delete_NotFound(t *testing.T) {
	router, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/notes/NONEXISTENT", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_Backlinks(t *testing.T) {
	router, svc := setupHandler(t)
	ctx := context.Background()

	target, err := svc.Create(ctx, handlerTestUserID, CreateNoteReq{
		Title: "Link Target",
		Body:  "target body",
	})
	require.NoError(t, err)

	_, err = svc.Create(ctx, handlerTestUserID, CreateNoteReq{
		Title: "Linker",
		Body:  "See [[Link Target]]",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/notes/"+target.ID+"/backlinks", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var backlinks []Note
	err = json.NewDecoder(rec.Body).Decode(&backlinks)
	require.NoError(t, err)
	require.Len(t, backlinks, 1)
	require.Equal(t, "Linker", backlinks[0].Title)
}

func TestHandler_Backlinks_NotFound(t *testing.T) {
	router, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/notes/NONEXISTENT/backlinks", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_MissingAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	dataDir := t.TempDir()
	mgr := userdb.NewSQLManager(dataDir, logger)
	t.Cleanup(func() { mgr.CloseAll() })

	noteStore := NewSQLStore()
	versionStore := NewVersionStore()
	projStore := project.NewStore()
	svc := NewService(noteStore, versionStore, projStore, mgr, nil, logger)
	handler := NewHandler(svc, logger)

	// Router WITHOUT auth middleware.
	r := chi.NewRouter()
	r.Mount("/api/notes", handler.Routes())

	req := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
