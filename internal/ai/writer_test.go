package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

// fixedDBManager wraps a single *sql.DB for all users.
// Used in writer tests where we need a pre-populated DB.
type fixedDBManager struct {
	db *sql.DB
}

func (m *fixedDBManager) Open(_ context.Context, _ string) (*sql.DB, error) {
	return m.db, nil
}
func (m *fixedDBManager) Close(_ string) error                          { return nil }
func (m *fixedDBManager) CloseAll() error                               { return nil }
func (m *fixedDBManager) UserNotesDir(_ string) string                  { return "/tmp/test-notes" }
func (m *fixedDBManager) UserDataDir(_ string) string                   { return "/tmp/test-data" }
func (m *fixedDBManager) ListUsers(_ context.Context) ([]string, error) { return nil, nil }
func (m *fixedDBManager) EnsureUserDirs(_ string) error                 { return nil }

func TestWriter_Assist_Expand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		require.Equal(t, "test-model", req.Model)
		require.False(t, req.Stream)

		// Verify prompt contains expand instruction.
		found := false
		for _, msg := range req.Messages {
			if msg.Role == "user" && len(msg.Content) > 0 {
				require.Contains(t, msg.Content, "Expand")
				require.Contains(t, msg.Content, "bullet points to expand")
				found = true
			}
		}
		require.True(t, found)

		json.NewEncoder(w).Encode(ollamaChatResponse{
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: "The expanded text with more details and paragraphs.",
			},
			Done: true,
		})
	}))
	defer srv.Close()

	ollama := NewOllamaClient(srv.URL, 30e9, 120e9)

	db := testutil.TestDB(t)
	_, err := db.Exec(
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Test Note", "test.md", "original body", "hash", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	)
	require.NoError(t, err)

	mgr := &fixedDBManager{db: db}
	writer := NewWriter(ollama, mgr, "test-model", nil)

	result, err := writer.Assist(context.Background(), "user1", "note1", ActionExpand, "bullet points to expand")
	require.NoError(t, err)
	require.Equal(t, "The expanded text with more details and paragraphs.", result)
}

func TestWriter_Assist_Summarize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: "- Key point 1\n- Key point 2",
			},
			Done: true,
		})
	}))
	defer srv.Close()

	ollama := NewOllamaClient(srv.URL, 30e9, 120e9)
	mgr := &fixedDBManager{db: nil}
	writer := NewWriter(ollama, mgr, "test-model", nil)

	result, err := writer.Assist(context.Background(), "user1", "note1", ActionSummarize, "long text to summarize")
	require.NoError(t, err)
	require.Contains(t, result, "Key point")
}

func TestWriter_Assist_ExtractActions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: "- [ ] Complete the review\n- [ ] Send the report by Friday",
			},
			Done: true,
		})
	}))
	defer srv.Close()

	ollama := NewOllamaClient(srv.URL, 30e9, 120e9)
	mgr := &fixedDBManager{db: nil}
	writer := NewWriter(ollama, mgr, "test-model", nil)

	result, err := writer.Assist(context.Background(), "user1", "note1", ActionExtractActions, "meeting notes with tasks")
	require.NoError(t, err)
	require.Contains(t, result, "- [ ]")
}

func TestWriter_Assist_InvalidAction(t *testing.T) {
	ollama := NewOllamaClient("http://localhost:0", 30e9, 120e9)
	mgr := &fixedDBManager{db: nil}
	writer := NewWriter(ollama, mgr, "test-model", nil)

	_, err := writer.Assist(context.Background(), "user1", "note1", "invalid-action", "some text")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidAction)
}

func TestWriter_Assist_EmptySelection_LoadsNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: "summarized body content",
			},
			Done: true,
		})
	}))
	defer srv.Close()

	ollama := NewOllamaClient(srv.URL, 30e9, 120e9)

	db := testutil.TestDB(t)
	_, err := db.Exec(
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note2", "Full Note", "full.md", "This is the full note body to be summarized.", "hash2", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	)
	require.NoError(t, err)

	mgr := &fixedDBManager{db: db}
	writer := NewWriter(ollama, mgr, "test-model", nil)

	result, err := writer.Assist(context.Background(), "user1", "note2", ActionSummarize, "")
	require.NoError(t, err)
	require.Equal(t, "summarized body content", result)
}

func TestWriter_Assist_NoteNotFound(t *testing.T) {
	db := testutil.TestDB(t)
	mgr := &fixedDBManager{db: db}
	ollama := NewOllamaClient("http://localhost:0", 30e9, 120e9)
	writer := NewWriter(ollama, mgr, "test-model", nil)

	_, err := writer.Assist(context.Background(), "user1", "nonexistent", ActionSummarize, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestBuildAssistPrompt(t *testing.T) {
	tests := []struct {
		action      string
		expectError bool
		contains    string
	}{
		{ActionExpand, false, "Expand"},
		{ActionSummarize, false, "Summarize"},
		{ActionExtractActions, false, "Extract all action items"},
		{"invalid", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			prompt, err := buildAssistPrompt(tt.action, "test text")
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Contains(t, prompt, tt.contains)
				require.Contains(t, prompt, "test text")
			}
		})
	}
}
