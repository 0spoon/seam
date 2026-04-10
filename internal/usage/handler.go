package usage

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/settings"
	"github.com/katata/seam/internal/userdb"
)

// Handler handles HTTP requests for usage dashboard endpoints.
type Handler struct {
	store       *Store
	dbManager   userdb.Manager
	settingsSvc *settings.Service
	logger      *slog.Logger
}

// NewHandler creates a new usage Handler.
func NewHandler(store *Store, dbManager userdb.Manager, settingsSvc *settings.Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		store:       store,
		dbManager:   dbManager,
		settingsSvc: settingsSvc,
		logger:      logger,
	}
}

// Routes returns a chi router with all usage routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/summary", h.getSummary)
	r.Get("/by-function", h.getByFunction)
	r.Get("/by-provider", h.getByProvider)
	r.Get("/by-model", h.getByModel)
	r.Get("/timeseries", h.getTimeSeries)
	r.Get("/budget", h.getBudget)
	r.Put("/budget", h.updateBudget)
	return r
}

func (h *Handler) getSummary(w http.ResponseWriter, r *http.Request) {
	userID, db, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	sum, err := h.store.GetSummary(r.Context(), db, from, to)
	if err != nil {
		h.logger.Error("get usage summary failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (h *Handler) getByFunction(w http.ResponseWriter, r *http.Request) {
	userID, db, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	result, err := h.store.GetByFunction(r.Context(), db, from, to)
	if err != nil {
		h.logger.Error("get usage by function failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if result == nil {
		result = []FunctionUsage{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) getByProvider(w http.ResponseWriter, r *http.Request) {
	userID, db, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	result, err := h.store.GetByProvider(r.Context(), db, from, to)
	if err != nil {
		h.logger.Error("get usage by provider failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if result == nil {
		result = []ProviderUsage{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) getByModel(w http.ResponseWriter, r *http.Request) {
	userID, db, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	result, err := h.store.GetByModel(r.Context(), db, from, to)
	if err != nil {
		h.logger.Error("get usage by model failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if result == nil {
		result = []ModelUsage{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) getTimeSeries(w http.ResponseWriter, r *http.Request) {
	userID, db, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	granularity := r.URL.Query().Get("granularity")
	if granularity == "" {
		granularity = "day"
	}
	switch granularity {
	case "hour", "day", "month":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "granularity must be hour, day, or month")
		return
	}

	result, err := h.store.GetTimeSeries(r.Context(), db, from, to, granularity)
	if err != nil {
		h.logger.Error("get usage timeseries failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if result == nil {
		result = []TimeSeriesPoint{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) getBudget(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	all, err := h.settingsSvc.GetAll(r.Context(), userID)
	if err != nil {
		h.logger.Error("get budget settings failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	enabled := all["usage_budget_enabled"] == "true"
	period := all["usage_budget_period"]
	if period == "" {
		period = "monthly"
	}
	maxTokens, _ := strconv.ParseInt(all["usage_budget_max_tokens"], 10, 64)
	gateLocal := all["usage_budget_gate_local"] == "true"

	var usedTokens int64
	if enabled && maxTokens > 0 {
		db, dbErr := h.dbManager.Open(r.Context(), userID)
		if dbErr == nil {
			usedTokens, _ = h.store.GetPeriodTotal(r.Context(), db, period, gateLocal)
		}
	}

	remaining := maxTokens - usedTokens
	if remaining < 0 || !enabled {
		remaining = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":          enabled,
		"period":           period,
		"max_tokens":       maxTokens,
		"used_tokens":      usedTokens,
		"remaining_tokens": remaining,
		"gate_local":       gateLocal,
	})
}

func (h *Handler) updateBudget(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Enabled   *bool  `json:"enabled"`
		Period    string `json:"period"`
		MaxTokens *int64 `json:"max_tokens"`
		GateLocal *bool  `json:"gate_local"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updates := make(map[string]string)
	if req.Enabled != nil {
		updates["usage_budget_enabled"] = strconv.FormatBool(*req.Enabled)
	}
	if req.Period != "" {
		updates["usage_budget_period"] = req.Period
	}
	if req.MaxTokens != nil {
		updates["usage_budget_max_tokens"] = strconv.FormatInt(*req.MaxTokens, 10)
	}
	if req.GateLocal != nil {
		updates["usage_budget_gate_local"] = strconv.FormatBool(*req.GateLocal)
	}

	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, "no budget fields provided")
		return
	}

	if err := h.settingsSvc.Update(r.Context(), userID, updates); err != nil {
		h.logger.Error("update budget failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// resolveDB opens the user's database. Returns false if it fails and writes an error.
func (h *Handler) resolveDB(w http.ResponseWriter, r *http.Request) (string, DBTX, bool) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return "", nil, false
	}
	db, err := h.dbManager.Open(r.Context(), userID)
	if err != nil {
		h.logger.Error("open user db failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return "", nil, false
	}
	return userID, db, true
}

// parseDateRange extracts from/to query params. Defaults to last 30 days.
func parseDateRange(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -30)

	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			// Try date-only format.
			t, err = time.Parse("2006-01-02", v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid 'from' date (use RFC3339 or YYYY-MM-DD)")
				return time.Time{}, time.Time{}, false
			}
		}
		from = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t, err = time.Parse("2006-01-02", v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid 'to' date (use RFC3339 or YYYY-MM-DD)")
				return time.Time{}, time.Time{}, false
			}
			// End of day for date-only.
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		to = t
	}

	return from, to, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("usage.writeJSON: encode error", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("usage.writeError: encode error", "error", err)
	}
}
