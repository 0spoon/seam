package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildSynthesisMessages(t *testing.T) {
	notes := []struct{ Title, Body string }{
		{Title: "Note 1", Body: "Content one"},
		{Title: "Note 2", Body: "Content two"},
	}

	messages := BuildSynthesisMessages("summarize", notes)
	require.Len(t, messages, 2) // system + user
	require.Equal(t, "system", messages[0].Role)
	require.Contains(t, messages[0].Content, "synthesize")
	require.Equal(t, "user", messages[1].Role)
	require.Contains(t, messages[1].Content, "Note 1")
	require.Contains(t, messages[1].Content, "Note 2")
	require.Contains(t, messages[1].Content, "summarize")
}

func TestSynthesizer_Synthesize_Project(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = "The project focuses on three key themes: API design, caching, and security."
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	mockMgr := newMockDBManager()

	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Create a project and notes.
	db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"proj1", "API Project", "api-project", now, now)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, project_id, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"n1", "API Design", "proj1", "api-design.md", "Design patterns for APIs", "h1", now, now)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, project_id, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"n2", "Caching Strategy", "proj1", "caching.md", "Redis and CDN caching", "h2", now, now)

	synth := NewSynthesizer(ollama, mockMgr, "chat-model", nil)
	result, err := synth.Synthesize(ctx, "user1", SynthesizePayload{
		Scope:     "project",
		ProjectID: "proj1",
		Prompt:    "summarize key themes",
	})
	require.NoError(t, err)
	require.Contains(t, result.Response, "key themes")
}

func TestSynthesizer_Synthesize_Tag(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = "Architecture notes cover microservices and event-driven patterns."
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	mockMgr := newMockDBManager()

	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"n1", "Microservices", "micro.md", "Microservices architecture", "h1", now, now)
	db.ExecContext(ctx,
		`INSERT INTO tags (id, name) VALUES (1, 'architecture')`)
	db.ExecContext(ctx,
		`INSERT INTO note_tags (note_id, tag_id) VALUES ('n1', 1)`)

	synth := NewSynthesizer(ollama, mockMgr, "chat-model", nil)
	result, err := synth.Synthesize(ctx, "user1", SynthesizePayload{
		Scope:  "tag",
		Tag:    "architecture",
		Prompt: "what are the key decisions?",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Response)
}

func TestSynthesizer_Synthesize_InvalidScope(t *testing.T) {
	mockMgr := newMockDBManager()
	synth := NewSynthesizer(nil, mockMgr, "model", nil)

	_, err := synth.Synthesize(context.Background(), "user1", SynthesizePayload{
		Scope:  "invalid",
		Prompt: "test",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid scope")
}

func TestSynthesizer_Synthesize_NoNotes(t *testing.T) {
	mockMgr := newMockDBManager()
	synth := NewSynthesizer(nil, mockMgr, "model", nil)

	// Open DB first so project scope query works.
	mockMgr.Open(context.Background(), "user1")

	result, err := synth.Synthesize(context.Background(), "user1", SynthesizePayload{
		Scope:     "project",
		ProjectID: "nonexistent",
		Prompt:    "summarize",
	})
	require.NoError(t, err)
	require.Contains(t, result.Response, "No notes found")
}

func TestSynthesizer_HandleSynthesizeTask(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = "Summary of the project."
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	mockMgr := newMockDBManager()

	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, pErr := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"proj1", "Test Project", "test-project", now, now)
	require.NoError(t, pErr)
	_, nErr := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, project_id, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"n1", "Note", "proj1", "note.md", "content", "h", now, now)
	require.NoError(t, nErr)

	synth := NewSynthesizer(ollama, mockMgr, "model", nil)
	task := &Task{
		ID:      "task1",
		UserID:  "user1",
		Type:    TaskTypeSynthesize,
		Payload: json.RawMessage(`{"scope":"project","project_id":"proj1","prompt":"summarize"}`),
	}

	result, err := synth.HandleSynthesizeTask(ctx, task)
	require.NoError(t, err)

	var sr SynthesisResult
	require.NoError(t, json.Unmarshal(result, &sr))
	require.Equal(t, "Summary of the project.", sr.Response)
}
