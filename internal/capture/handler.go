package capture

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for capture endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new capture Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with capture routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.capture)
	return r
}

// captureRequest is the request body for POST /api/capture.
type captureRequest struct {
	Type string `json:"type"` // "url" or "voice"
	URL  string `json:"url"`  // required when type=url
}

func (h *Handler) capture(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	// Check if this is a multipart form (voice upload) or JSON (URL capture).
	contentType := r.Header.Get("Content-Type")

	// For multipart/form-data, handle voice capture.
	if len(contentType) >= 19 && contentType[:19] == "multipart/form-data" {
		r.Body = http.MaxBytesReader(w, r.Body, 25<<20) // 25 MB max upload
		h.handleVoiceCapture(w, r, userID)
		return
	}

	// Otherwise, JSON body for URL capture (limit to 1MB).
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req captureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Type {
	case "url":
		h.handleURLCapture(w, r, userID, req.URL)
	default:
		writeError(w, http.StatusBadRequest, "type must be 'url' or use multipart form for voice upload")
	}
}

func (h *Handler) handleURLCapture(w http.ResponseWriter, r *http.Request, userID, rawURL string) {
	if rawURL == "" {
		writeError(w, http.StatusBadRequest, "url is required for URL capture")
		return
	}

	n, err := h.service.CaptureURL(r.Context(), userID, rawURL)
	if err != nil {
		if errors.Is(err, ErrInvalidURL) || errors.Is(err, ErrUnsafeScheme) {
			writeError(w, http.StatusBadRequest, "invalid or unsafe URL")
			return
		}
		if errors.Is(err, ErrPrivateIP) {
			writeError(w, http.StatusForbidden, "URL points to a private/loopback address")
			return
		}
		if errors.Is(err, ErrFetchFailed) {
			writeError(w, http.StatusBadGateway, "failed to fetch URL")
			return
		}
		h.logger.Error("capture.Handler.handleURLCapture: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "capture failed")
		return
	}

	writeJSON(w, http.StatusCreated, n)
}

func (h *Handler) handleVoiceCapture(w http.ResponseWriter, r *http.Request, userID string) {
	// Parse multipart form (max 25MB for audio files).
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "audio file is required (field name: 'audio')")
		return
	}
	defer file.Close()

	n, err := h.service.CaptureVoice(r.Context(), userID, file, header.Filename)
	if err != nil {
		h.logger.Error("capture.Handler.handleVoiceCapture: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "voice capture failed")
		return
	}

	writeJSON(w, http.StatusCreated, n)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("capture.writeJSON: encode error", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("capture.writeError: encode error", "error", err)
	}
}
