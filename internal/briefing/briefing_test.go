package briefing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/task"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockNoteService struct {
	listNotes     []*note.Note
	listTotal     int
	listErr       error
	created       *note.CreateNoteReq
	updated       *note.UpdateNoteReq
	updatedNoteID string
	dedupeNotes   []*note.Note // returned when filter.ProjectID is set (existing-for-today lookup)
	dedupeOnce    bool         // serve dedupeNotes only on the first matching call
	dedupeServed  bool
}

func (m *mockNoteService) List(ctx context.Context, userID string, filter note.NoteFilter) ([]*note.Note, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	if filter.ProjectID != "" && len(m.dedupeNotes) > 0 && (!m.dedupeOnce || !m.dedupeServed) {
		m.dedupeServed = true
		return m.dedupeNotes, len(m.dedupeNotes), nil
	}
	return m.listNotes, m.listTotal, nil
}
func (m *mockNoteService) Create(ctx context.Context, userID string, req note.CreateNoteReq) (*note.Note, error) {
	cp := req
	m.created = &cp
	return &note.Note{
		ID:        "note-id",
		Title:     req.Title,
		Body:      req.Body,
		ProjectID: req.ProjectID,
		Tags:      req.Tags,
	}, nil
}
func (m *mockNoteService) Update(ctx context.Context, userID, noteID string, req note.UpdateNoteReq) (*note.Note, error) {
	cp := req
	m.updated = &cp
	m.updatedNoteID = noteID
	body := ""
	if req.Body != nil {
		body = *req.Body
	}
	return &note.Note{
		ID:    noteID,
		Title: "updated",
		Body:  body,
	}, nil
}

type mockProjectService struct {
	existing *project.Project
	getErr   error
	created  *project.Project
}

func (m *mockProjectService) GetBySlug(ctx context.Context, userID, slug string) (*project.Project, error) {
	if m.existing != nil {
		return m.existing, nil
	}
	if m.getErr != nil {
		return nil, m.getErr
	}
	return nil, project.ErrNotFound
}
func (m *mockProjectService) Create(ctx context.Context, userID, name, description string) (*project.Project, error) {
	p := &project.Project{ID: "proj-1", Slug: "briefings", Name: name}
	m.created = p
	return p, nil
}

type mockTaskService struct {
	listTasks []*task.Task
	listTotal int
	listErr   error
}

func (m *mockTaskService) List(ctx context.Context, userID string, filter task.TaskFilter) ([]*task.Task, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.listTasks, m.listTotal, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRenderBriefing_EmptyData(t *testing.T) {
	now := time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC)
	body := renderBriefing(briefingData{}, now, 24)

	require.Contains(t, body, "Daily Briefing")
	require.Contains(t, body, "## Recent activity")
	require.Contains(t, body, "_No notes created or modified in this window._")
	require.Contains(t, body, "## Open tasks")
	require.Contains(t, body, "_No open tasks._")
	require.Contains(t, body, "## Suggested actions")
}

func TestRenderBriefing_WithContent(t *testing.T) {
	now := time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC)
	data := briefingData{
		RecentNotes: []*note.Note{
			{ID: "n1", Title: "Distributed systems thoughts", UpdatedAt: now.Add(-2 * time.Hour)},
			{ID: "n2", Title: "Vacation planning", UpdatedAt: now.Add(-30 * time.Minute)},
		},
		NoteCount: 2,
		OpenTasks: []*task.Task{
			{ID: "t1", NoteID: "n1", Content: "Read Raft paper", UpdatedAt: now},
			{ID: "t2", NoteID: "n2", Content: "Book flights", UpdatedAt: now.Add(-1 * time.Hour)},
		},
		OpenCount: 2,
	}

	body := renderBriefing(data, now, 24)

	require.Contains(t, body, "[[Distributed systems thoughts]]")
	require.Contains(t, body, "[[Vacation planning]]")
	require.Contains(t, body, "Read Raft paper")
	require.Contains(t, body, "Book flights")
	require.Contains(t, body, "2 open task(s)")
}

func TestRenderBriefing_SuggestActions_ManyOpenTasks(t *testing.T) {
	data := briefingData{OpenCount: 12}
	body := renderBriefing(data, time.Now().UTC(), 24)
	require.Contains(t, body, "12 open tasks")
}

func TestRenderBriefing_DegradedWithErrors(t *testing.T) {
	data := briefingData{RecentErrors: []string{"Recent notes unavailable."}}
	body := renderBriefing(data, time.Now().UTC(), 24)
	require.Contains(t, body, "Recent notes unavailable.")
}

func TestService_Generate_HappyPath(t *testing.T) {
	notes := &mockNoteService{
		listNotes: []*note.Note{
			{ID: "n1", Title: "Yesterday's note", UpdatedAt: time.Now().UTC()},
		},
		listTotal: 1,
	}
	tasks := &mockTaskService{
		listTasks: []*task.Task{
			{ID: "t1", NoteID: "n1", Content: "Do thing", UpdatedAt: time.Now().UTC()},
		},
		listTotal: 1,
	}
	projects := &mockProjectService{}

	svc := NewService(Config{
		NoteService:    notes,
		TaskService:    tasks,
		ProjectService: projects,
	})

	created, err := svc.Generate(context.Background(), "default", json.RawMessage(`{"lookback_hours":12}`))
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Contains(t, created.Title, "Daily Briefing")
	require.Equal(t, "proj-1", created.ProjectID)
	require.Equal(t, []string{"briefing", "daily"}, created.Tags)

	// Project was auto-created.
	require.NotNil(t, projects.created)

	// Note Create was called with our rendered body.
	require.NotNil(t, notes.created)
	require.Contains(t, notes.created.Body, "[[Yesterday's note]]")
	require.Contains(t, notes.created.Body, "Do thing")
}

func TestService_Generate_ReusesExistingProject(t *testing.T) {
	existing := &project.Project{ID: "proj-existing", Slug: "briefings", Name: "briefings"}
	notes := &mockNoteService{}
	tasks := &mockTaskService{}
	projects := &mockProjectService{existing: existing}

	svc := NewService(Config{
		NoteService:    notes,
		TaskService:    tasks,
		ProjectService: projects,
	})

	created, err := svc.Generate(context.Background(), "default", nil)
	require.NoError(t, err)
	require.Equal(t, "proj-existing", created.ProjectID)
	require.Nil(t, projects.created, "should not have created a duplicate project")
}

func TestService_Generate_TolerantOfTaskListError(t *testing.T) {
	notes := &mockNoteService{listNotes: []*note.Note{{ID: "n1", Title: "Note"}}, listTotal: 1}
	tasks := &mockTaskService{listErr: errors.New("db down")}
	projects := &mockProjectService{}

	svc := NewService(Config{
		NoteService:    notes,
		TaskService:    tasks,
		ProjectService: projects,
	})

	created, err := svc.Generate(context.Background(), "default", nil)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.True(t, strings.Contains(notes.created.Body, "Open tasks unavailable"))
}

func TestActionConfig_Defaults(t *testing.T) {
	c := ActionConfig{}
	c.applyDefaults()
	require.Equal(t, "briefings", c.ProjectSlug)
	require.Equal(t, 24, c.LookbackHours)
	require.Equal(t, 10, c.MaxNotes)
	require.Equal(t, 20, c.MaxTasks)
}

func TestService_Action_RunnerAdapter(t *testing.T) {
	notes := &mockNoteService{}
	tasks := &mockTaskService{}
	projects := &mockProjectService{}
	svc := NewService(Config{
		NoteService:    notes,
		TaskService:    tasks,
		ProjectService: projects,
	})

	runner := svc.Action()
	err := runner(context.Background(), "default", nil)
	require.NoError(t, err)
	require.NotNil(t, notes.created)
}

func TestGroupTasksByNote_StableOrder(t *testing.T) {
	now := time.Now().UTC()
	tasks := []*task.Task{
		{ID: "t1", NoteID: "n1", Content: "a", UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: "t2", NoteID: "n1", Content: "b", UpdatedAt: now.Add(-1 * time.Hour)},
		{ID: "t3", NoteID: "n2", Content: "c", UpdatedAt: now},
	}
	groups := groupTasksByNote(tasks)
	require.Len(t, groups, 2)

	// most-recent timestamps per note should be set
	for _, g := range groups {
		require.False(t, g.mostRecent.IsZero())
	}
}
