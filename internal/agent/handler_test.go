package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

func agentAPIRequest(t *testing.T, h *Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), testUserID))
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	return w
}

func TestAgentHandler_ListSessions(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	_, err := svc.SessionStart(ctx, testUserID, "build-thing", "", DefaultMaxContextChars)
	require.NoError(t, err)
	require.NoError(t, svc.SessionEnd(ctx, testUserID, "build-thing", "shipped it"))

	h := NewHandler(svc, nil)
	w := agentAPIRequest(t, h, http.MethodGet, "/sessions?status=completed")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Sessions []*Session `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Sessions, 1)
	require.Equal(t, "build-thing", resp.Sessions[0].Name)
	require.Equal(t, StatusCompleted, resp.Sessions[0].Status)
}

func TestAgentHandler_GetSessionDetail(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	_, err := svc.SessionStart(ctx, testUserID, "with-plan", "", DefaultMaxContextChars)
	require.NoError(t, err)
	_, err = svc.SessionPlanSet(ctx, testUserID, "with-plan", "the plan body")
	require.NoError(t, err)

	h := NewHandler(svc, nil)
	w := agentAPIRequest(t, h, http.MethodGet, "/sessions/with-plan")
	require.Equal(t, http.StatusOK, w.Code)

	var detail SessionDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Equal(t, "with-plan", detail.Session.Name)
	require.NotEmpty(t, detail.PlanNoteID)
}

func TestAgentHandler_GetSession_NotFound(t *testing.T) {
	svc, _ := setupTestService(t)
	h := NewHandler(svc, nil)
	w := agentAPIRequest(t, h, http.MethodGet, "/sessions/does-not-exist")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestAgentHandler_ListMemories_ProjectFilter(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	p, err := svc.cfg.ProjectService.Create(ctx, testUserID, "Proj X", "")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "protocol", "scoped", "body", "desc", p.Slug)
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "protocol", "unscoped", "body", "desc", "")
	require.NoError(t, err)

	h := NewHandler(svc, nil)
	w := agentAPIRequest(t, h, http.MethodGet, "/memories?project="+p.Slug)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Memories []MemoryItem `json:"memories"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Memories, 1)
	require.Equal(t, "scoped", resp.Memories[0].Name)
	require.Equal(t, p.Slug, resp.Memories[0].Project)
	require.NotEmpty(t, resp.Memories[0].NoteID)
}

func TestAgentHandler_Unauthorized(t *testing.T) {
	svc, _ := setupTestService(t)
	h := NewHandler(svc, nil)
	// No userID in context.
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
