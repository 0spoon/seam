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
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/task"
)

// --- notes_update tool tests ---

func TestNotesUpdate_Success(t *testing.T) {
	mock := &mockAgentService{
		notesUpdateFn: func(_ context.Context, userID, noteID string, title, body, projectSlug *string, tags *[]string) (*note.Note, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "note-123", noteID)
			require.NotNil(t, title)
			require.Equal(t, "Updated Title", *title)
			require.NotNil(t, body)
			require.Equal(t, "New body content", *body)
			require.Nil(t, projectSlug)
			require.Nil(t, tags)
			return &note.Note{ID: "note-123", Title: "Updated Title"}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_update", map[string]any{
		"id":    "note-123",
		"title": "Updated Title",
		"body":  "New body content",
	})

	require.False(t, result.IsError)
	txt := textOf(t, result)
	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(txt), &resp))
	require.Equal(t, "note-123", resp["note_id"])
	require.Equal(t, "Updated Title", resp["title"])
}

func TestNotesUpdate_WithTags(t *testing.T) {
	mock := &mockAgentService{
		notesUpdateFn: func(_ context.Context, _, _ string, _, _, _ *string, tags *[]string) (*note.Note, error) {
			require.NotNil(t, tags)
			require.Equal(t, []string{"go", "refactor"}, *tags)
			return &note.Note{ID: "n1", Title: "T"}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_update", map[string]any{
		"id":   "n1",
		"tags": "go, refactor",
	})
	require.False(t, result.IsError)
}

func TestNotesUpdate_WithProject(t *testing.T) {
	mock := &mockAgentService{
		notesUpdateFn: func(_ context.Context, _, _ string, _, _, projectSlug *string, _ *[]string) (*note.Note, error) {
			require.NotNil(t, projectSlug)
			require.Equal(t, "my-project", *projectSlug)
			return &note.Note{ID: "n1", Title: "T"}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_update", map[string]any{
		"id":      "n1",
		"project": "my-project",
	})
	require.False(t, result.IsError)
}

func TestNotesUpdate_MissingID(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_update", map[string]any{
		"title": "New Title",
	})
	require.True(t, result.IsError)
}

func TestNotesUpdate_NoFieldsProvided(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_update", map[string]any{
		"id": "note-123",
	})
	require.True(t, result.IsError)
	txt := textOf(t, result)
	require.Contains(t, txt, "at least one field")
}

func TestNotesUpdate_NotFound(t *testing.T) {
	mock := &mockAgentService{
		notesUpdateFn: func(context.Context, string, string, *string, *string, *string, *[]string) (*note.Note, error) {
			return nil, note.ErrNotFound
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_update", map[string]any{
		"id":    "nonexistent",
		"title": "x",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "not found")
}

func TestNotesUpdate_BodyTooLong(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	longBody := make([]byte, 512*1024+1)
	for i := range longBody {
		longBody[i] = 'x'
	}
	result := directCall(t, srv, "notes_update", map[string]any{
		"id":   "n1",
		"body": string(longBody),
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "body too long")
}

// --- notes_delete tool tests ---

func TestNotesDelete_Success(t *testing.T) {
	mock := &mockAgentService{
		notesDeleteFn: func(_ context.Context, userID, noteID string) error {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "note-456", noteID)
			return nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_delete", map[string]any{
		"id": "note-456",
	})

	require.False(t, result.IsError)
	require.Contains(t, textOf(t, result), "deleted")
}

func TestNotesDelete_NotFound(t *testing.T) {
	mock := &mockAgentService{
		notesDeleteFn: func(context.Context, string, string) error {
			return note.ErrNotFound
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_delete", map[string]any{
		"id": "nonexistent",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "not found")
}

func TestNotesDelete_MissingID(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_delete", map[string]any{})
	require.True(t, result.IsError)
}

// --- notes_tags tool tests ---

func TestNotesTags_Success(t *testing.T) {
	mock := &mockAgentService{
		notesTagsFn: func(_ context.Context, userID string) ([]note.TagCount, error) {
			require.Equal(t, toolTestUser, userID)
			return []note.TagCount{
				{Name: "go", Count: 15},
				{Name: "architecture", Count: 8},
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_tags", map[string]any{})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	tags, ok := resp["tags"].([]interface{})
	require.True(t, ok)
	require.Len(t, tags, 2)
}

func TestNotesTags_Empty(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_tags", map[string]any{})
	require.False(t, result.IsError)
}

// --- notes_daily tool tests ---

func TestNotesDaily_Success_DefaultDate(t *testing.T) {
	mock := &mockAgentService{
		notesDailyFn: func(_ context.Context, userID string, date time.Time) (*note.Note, error) {
			require.Equal(t, toolTestUser, userID)
			// Date should be today.
			require.Equal(t, time.Now().Format("2006-01-02"), date.Format("2006-01-02"))
			return &note.Note{
				ID:    "daily-1",
				Title: date.Format("2006-01-02") + " Daily Note",
				Body:  "",
				Tags:  []string{"daily"},
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_daily", map[string]any{})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "daily-1", resp["id"])
}

func TestNotesDaily_WithDate(t *testing.T) {
	mock := &mockAgentService{
		notesDailyFn: func(_ context.Context, _ string, date time.Time) (*note.Note, error) {
			require.Equal(t, "2026-03-15", date.Format("2006-01-02"))
			return &note.Note{
				ID:    "daily-2",
				Title: "2026-03-15 Daily Note",
				Body:  "existing content",
				Tags:  []string{"daily"},
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "notes_daily", map[string]any{
		"date": "2026-03-15",
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "daily-2", resp["id"])
}

func TestNotesDaily_InvalidDate(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "notes_daily", map[string]any{
		"date": "not-a-date",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "invalid date format")
}

// --- project_list tool tests ---

func TestProjectList_Success(t *testing.T) {
	mock := &mockAgentService{
		projectListFn: func(_ context.Context, userID string) ([]*project.Project, error) {
			require.Equal(t, toolTestUser, userID)
			return []*project.Project{
				{ID: "p1", Name: "Backend", Slug: "backend", Description: "Backend services"},
				{ID: "p2", Name: "Frontend", Slug: "frontend"},
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "project_list", map[string]any{})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	projects, ok := resp["projects"].([]interface{})
	require.True(t, ok)
	require.Len(t, projects, 2)
}

func TestProjectList_Empty(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "project_list", map[string]any{})
	require.False(t, result.IsError)
}

// --- project_create tool tests ---

func TestProjectCreate_Success(t *testing.T) {
	mock := &mockAgentService{
		projectCreateFn: func(_ context.Context, userID, name, description string) (*project.Project, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "New Project", name)
			require.Equal(t, "A cool project", description)
			return &project.Project{
				ID:   "p-new",
				Name: "New Project",
				Slug: "new-project",
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "project_create", map[string]any{
		"name":        "New Project",
		"description": "A cool project",
	})

	require.False(t, result.IsError)
	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "p-new", resp["project_id"])
	require.Equal(t, "New Project", resp["name"])
	require.Equal(t, "new-project", resp["slug"])
}

func TestProjectCreate_MissingName(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "project_create", map[string]any{})
	require.True(t, result.IsError)
}

func TestProjectCreate_SlugExists(t *testing.T) {
	mock := &mockAgentService{
		projectCreateFn: func(context.Context, string, string, string) (*project.Project, error) {
			return nil, project.ErrSlugExists
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "project_create", map[string]any{
		"name": "Existing",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "slug already exists")
}

// --- tasks_toggle tool tests ---

func TestTasksToggle_Success(t *testing.T) {
	mockTask := &mockTaskService{
		toggleDoneFn: func(_ context.Context, userID, taskID string, done bool) error {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "task-789", taskID)
			require.True(t, done)
			return nil
		},
	}

	mock := &mockAgentService{}
	srv := newTestServerWithTasks(t, mock, mockTask)
	result := directCall(t, srv, "tasks_toggle", map[string]any{
		"id":   "task-789",
		"done": "true",
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "task-789", resp["task_id"])
	require.Equal(t, true, resp["done"])
}

func TestTasksToggle_MarkUndone(t *testing.T) {
	mockTask := &mockTaskService{
		toggleDoneFn: func(_ context.Context, _, _ string, done bool) error {
			require.False(t, done)
			return nil
		},
	}

	mock := &mockAgentService{}
	srv := newTestServerWithTasks(t, mock, mockTask)
	result := directCall(t, srv, "tasks_toggle", map[string]any{
		"id":   "task-789",
		"done": "false",
	})
	require.False(t, result.IsError)
}

func TestTasksToggle_NotFound(t *testing.T) {
	mockTask := &mockTaskService{
		toggleDoneFn: func(context.Context, string, string, bool) error {
			return task.ErrNotFound
		},
	}

	mock := &mockAgentService{}
	srv := newTestServerWithTasks(t, mock, mockTask)
	result := directCall(t, srv, "tasks_toggle", map[string]any{
		"id":   "nonexistent",
		"done": "true",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "task not found")
}

func TestTasksToggle_MissingID(t *testing.T) {
	mockTask := &mockTaskService{}
	srv := newTestServerWithTasks(t, &mockAgentService{}, mockTask)
	result := directCall(t, srv, "tasks_toggle", map[string]any{
		"done": "true",
	})
	require.True(t, result.IsError)
}

func TestTasksToggle_MissingDone(t *testing.T) {
	mockTask := &mockTaskService{}
	srv := newTestServerWithTasks(t, &mockAgentService{}, mockTask)
	result := directCall(t, srv, "tasks_toggle", map[string]any{
		"id": "task-1",
	})
	require.True(t, result.IsError)
}

// --- Test Helpers ---

// mockTaskService implements seamcp.TaskService for testing.
type mockTaskService struct {
	listFn       func(ctx context.Context, userID string, filter task.TaskFilter) ([]*task.Task, int, error)
	summaryFn    func(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error)
	toggleDoneFn func(ctx context.Context, userID, taskID string, done bool) error
}

func (m *mockTaskService) List(ctx context.Context, userID string, filter task.TaskFilter) ([]*task.Task, int, error) {
	if m.listFn == nil {
		return []*task.Task{}, 0, nil
	}
	return m.listFn(ctx, userID, filter)
}

func (m *mockTaskService) Summary(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error) {
	if m.summaryFn == nil {
		return &task.TaskSummary{}, nil
	}
	return m.summaryFn(ctx, userID, filter)
}

func (m *mockTaskService) ToggleDone(ctx context.Context, userID, taskID string, done bool) error {
	if m.toggleDoneFn == nil {
		panic("mockTaskService.ToggleDone not implemented")
	}
	return m.toggleDoneFn(ctx, userID, taskID, done)
}

// newTestServerWithTasks creates a seamcp.Server with both agent and task service mocks.
func newTestServerWithTasks(t *testing.T, mock *mockAgentService, taskMock *mockTaskService) *seamcp.Server {
	t.Helper()
	srv := seamcp.New(seamcp.Config{
		AgentService: mock,
		TaskService:  taskMock,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { srv.Close() })
	return srv
}
