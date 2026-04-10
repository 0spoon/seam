package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	seamcp "github.com/katata/seam/internal/mcp"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/review"
	"github.com/katata/seam/internal/template"
)

// --- notes_append tool tests ---

func TestNotesAppend_Success(t *testing.T) {
	mock := &mockAgentService{
		notesAppendFn: func(_ context.Context, userID, noteID, text string) (*note.Note, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "note-abc", noteID)
			require.Equal(t, "Found a critical bug in auth middleware", text)
			return &note.Note{ID: "note-abc", Title: "Debug Journal"}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_append", map[string]any{
		"id":   "note-abc",
		"text": "Found a critical bug in auth middleware",
	})

	require.False(t, result.IsError)
	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "note-abc", resp["note_id"])
}

func TestNotesAppend_MissingID(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_append", map[string]any{
		"text": "some text",
	})
	require.True(t, result.IsError)
}

func TestNotesAppend_MissingText(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_append", map[string]any{
		"id": "note-abc",
	})
	require.True(t, result.IsError)
}

func TestNotesAppend_NotFound(t *testing.T) {
	mock := &mockAgentService{
		notesAppendFn: func(context.Context, string, string, string) (*note.Note, error) {
			return nil, note.ErrNotFound
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_append", map[string]any{
		"id":   "nonexistent",
		"text": "x",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "not found")
}

// --- notes_changelog tool tests ---

func TestNotesChangelog_Success(t *testing.T) {
	mock := &mockAgentService{
		notesChangelogFn: func(_ context.Context, userID string, since, until time.Time, limit int) ([]*note.Note, int, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "2026-04-01", since.Format("2006-01-02"))
			require.Equal(t, 20, limit)
			return []*note.Note{
				{ID: "n1", Title: "Changed Note", Tags: []string{"go"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			}, 1, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_changelog", map[string]any{
		"since": "2026-04-01",
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	changes, ok := resp["changes"].([]interface{})
	require.True(t, ok)
	require.Len(t, changes, 1)
	require.Equal(t, float64(1), resp["total"])
}

func TestNotesChangelog_Defaults(t *testing.T) {
	mock := &mockAgentService{
		notesChangelogFn: func(_ context.Context, _ string, since, until time.Time, limit int) ([]*note.Note, int, error) {
			// Default: 7 days ago to now.
			require.WithinDuration(t, time.Now().AddDate(0, 0, -7), since, 2*time.Second)
			require.WithinDuration(t, time.Now(), until, 2*time.Second)
			require.Equal(t, 20, limit)
			return []*note.Note{}, 0, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_changelog", map[string]any{})
	require.False(t, result.IsError)
}

func TestNotesChangelog_InvalidDate(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_changelog", map[string]any{
		"since": "not-a-date",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "invalid since date")
}

func TestNotesChangelog_InvalidUntilDate(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_changelog", map[string]any{
		"until": "bad",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "invalid until date")
}

// --- notes_versions tool tests ---

func TestNotesVersions_ListSuccess(t *testing.T) {
	mock := &mockAgentService{
		notesVersionsFn: func(_ context.Context, userID, noteID string, limit int) ([]*note.NoteVersion, int, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "note-v1", noteID)
			require.Equal(t, 10, limit)
			return []*note.NoteVersion{
				{NoteID: "note-v1", Version: 3, Title: "Latest", CreatedAt: time.Now()},
				{NoteID: "note-v1", Version: 2, Title: "Middle", CreatedAt: time.Now().Add(-time.Hour)},
			}, 3, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_versions", map[string]any{
		"id": "note-v1",
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	versions, ok := resp["versions"].([]interface{})
	require.True(t, ok)
	require.Len(t, versions, 2)
	require.Equal(t, float64(3), resp["total"])
}

func TestNotesVersions_GetSpecific(t *testing.T) {
	mock := &mockAgentService{
		notesGetVersionFn: func(_ context.Context, userID, noteID string, version int) (*note.NoteVersion, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "note-v1", noteID)
			require.Equal(t, 2, version)
			return &note.NoteVersion{
				NoteID:  "note-v1",
				Version: 2,
				Title:   "Old Title",
				Body:    "Old body content",
				CreatedAt: time.Now().Add(-time.Hour),
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_versions", map[string]any{
		"id":      "note-v1",
		"version": float64(2),
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "Old Title", resp["title"])
	require.Equal(t, "Old body content", resp["body"])
	require.Equal(t, float64(2), resp["version"])
}

func TestNotesVersions_MissingID(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_versions", map[string]any{})
	require.True(t, result.IsError)
}

func TestNotesVersions_NotFound(t *testing.T) {
	mock := &mockAgentService{
		notesVersionsFn: func(context.Context, string, string, int) ([]*note.NoteVersion, int, error) {
			return nil, 0, note.ErrNotFound
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_versions", map[string]any{
		"id": "nonexistent",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "not found")
}

func TestNotesVersions_VersionNotFound(t *testing.T) {
	mock := &mockAgentService{
		notesGetVersionFn: func(context.Context, string, string, int) (*note.NoteVersion, error) {
			return nil, note.ErrVersionNotFound
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_versions", map[string]any{
		"id":      "note-v1",
		"version": float64(99),
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "version not found")
}

// --- graph_neighbors tool tests ---

func TestGraphNeighbors_Success(t *testing.T) {
	mock := &mockAgentService{
		notesBacklinksFn: func(_ context.Context, userID, noteID string) ([]*note.Note, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "note-g1", noteID)
			return []*note.Note{
				{ID: "n2", Title: "Linking Note"},
				{ID: "n3", Title: "Another Linker"},
			}, nil
		},
	}

	mockGraph := &mockGraphService{
		getTwoHopFn: func(_ context.Context, userID, noteID string) ([]graph.TwoHopNode, error) {
			require.Equal(t, "note-g1", noteID)
			return []graph.TwoHopNode{
				{ID: "n4", Title: "Two Hop Away", ViaID: "n2", ViaTitle: "Linking Note"},
			}, nil
		},
	}

	srv := newTestServerWithGraph(t, mock, mockGraph)
	result := directCall(t, srv, "graph_neighbors", map[string]any{
		"id": "note-g1",
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	backlinks, ok := resp["backlinks"].([]interface{})
	require.True(t, ok)
	require.Len(t, backlinks, 2)
	require.NotNil(t, resp["two_hop"])
}

func TestGraphNeighbors_NoTwoHop(t *testing.T) {
	mock := &mockAgentService{
		notesBacklinksFn: func(context.Context, string, string) ([]*note.Note, error) {
			return []*note.Note{}, nil
		},
	}

	mockGraph := &mockGraphService{}
	srv := newTestServerWithGraph(t, mock, mockGraph)
	result := directCall(t, srv, "graph_neighbors", map[string]any{
		"id":              "note-g1",
		"include_two_hop": "false",
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Nil(t, resp["two_hop"])
}

func TestGraphNeighbors_MissingID(t *testing.T) {
	mockGraph := &mockGraphService{}
	srv := newTestServerWithGraph(t, &mockAgentService{}, mockGraph)
	result := directCall(t, srv, "graph_neighbors", map[string]any{})
	require.True(t, result.IsError)
}

// --- review_queue tool tests ---

func TestReviewQueue_Success(t *testing.T) {
	mockReview := &mockReviewService{
		getQueueFn: func(_ context.Context, userID string, limit int) ([]review.ReviewItem, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, 10, limit)
			return []review.ReviewItem{
				{
					Type:        "orphan",
					NoteID:      "n-orphan",
					NoteTitle:   "Lonely Note",
					NoteSnippet: "This note has no links...",
					Suggestions: []review.Suggestion{
						{Action: "add_link", Target: "n-related", Reason: "similar content"},
					},
				},
				{
					Type:      "untagged",
					NoteID:    "n-notag",
					NoteTitle: "Untagged Note",
				},
			}, nil
		},
	}

	srv := newTestServerWithReview(t, &mockAgentService{}, mockReview)
	result := directCall(t, srv, "review_queue", map[string]any{})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	items, ok := resp["items"].([]interface{})
	require.True(t, ok)
	require.Len(t, items, 2)
	require.Equal(t, float64(2), resp["count"])
}

func TestReviewQueue_Empty(t *testing.T) {
	mockReview := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return []review.ReviewItem{}, nil
		},
	}

	srv := newTestServerWithReview(t, &mockAgentService{}, mockReview)
	result := directCall(t, srv, "review_queue", map[string]any{})
	require.False(t, result.IsError)
}

func TestReviewQueue_CustomLimit(t *testing.T) {
	mockReview := &mockReviewService{
		getQueueFn: func(_ context.Context, _ string, limit int) ([]review.ReviewItem, error) {
			require.Equal(t, 5, limit)
			return []review.ReviewItem{}, nil
		},
	}

	srv := newTestServerWithReview(t, &mockAgentService{}, mockReview)
	result := directCall(t, srv, "review_queue", map[string]any{
		"limit": float64(5),
	})
	require.False(t, result.IsError)
}

// --- notes_from_template tool tests ---

func TestNotesFromTemplate_ListTemplates(t *testing.T) {
	mockTpl := &mockTemplateService{
		listFn: func(_ context.Context, userID string) ([]template.TemplateMeta, error) {
			require.Equal(t, toolTestUser, userID)
			return []template.TemplateMeta{
				{Name: "meeting-notes", Description: "Meeting notes template"},
				{Name: "research-summary", Description: "Research summary"},
			}, nil
		},
	}

	srv := newTestServerWithTemplate(t, &mockAgentService{}, mockTpl)
	result := directCall(t, srv, "notes_from_template", map[string]any{
		"list": "true",
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	templates, ok := resp["templates"].([]interface{})
	require.True(t, ok)
	require.Len(t, templates, 2)
}

func TestNotesFromTemplate_CreateSuccess(t *testing.T) {
	mockTpl := &mockTemplateService{
		applyFn: func(_ context.Context, userID, name string, vars map[string]string) (string, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "meeting-notes", name)
			require.Equal(t, "Auth Redesign", vars["topic"])
			return "# Meeting: Auth Redesign\n\nDate: 2026-04-09\n\n## Attendees\n\n## Notes\n", nil
		},
	}

	mock := &mockAgentService{
		notesCreateFn: func(_ context.Context, userID, title, body, projectSlug string, tags []string) (*note.Note, error) {
			require.Equal(t, "Auth Meeting", title)
			require.Contains(t, body, "Auth Redesign")
			require.Equal(t, "backend", projectSlug)
			return &note.Note{ID: "n-new", Title: "Auth Meeting"}, nil
		},
	}

	srv := newTestServerWithTemplate(t, mock, mockTpl)
	result := directCall(t, srv, "notes_from_template", map[string]any{
		"template": "meeting-notes",
		"title":    "Auth Meeting",
		"project":  "backend",
		"vars":     `{"topic":"Auth Redesign"}`,
	})

	require.False(t, result.IsError)
	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "n-new", resp["note_id"])
	require.Equal(t, "meeting-notes", resp["template"])
}

func TestNotesFromTemplate_MissingTemplate(t *testing.T) {
	srv := newTestServerWithTemplate(t, &mockAgentService{}, &mockTemplateService{})
	result := directCall(t, srv, "notes_from_template", map[string]any{
		"title": "x",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "template")
}

func TestNotesFromTemplate_MissingTitle(t *testing.T) {
	mockTpl := &mockTemplateService{
		applyFn: func(context.Context, string, string, map[string]string) (string, error) {
			return "body", nil
		},
	}

	srv := newTestServerWithTemplate(t, &mockAgentService{}, mockTpl)
	result := directCall(t, srv, "notes_from_template", map[string]any{
		"template": "meeting-notes",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "title")
}

func TestNotesFromTemplate_TemplateNotFound(t *testing.T) {
	mockTpl := &mockTemplateService{
		applyFn: func(context.Context, string, string, map[string]string) (string, error) {
			return "", template.ErrTemplateNotFound
		},
	}

	srv := newTestServerWithTemplate(t, &mockAgentService{}, mockTpl)
	result := directCall(t, srv, "notes_from_template", map[string]any{
		"template": "nonexistent",
		"title":    "x",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "template not found")
}

func TestNotesFromTemplate_InvalidVarsJSON(t *testing.T) {
	mockTpl := &mockTemplateService{
		applyFn: func(context.Context, string, string, map[string]string) (string, error) {
			return "body", nil
		},
	}

	srv := newTestServerWithTemplate(t, &mockAgentService{}, mockTpl)
	result := directCall(t, srv, "notes_from_template", map[string]any{
		"template": "meeting-notes",
		"title":    "x",
		"vars":     "not-valid-json",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "invalid vars")
}

// --- Mock services ---

type mockGraphService struct {
	getGraphFn  func(ctx context.Context, userID string, filter graph.GraphFilter) (*graph.Graph, error)
	getTwoHopFn func(ctx context.Context, userID, noteID string) ([]graph.TwoHopNode, error)
	getOrphansFn func(ctx context.Context, userID string) ([]graph.Node, error)
}

func (m *mockGraphService) GetGraph(ctx context.Context, userID string, filter graph.GraphFilter) (*graph.Graph, error) {
	if m.getGraphFn == nil {
		return &graph.Graph{}, nil
	}
	return m.getGraphFn(ctx, userID, filter)
}

func (m *mockGraphService) GetTwoHopBacklinks(ctx context.Context, userID, noteID string) ([]graph.TwoHopNode, error) {
	if m.getTwoHopFn == nil {
		return []graph.TwoHopNode{}, nil
	}
	return m.getTwoHopFn(ctx, userID, noteID)
}

func (m *mockGraphService) GetOrphanNotes(ctx context.Context, userID string) ([]graph.Node, error) {
	if m.getOrphansFn == nil {
		return []graph.Node{}, nil
	}
	return m.getOrphansFn(ctx, userID)
}

type mockReviewService struct {
	getQueueFn func(ctx context.Context, userID string, limit int) ([]review.ReviewItem, error)
}

func (m *mockReviewService) GetQueue(ctx context.Context, userID string, limit int) ([]review.ReviewItem, error) {
	if m.getQueueFn == nil {
		return []review.ReviewItem{}, nil
	}
	return m.getQueueFn(ctx, userID, limit)
}

type mockTemplateService struct {
	listFn  func(ctx context.Context, userID string) ([]template.TemplateMeta, error)
	applyFn func(ctx context.Context, userID, name string, vars map[string]string) (string, error)
}

func (m *mockTemplateService) List(ctx context.Context, userID string) ([]template.TemplateMeta, error) {
	if m.listFn == nil {
		return []template.TemplateMeta{}, nil
	}
	return m.listFn(ctx, userID)
}

func (m *mockTemplateService) Apply(ctx context.Context, userID, name string, vars map[string]string) (string, error) {
	if m.applyFn == nil {
		panic("mockTemplateService.Apply not implemented")
	}
	return m.applyFn(ctx, userID, name, vars)
}

// --- Test server constructors ---

func newTestServerWithGraph(t *testing.T, mock *mockAgentService, graphMock *mockGraphService) *seamcp.Server {
	t.Helper()
	srv := seamcp.New(seamcp.Config{
		AgentService: mock,
		GraphService: graphMock,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { srv.Close() })
	return srv
}

func newTestServerWithReview(t *testing.T, mock *mockAgentService, reviewMock *mockReviewService) *seamcp.Server {
	t.Helper()
	srv := seamcp.New(seamcp.Config{
		AgentService:  mock,
		ReviewService: reviewMock,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { srv.Close() })
	return srv
}

func newTestServerWithTemplate(t *testing.T, mock *mockAgentService, tplMock *mockTemplateService) *seamcp.Server {
	t.Helper()
	srv := seamcp.New(seamcp.Config{
		AgentService:    mock,
		TemplateService: tplMock,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { srv.Close() })
	return srv
}
