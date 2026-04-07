package scheduler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

// withUser injects an authenticated user ID into the request context the way
// AuthMiddleware does in production.
func withUser(r *http.Request, userID string) *http.Request {
	return r.WithContext(reqctx.WithUserID(r.Context(), userID))
}

func TestHandler_Create_Recurring(t *testing.T) {
	now := time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	h := NewHandler(svc, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Daily Briefing",
		"cron_expr":   "0 8 * * *",
		"action_type": "briefing",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = withUser(req, "default")
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	var sch Schedule
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sch))
	require.NotEmpty(t, sch.ID)
	require.Equal(t, ActionBriefing, sch.ActionType)
	require.Equal(t, "0 8 * * *", sch.CronExpr)
	require.NotNil(t, sch.NextRunAt)
}

func TestHandler_Create_ValidationError(t *testing.T) {
	svc, _ := newTestService(t, time.Now())
	h := NewHandler(svc, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Bad",
		"action_type": "briefing",
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)), "default")
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "cron_expr")
}

func TestHandler_Create_Unauthorized(t *testing.T) {
	svc, _ := newTestService(t, time.Now())
	h := NewHandler(svc, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Daily Briefing",
		"cron_expr":   "0 8 * * *",
		"action_type": "briefing",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)) // no user
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_List(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC))
	_, err := svc.Create(t.Context(), "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)

	h := NewHandler(svc, nil)
	req := withUser(httptest.NewRequest(http.MethodGet, "/", nil), "default")
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var out []Schedule
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out, 1)
	require.Equal(t, "Daily Briefing", out[0].Name)
}

func TestHandler_GetUpdateDelete(t *testing.T) {
	svc, _ := newTestService(t, time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC))
	created, err := svc.Create(t.Context(), "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)

	h := NewHandler(svc, nil)

	// Get
	req := withUser(httptest.NewRequest(http.MethodGet, "/"+created.ID, nil), "default")
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Update
	body, _ := json.Marshal(map[string]interface{}{"cron_expr": "0 18 * * *"})
	req = withUser(httptest.NewRequest(http.MethodPut, "/"+created.ID, bytes.NewReader(body)), "default")
	w = httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var updated Schedule
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	require.Equal(t, "0 18 * * *", updated.CronExpr)

	// Delete
	req = withUser(httptest.NewRequest(http.MethodDelete, "/"+created.ID, nil), "default")
	w = httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	// Confirm gone
	req = withUser(httptest.NewRequest(http.MethodGet, "/"+created.ID, nil), "default")
	w = httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}
