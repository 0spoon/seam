package search

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for search endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new search Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with search routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.searchFTS)
	r.Get("/semantic", h.searchSemantic)
	return r
}

func (h *Handler) searchFTS(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	// Parse optional recency_bias parameter.
	var recencyBias float64
	if rb := r.URL.Query().Get("recency_bias"); rb != "" {
		parsed, err := strconv.ParseFloat(rb, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recency_bias: must be a number")
			return
		}
		if parsed < 0 || parsed > 1 {
			writeError(w, http.StatusBadRequest, "invalid recency_bias: must be between 0.0 and 1.0")
			return
		}
		recencyBias = parsed
	}

	var results []FTSResult
	var total int
	var err error
	if recencyBias > 0 {
		results, total, err = h.service.SearchFTSWithRecency(r.Context(), userID, query, limit, offset, recencyBias)
	} else {
		results, total, err = h.service.SearchFTS(r.Context(), userID, query, limit, offset)
	}
	if err != nil {
		h.logger.Error("search failed", "error", err, "query", query)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if results == nil {
		results = []FTSResult{}
	}

	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) searchSemantic(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}

	// Parse optional recency_bias parameter.
	var recencyBias float64
	if rb := r.URL.Query().Get("recency_bias"); rb != "" {
		parsed, err := strconv.ParseFloat(rb, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recency_bias: must be a number")
			return
		}
		if parsed < 0 || parsed > 1 {
			writeError(w, http.StatusBadRequest, "invalid recency_bias: must be between 0.0 and 1.0")
			return
		}
		recencyBias = parsed
	}

	var results []SemanticResult
	var err error
	if recencyBias > 0 {
		results, err = h.service.SearchSemanticWithRecency(r.Context(), userID, query, limit, recencyBias)
	} else {
		results, err = h.service.SearchSemantic(r.Context(), userID, query, limit)
	}
	if err != nil {
		h.logger.Error("semantic search failed", "error", err, "query", query)
		writeError(w, http.StatusInternalServerError, "semantic search not available")
		return
	}

	if results == nil {
		results = []SemanticResult{}
	}

	writeJSON(w, http.StatusOK, results)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("search.writeJSON: encode error", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("search.writeError: encode error", "error", err)
	}
}
