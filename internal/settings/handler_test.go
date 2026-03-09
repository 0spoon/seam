package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

// mockService implements ServiceInterface for handler tests.
type mockService struct {
	getAll func(ctx context.Context, userID string) (map[string]string, error)
	update func(ctx context.Context, userID string, settings map[string]string) error
	delete func(ctx context.Context, userID, key string) error
}

func (m *mockService) GetAll(ctx context.Context, userID string) (map[string]string, error) {
	if m.getAll != nil {
		return m.getAll(ctx, userID)
	}
	return map[string]string{}, nil
}

func (m *mockService) Update(ctx context.Context, userID string, settings map[string]string) error {
	if m.update != nil {
		return m.update(ctx, userID, settings)
	}
	return nil
}

func (m *mockService) Delete(ctx context.Context, userID, key string) error {
	if m.delete != nil {
		return m.delete(ctx, userID, key)
	}
	return nil
}

func newTestRouter(svc ServiceInterface) *chi.Mux {
	h := NewHandler(svc, nil)
	r := chi.NewRouter()
	// Inject a test user ID into the context.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/settings", h.Routes())
	return r
}

func TestHandler_GetAll(t *testing.T) {
	svc := &mockService{
		getAll: func(_ context.Context, userID string) (map[string]string, error) {
			require.Equal(t, "test-user", userID)
			return map[string]string{"editor_view_mode": "split"}, nil
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/settings/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "split", body["editor_view_mode"])
}

func TestHandler_GetAll_Unauthorized(t *testing.T) {
	h := NewHandler(&mockService{}, nil)
	r := chi.NewRouter()
	r.Mount("/settings", h.Routes())

	req := httptest.NewRequest(http.MethodGet, "/settings/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_Update_ValidKey(t *testing.T) {
	var capturedSettings map[string]string
	svc := &mockService{
		update: func(_ context.Context, _ string, settings map[string]string) error {
			capturedSettings = settings
			return nil
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]string{"editor_view_mode": "preview"})
	req := httptest.NewRequest(http.MethodPut, "/settings/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "preview", capturedSettings["editor_view_mode"])
}

func TestHandler_Update_InvalidKey(t *testing.T) {
	svc := &mockService{
		update: func(_ context.Context, _ string, _ map[string]string) error {
			return ErrInvalidKey
		},
	}
	r := newTestRouter(svc)

	body, _ := json.Marshal(map[string]string{"bad_key": "value"})
	req := httptest.NewRequest(http.MethodPut, "/settings/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Update_EmptyBody(t *testing.T) {
	r := newTestRouter(&mockService{})

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPut, "/settings/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Delete(t *testing.T) {
	var capturedKey string
	svc := &mockService{
		delete: func(_ context.Context, _ string, key string) error {
			capturedKey = key
			return nil
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodDelete, "/settings/editor_view_mode", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "editor_view_mode", capturedKey)
}

func TestHandler_Delete_InvalidKey(t *testing.T) {
	svc := &mockService{
		delete: func(_ context.Context, _ string, _ string) error {
			return ErrInvalidKey
		},
	}
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodDelete, "/settings/bad_key", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
