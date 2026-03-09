package note

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// listVersions handles GET /api/notes/{id}/versions?limit=20&offset=0
func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 100 {
		limit = 100
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	versions, total, err := h.service.ListVersions(r.Context(), userID, noteID, limit, offset)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("list versions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if versions == nil {
		versions = []*NoteVersion{}
	}

	w.Header().Set("X-Total-Count", fmt.Sprintf("%d", total))
	writeJSON(w, http.StatusOK, versions)
}

// getVersion handles GET /api/notes/{id}/versions/{version}
func (h *Handler) getVersion(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")
	versionStr := chi.URLParam(r, "version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid version number")
		return
	}

	v, err := h.service.GetVersion(r.Context(), userID, noteID, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		if errors.Is(err, ErrVersionNotFound) {
			writeError(w, http.StatusNotFound, "version not found")
			return
		}
		h.logger.Error("get version failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, v)
}

// restoreVersion handles POST /api/notes/{id}/versions/{version}/restore
func (h *Handler) restoreVersion(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")
	versionStr := chi.URLParam(r, "version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid version number")
		return
	}

	// Get the version content.
	v, err := h.service.GetVersion(r.Context(), userID, noteID, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		if errors.Is(err, ErrVersionNotFound) {
			writeError(w, http.StatusNotFound, "version not found")
			return
		}
		h.logger.Error("restore version: get version failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Update the note with the version's content (this will auto-create a new version).
	updated, err := h.service.Update(r.Context(), userID, noteID, UpdateNoteReq{
		Title: &v.Title,
		Body:  &v.Body,
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("restore version: update failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}
