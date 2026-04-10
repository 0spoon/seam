package librarian

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/review"
	"github.com/stretchr/testify/require"
)

// -- Mock implementations --

type mockNoteService struct {
	getFn      func(ctx context.Context, userID, noteID string) (*note.Note, error)
	updateFn   func(ctx context.Context, userID, noteID string, req note.UpdateNoteReq) (*note.Note, error)
	listTagsFn func(ctx context.Context, userID string) ([]note.TagCount, error)
	getCalls   map[string]int // tracks call count per noteID for content hash guard testing
}

func (m *mockNoteService) Get(ctx context.Context, userID, noteID string) (*note.Note, error) {
	if m.getCalls == nil {
		m.getCalls = make(map[string]int)
	}
	m.getCalls[noteID]++
	if m.getFn != nil {
		return m.getFn(ctx, userID, noteID)
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockNoteService) Update(ctx context.Context, userID, noteID string, req note.UpdateNoteReq) (*note.Note, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, userID, noteID, req)
	}
	return &note.Note{ID: noteID}, nil
}

func (m *mockNoteService) ListTags(ctx context.Context, userID string) ([]note.TagCount, error) {
	if m.listTagsFn != nil {
		return m.listTagsFn(ctx, userID)
	}
	return nil, nil
}

type mockProjectService struct {
	listFn func(ctx context.Context, userID string) ([]*project.Project, error)
}

func (m *mockProjectService) List(ctx context.Context, userID string) ([]*project.Project, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID)
	}
	return nil, nil
}

type mockReviewService struct {
	getQueueFn func(ctx context.Context, userID string, limit int) ([]review.ReviewItem, error)
}

func (m *mockReviewService) GetQueue(ctx context.Context, userID string, limit int) ([]review.ReviewItem, error) {
	if m.getQueueFn != nil {
		return m.getQueueFn(ctx, userID, limit)
	}
	return nil, nil
}

type mockSettingsService struct {
	getAllFn func(ctx context.Context, userID string) (map[string]string, error)
}

func (m *mockSettingsService) GetAll(ctx context.Context, userID string) (map[string]string, error) {
	if m.getAllFn != nil {
		return m.getAllFn(ctx, userID)
	}
	return map[string]string{"librarian_enabled": "true"}, nil
}

type mockChatCompleter struct {
	chatCompletionFn func(ctx context.Context, model string, messages []ai.ChatMessage) (*ai.ChatResponse, error)
}

func (m *mockChatCompleter) ChatCompletion(ctx context.Context, model string, messages []ai.ChatMessage) (*ai.ChatResponse, error) {
	if m.chatCompletionFn != nil {
		return m.chatCompletionFn(ctx, model, messages)
	}
	return &ai.ChatResponse{Content: `{"project_id":"","tags":[],"rationale":"no match"}`}, nil
}

func (m *mockChatCompleter) ChatCompletionStream(ctx context.Context, model string, messages []ai.ChatMessage) (<-chan string, <-chan error) {
	ch := make(chan string)
	errCh := make(chan error, 1)
	close(ch)
	close(errCh)
	return ch, errCh
}

// -- Helper to build a test service --

func newTestService(
	notes *mockNoteService,
	projects *mockProjectService,
	reviews *mockReviewService,
	settings *mockSettingsService,
	chat *mockChatCompleter,
) *Service {
	return NewService(Config{
		NoteService:     notes,
		ProjectService:  projects,
		ReviewService:   reviews,
		SettingsService: settings,
		Chat:            chat,
		ChatModel:       "test-model",
	})
}

func defaultNote(id string, updatedMinutesAgo int) *note.Note {
	return &note.Note{
		ID:          id,
		Title:       "Test Note " + id,
		Body:        "Some content for classification.",
		ContentHash: "hash_" + id,
		Tags:        []string{},
		UpdatedAt:   time.Now().UTC().Add(-time.Duration(updatedMinutesAgo) * time.Minute),
	}
}

func defaultProjects() []*project.Project {
	return []*project.Project{
		{ID: "proj_1", Name: "Research", Slug: "research", Description: "Research notes"},
		{ID: "proj_2", Name: "Work", Slug: "work", Description: "Work notes"},
	}
}

func defaultTags() []note.TagCount {
	return []note.TagCount{
		{Name: "golang", Count: 5},
		{Name: "design", Count: 3},
		{Name: "meeting", Count: 7},
	}
}

func llmResponse(projectID string, tags []string, rationale string) *ai.ChatResponse {
	cls := classification{ProjectID: projectID, Tags: tags, Rationale: rationale}
	b, _ := json.Marshal(cls)
	return &ai.ChatResponse{Content: string(b)}
}

// -- Tests --

func TestRun_DisabledSetting(t *testing.T) {
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			t.Fatal("review queue should not be called when disabled")
			return nil, nil
		},
	}
	settings := &mockSettingsService{
		getAllFn: func(context.Context, string) (map[string]string, error) {
			return map[string]string{"librarian_enabled": "false"}, nil
		},
	}

	svc := newTestService(nil, nil, reviews, settings, nil)
	err := svc.Run(context.Background(), "user1", nil)
	require.NoError(t, err)
}

func TestRun_NoCandidates(t *testing.T) {
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return []review.ReviewItem{}, nil
		},
	}
	projects := &mockProjectService{
		listFn: func(context.Context, string) ([]*project.Project, error) {
			return defaultProjects(), nil
		},
	}

	svc := newTestService(nil, projects, reviews, &mockSettingsService{}, nil)
	err := svc.Run(context.Background(), "user1", nil)
	require.NoError(t, err)
}

func TestRun_CooldownFilter(t *testing.T) {
	n := defaultNote("note1", 5) // updated 5 minutes ago, within 15-min cooldown
	notes := &mockNoteService{
		getFn: func(_ context.Context, _, noteID string) (*note.Note, error) {
			return n, nil
		},
	}
	updateCalled := false
	notes.updateFn = func(context.Context, string, string, note.UpdateNoteReq) (*note.Note, error) {
		updateCalled = true
		return n, nil
	}
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return []review.ReviewItem{{NoteID: "note1", Type: "inbox"}}, nil
		},
	}
	projects := &mockProjectService{
		listFn: func(context.Context, string) ([]*project.Project, error) {
			return defaultProjects(), nil
		},
	}
	tags := &mockNoteService{}
	// Override notes to also handle ListTags.
	notes.listTagsFn = func(context.Context, string) ([]note.TagCount, error) {
		return defaultTags(), nil
	}

	svc := newTestService(notes, projects, reviews, &mockSettingsService{}, nil)
	err := svc.Run(context.Background(), "user1", nil)
	require.NoError(t, err)
	require.False(t, updateCalled, "note within cooldown should not be updated")
	_ = tags // suppress unused
}

func TestRun_AlreadyReviewed(t *testing.T) {
	n := defaultNote("note1", 30)
	n.Tags = []string{"librarian:reviewed"}

	notes := &mockNoteService{
		getFn: func(_ context.Context, _, _ string) (*note.Note, error) {
			return n, nil
		},
		listTagsFn: func(context.Context, string) ([]note.TagCount, error) {
			return defaultTags(), nil
		},
	}
	updateCalled := false
	notes.updateFn = func(context.Context, string, string, note.UpdateNoteReq) (*note.Note, error) {
		updateCalled = true
		return n, nil
	}
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return []review.ReviewItem{{NoteID: "note1", Type: "inbox"}}, nil
		},
	}
	projects := &mockProjectService{
		listFn: func(context.Context, string) ([]*project.Project, error) {
			return defaultProjects(), nil
		},
	}

	svc := newTestService(notes, projects, reviews, &mockSettingsService{}, nil)
	err := svc.Run(context.Background(), "user1", nil)
	require.NoError(t, err)
	require.False(t, updateCalled, "already-reviewed note should not be updated")
}

func TestRun_ContentHashGuard(t *testing.T) {
	callCount := 0
	notes := &mockNoteService{
		getFn: func(_ context.Context, _, _ string) (*note.Note, error) {
			callCount++
			n := defaultNote("note1", 30)
			if callCount == 1 {
				n.ContentHash = "hash_original"
			} else {
				// Second call returns a different hash -- simulates concurrent edit.
				n.ContentHash = "hash_changed"
			}
			return n, nil
		},
		listTagsFn: func(context.Context, string) ([]note.TagCount, error) {
			return defaultTags(), nil
		},
	}
	updateCalled := false
	notes.updateFn = func(context.Context, string, string, note.UpdateNoteReq) (*note.Note, error) {
		updateCalled = true
		return defaultNote("note1", 30), nil
	}
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return []review.ReviewItem{{NoteID: "note1", Type: "inbox"}}, nil
		},
	}
	projects := &mockProjectService{
		listFn: func(context.Context, string) ([]*project.Project, error) {
			return defaultProjects(), nil
		},
	}
	chat := &mockChatCompleter{
		chatCompletionFn: func(context.Context, string, []ai.ChatMessage) (*ai.ChatResponse, error) {
			return llmResponse("proj_1", []string{"golang"}, "matches research"), nil
		},
	}

	svc := newTestService(notes, projects, reviews, &mockSettingsService{}, chat)
	err := svc.Run(context.Background(), "user1", nil)
	require.NoError(t, err)
	require.False(t, updateCalled, "note with changed hash should not be updated")
	require.Equal(t, 2, callCount, "note should be read twice (before and after LLM)")
}

func TestRun_HappyPath(t *testing.T) {
	n := defaultNote("note1", 30)

	notes := &mockNoteService{
		getFn: func(_ context.Context, _, _ string) (*note.Note, error) {
			return n, nil
		},
		listTagsFn: func(context.Context, string) ([]note.TagCount, error) {
			return defaultTags(), nil
		},
	}
	var capturedReq note.UpdateNoteReq
	notes.updateFn = func(_ context.Context, _, _ string, req note.UpdateNoteReq) (*note.Note, error) {
		capturedReq = req
		return n, nil
	}
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return []review.ReviewItem{{NoteID: "note1", Type: "inbox"}}, nil
		},
	}
	projects := &mockProjectService{
		listFn: func(context.Context, string) ([]*project.Project, error) {
			return defaultProjects(), nil
		},
	}
	chat := &mockChatCompleter{
		chatCompletionFn: func(context.Context, string, []ai.ChatMessage) (*ai.ChatResponse, error) {
			return llmResponse("proj_1", []string{"golang"}, "matches research"), nil
		},
	}

	svc := newTestService(notes, projects, reviews, &mockSettingsService{}, chat)
	err := svc.Run(context.Background(), "user1", nil)
	require.NoError(t, err)
	require.NotNil(t, capturedReq.ProjectID, "project should be assigned")
	require.Equal(t, "proj_1", *capturedReq.ProjectID)
	require.NotNil(t, capturedReq.Tags)
	require.Contains(t, *capturedReq.Tags, "golang")
	require.Contains(t, *capturedReq.Tags, "librarian:reviewed")
}

func TestRun_MaxPerRun(t *testing.T) {
	processed := 0
	notes := &mockNoteService{
		getFn: func(_ context.Context, _, noteID string) (*note.Note, error) {
			return defaultNote(noteID, 30), nil
		},
		listTagsFn: func(context.Context, string) ([]note.TagCount, error) {
			return defaultTags(), nil
		},
	}
	notes.updateFn = func(_ context.Context, _, _ string, _ note.UpdateNoteReq) (*note.Note, error) {
		processed++
		return &note.Note{ID: "x"}, nil
	}
	// Provide 10 candidates but limit to 3.
	var items []review.ReviewItem
	for i := range 10 {
		items = append(items, review.ReviewItem{NoteID: fmt.Sprintf("note_%d", i), Type: "inbox"})
	}
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return items, nil
		},
	}
	projects := &mockProjectService{
		listFn: func(context.Context, string) ([]*project.Project, error) {
			return defaultProjects(), nil
		},
	}
	chat := &mockChatCompleter{
		chatCompletionFn: func(context.Context, string, []ai.ChatMessage) (*ai.ChatResponse, error) {
			return llmResponse("", nil, "no match"), nil
		},
	}

	cfg := ActionConfig{MaxPerRun: 3, CooldownMinutes: 15}
	cfgJSON, _ := json.Marshal(cfg)

	svc := newTestService(notes, projects, reviews, &mockSettingsService{}, chat)
	err := svc.Run(context.Background(), "user1", cfgJSON)
	require.NoError(t, err)
	require.Equal(t, 3, processed, "should stop after MaxPerRun")
}

func TestRun_NoProjectsOrTags(t *testing.T) {
	reviews := &mockReviewService{
		getQueueFn: func(context.Context, string, int) ([]review.ReviewItem, error) {
			return []review.ReviewItem{{NoteID: "note1"}}, nil
		},
	}
	projects := &mockProjectService{
		listFn: func(context.Context, string) ([]*project.Project, error) {
			return nil, nil // no projects
		},
	}
	notes := &mockNoteService{
		listTagsFn: func(context.Context, string) ([]note.TagCount, error) {
			return nil, nil // no tags
		},
	}
	updateCalled := false
	notes.updateFn = func(context.Context, string, string, note.UpdateNoteReq) (*note.Note, error) {
		updateCalled = true
		return nil, nil
	}

	svc := newTestService(notes, projects, reviews, &mockSettingsService{}, nil)
	err := svc.Run(context.Background(), "user1", nil)
	require.NoError(t, err)
	require.False(t, updateCalled, "should short-circuit with no projects/tags")
}

func TestClassify_ParsesLLMResponse(t *testing.T) {
	content := `{"project_id":"proj_1","tags":["golang","design"],"rationale":"matches well"}`
	cls, err := parseClassification(content)
	require.NoError(t, err)
	require.Equal(t, "proj_1", cls.ProjectID)
	require.Equal(t, []string{"golang", "design"}, cls.Tags)
	require.Equal(t, "matches well", cls.Rationale)
}

func TestClassify_HandlesMalformedJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "markdown fenced",
			content: "```json\n{\"project_id\":\"proj_1\",\"tags\":[\"golang\"],\"rationale\":\"ok\"}\n```",
		},
		{
			name:    "preamble text",
			content: "Here is my classification:\n{\"project_id\":\"proj_1\",\"tags\":[\"golang\"],\"rationale\":\"ok\"}",
		},
		{
			name:    "trailing text",
			content: "{\"project_id\":\"proj_1\",\"tags\":[\"golang\"],\"rationale\":\"ok\"}\nI hope this helps!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cls, err := parseClassification(tt.content)
			require.NoError(t, err)
			require.Equal(t, "proj_1", cls.ProjectID)
			require.Equal(t, []string{"golang"}, cls.Tags)
		})
	}
}

func TestValidateClassification_FiltersUnknown(t *testing.T) {
	cls := &classification{
		ProjectID: "unknown_proj",
		Tags:      []string{"golang", "unknown_tag", "DESIGN"},
	}

	projects := defaultProjects()
	tags := defaultTags()

	result := validateClassification(cls, projects, tags)
	require.Empty(t, result.ProjectID, "unknown project should be filtered")
	require.Equal(t, []string{"golang", "design"}, result.Tags, "unknown tags filtered, case normalized")
}

func TestValidateClassification_ValidProject(t *testing.T) {
	cls := &classification{
		ProjectID: "proj_1",
		Tags:      []string{"golang"},
	}
	result := validateClassification(cls, defaultProjects(), defaultTags())
	require.Equal(t, "proj_1", result.ProjectID)
	require.Equal(t, []string{"golang"}, result.Tags)
}

func TestBuildUpdate_InboxToProject(t *testing.T) {
	n := &note.Note{
		ID:        "note1",
		ProjectID: "", // inbox
		Tags:      []string{},
	}
	cls := &classification{ProjectID: "proj_1", Tags: []string{"golang"}}

	svc := &Service{}
	req := svc.buildUpdate(n, cls)
	require.NotNil(t, req)
	require.NotNil(t, req.ProjectID)
	require.Equal(t, "proj_1", *req.ProjectID)
	require.NotNil(t, req.Tags)
	require.Contains(t, *req.Tags, "golang")
	require.Contains(t, *req.Tags, "librarian:reviewed")
}

func TestBuildUpdate_KeepsExistingProject(t *testing.T) {
	n := &note.Note{
		ID:        "note1",
		ProjectID: "proj_2", // already has a project
		Tags:      []string{},
	}
	cls := &classification{ProjectID: "proj_1", Tags: []string{"golang"}}

	svc := &Service{}
	req := svc.buildUpdate(n, cls)
	require.NotNil(t, req)
	require.Nil(t, req.ProjectID, "existing project should not be changed")
	require.Contains(t, *req.Tags, "golang")
}

func TestBuildUpdate_TagMerge(t *testing.T) {
	n := &note.Note{
		ID:   "note1",
		Tags: []string{"existing", "golang"},
	}
	cls := &classification{Tags: []string{"golang", "design"}}

	svc := &Service{}
	req := svc.buildUpdate(n, cls)
	require.NotNil(t, req)
	require.NotNil(t, req.Tags)
	tags := *req.Tags
	require.Contains(t, tags, "existing")
	require.Contains(t, tags, "golang")
	require.Contains(t, tags, "design")
	require.Contains(t, tags, "librarian:reviewed")
	// No duplicates.
	count := 0
	for _, t2 := range tags {
		if t2 == "golang" {
			count++
		}
	}
	require.Equal(t, 1, count, "golang should appear only once")
}

func TestBuildUpdate_NoClassificationChanges(t *testing.T) {
	n := &note.Note{
		ID:        "note1",
		ProjectID: "proj_1",
		Tags:      []string{"golang"},
	}
	cls := &classification{ProjectID: "proj_1", Tags: nil}

	svc := &Service{}
	req := svc.buildUpdate(n, cls)
	// buildUpdate still adds librarian:reviewed tag, so req is non-nil.
	require.NotNil(t, req)
	require.Nil(t, req.ProjectID, "project should not change")
	require.Contains(t, *req.Tags, "librarian:reviewed")
	require.Contains(t, *req.Tags, "golang")
}

func TestBuildUpdate_AlreadyFullyReviewed(t *testing.T) {
	n := &note.Note{
		ID:        "note1",
		ProjectID: "proj_1",
		Tags:      []string{"golang", "librarian:reviewed"},
	}
	cls := &classification{ProjectID: "proj_1", Tags: nil}

	svc := &Service{}
	req := svc.buildUpdate(n, cls)
	// No project change, tags already include librarian:reviewed, no new tags.
	require.Nil(t, req, "nothing to change when already fully organized")
}

func TestHelpers_HasTag(t *testing.T) {
	require.True(t, hasTag([]string{"Foo", "bar"}, "foo"))
	require.True(t, hasTag([]string{"Foo", "bar"}, "BAR"))
	require.False(t, hasTag([]string{"Foo"}, "baz"))
	require.False(t, hasTag(nil, "foo"))
}

func TestHelpers_MergeUnique(t *testing.T) {
	result := mergeUnique([]string{"a", "b"}, []string{"b", "c"})
	require.Equal(t, []string{"a", "b", "c"}, result)

	result = mergeUnique(nil, []string{"a"})
	require.Equal(t, []string{"a"}, result)

	result = mergeUnique([]string{"a"}, nil)
	require.Equal(t, []string{"a"}, result)
}

func TestHelpers_TagsEqual(t *testing.T) {
	require.True(t, tagsEqual([]string{"a", "b"}, []string{"b", "a"}))
	require.True(t, tagsEqual(nil, nil))
	require.True(t, tagsEqual([]string{}, []string{}))
	require.False(t, tagsEqual([]string{"a"}, []string{"a", "b"}))
	require.False(t, tagsEqual([]string{"a", "a"}, []string{"a", "b"}))
}
