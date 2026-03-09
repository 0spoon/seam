package graph

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for the graph endpoint.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new graph Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with all graph routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.getGraph)
	r.Get("/two-hop-backlinks/{id}", h.getTwoHopBacklinks)
	r.Get("/orphans", h.getOrphans)
	return r
}

func (h *Handler) getGraph(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	filter := GraphFilter{}

	if projectID := r.URL.Query().Get("project"); projectID != "" {
		filter.ProjectID = projectID
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filter.Tag = tag
	}
	if since := r.URL.Query().Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter: must be RFC3339 format")
			return
		}
		filter.Since = t
	}
	if until := r.URL.Query().Get("until"); until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid until parameter: must be RFC3339 format")
			return
		}
		filter.Until = t
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if v, err := strconv.Atoi(limit); err == nil && v > 0 {
			filter.Limit = v
		}
	}
	// Cap at maximum 500 nodes.
	if filter.Limit > 500 {
		filter.Limit = 500
	}

	graph, err := h.service.GetGraph(r.Context(), userID, filter)
	if err != nil {
		h.logger.Error("get graph failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Ensure non-nil slices for clean JSON output.
	if graph.Nodes == nil {
		graph.Nodes = []Node{}
	}
	if graph.Edges == nil {
		graph.Edges = []Edge{}
	}

	writeJSON(w, http.StatusOK, graph)
}

func (h *Handler) getTwoHopBacklinks(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")
	if noteID == "" {
		writeError(w, http.StatusBadRequest, "note ID is required")
		return
	}

	nodes, err := h.service.GetTwoHopBacklinks(r.Context(), userID, noteID)
	if err != nil {
		h.logger.Error("get two-hop backlinks failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if nodes == nil {
		nodes = []TwoHopNode{}
	}

	writeJSON(w, http.StatusOK, nodes)
}

func (h *Handler) getOrphans(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	nodes, err := h.service.GetOrphanNotes(r.Context(), userID)
	if err != nil {
		h.logger.Error("get orphan notes failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if nodes == nil {
		nodes = []Node{}
	}

	writeJSON(w, http.StatusOK, nodes)
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("graph.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
