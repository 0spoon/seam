package capture

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

func TestHandler_CaptureURL_FetchError(t *testing.T) {
	// Test that the handler correctly returns 502 when the remote server is down.
	// We use a URL pointing to a server that returns 404.
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer pageServer.Close()

	fetcher := &URLFetcher{client: pageServer.Client()}
	svc := NewService(nil, fetcher, nil, nil)
	handler := NewHandler(svc, nil)

	body := `{"type":"url","url":"` + pageServer.URL + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/capture/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user-id")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	// Remote server returned 404, so fetch failed -> 502 Bad Gateway.
	require.Equal(t, http.StatusBadGateway, w.Code)
}

func TestHandler_CaptureURL_MissingURL(t *testing.T) {
	svc := NewService(nil, NewURLFetcher(), nil, nil)
	handler := NewHandler(svc, nil)

	body := `{"type":"url","url":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/capture/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user-id")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_CaptureURL_InvalidType(t *testing.T) {
	svc := NewService(nil, NewURLFetcher(), nil, nil)
	handler := NewHandler(svc, nil)

	body := `{"type":"invalid","url":"http://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/capture/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user-id")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_CaptureURL_Unauthorized(t *testing.T) {
	svc := NewService(nil, NewURLFetcher(), nil, nil)
	handler := NewHandler(svc, nil)

	body := `{"type":"url","url":"http://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/capture/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// No user ID middleware.
	r := chi.NewRouter()
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_CaptureURL_InvalidJSON(t *testing.T) {
	svc := NewService(nil, NewURLFetcher(), nil, nil)
	handler := NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/capture/", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user-id")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_VoiceCapture_MissingAudioField(t *testing.T) {
	svc := NewService(nil, NewURLFetcher(), nil, nil)
	handler := NewHandler(svc, nil)

	// Create multipart form without the "audio" field.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("type", "voice")
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/capture/", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user-id")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	require.Contains(t, resp["error"], "audio file is required")
}

func TestHandler_CaptureURL_UnsafeScheme(t *testing.T) {
	svc := NewService(nil, NewURLFetcher(), nil, nil)
	handler := NewHandler(svc, nil)

	body := `{"type":"url","url":"file:///etc/passwd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/capture/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user-id")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
