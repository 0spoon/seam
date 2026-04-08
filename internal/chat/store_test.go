package chat

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestStore_CreateConversation(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	conv := Conversation{
		ID:        "conv1",
		Title:     "Test conversation",
		CreatedAt: "2026-03-09T00:00:00Z",
		UpdatedAt: "2026-03-09T00:00:00Z",
	}

	err := store.CreateConversation(ctx, db, conv)
	require.NoError(t, err)

	// Verify it was created.
	got, msgs, err := store.GetConversation(ctx, db, "conv1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "conv1", got.ID)
	require.Equal(t, "Test conversation", got.Title)
	require.Empty(t, msgs)
}

func TestStore_ListConversations(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	// Create two conversations.
	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv1", Title: "First", CreatedAt: "2026-03-09T00:00:00Z", UpdatedAt: "2026-03-09T00:00:00Z",
	}))
	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv2", Title: "Second", CreatedAt: "2026-03-09T01:00:00Z", UpdatedAt: "2026-03-09T01:00:00Z",
	}))

	convs, total, err := store.ListConversations(ctx, db, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, convs, 2)
	// Most recent first.
	require.Equal(t, "conv2", convs[0].ID)
	require.Equal(t, "conv1", convs[1].ID)

	// Test pagination.
	convs, total, err = store.ListConversations(ctx, db, 1, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, convs, 1)
	require.Equal(t, "conv2", convs[0].ID)
}

func TestStore_DeleteConversation(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv1", Title: "Delete me", CreatedAt: "2026-03-09T00:00:00Z", UpdatedAt: "2026-03-09T00:00:00Z",
	}))

	// Add a message.
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID: "msg1", ConversationID: "conv1", Role: "user", Content: "Hello", CreatedAt: "2026-03-09T00:00:01Z",
	}))

	// Delete should cascade.
	require.NoError(t, store.DeleteConversation(ctx, db, "conv1"))

	got, _, err := store.GetConversation(ctx, db, "conv1")
	require.ErrorIs(t, err, ErrNotFound)
	require.Nil(t, got)
}

func TestStore_AddMessage(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv1", Title: "", CreatedAt: "2026-03-09T00:00:00Z", UpdatedAt: "2026-03-09T00:00:00Z",
	}))

	// Add user message.
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID: "msg1", ConversationID: "conv1", Role: "user", Content: "What is caching?", CreatedAt: "2026-03-09T00:00:01Z",
	}))

	// Add assistant message with citations.
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID: "msg2", ConversationID: "conv1", Role: "assistant", Content: "Caching is...",
		Citations: []Citation{{ID: "note1", Title: "Caching Guide"}},
		CreatedAt: "2026-03-09T00:00:02Z",
	}))

	// Verify messages.
	_, msgs, err := store.GetConversation(ctx, db, "conv1")
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, "user", msgs[0].Role)
	require.Equal(t, "What is caching?", msgs[0].Content)
	require.Equal(t, "assistant", msgs[1].Role)
	require.Len(t, msgs[1].Citations, 1)
	require.Equal(t, "note1", msgs[1].Citations[0].ID)
	require.Equal(t, "Caching Guide", msgs[1].Citations[0].Title)
}

func TestStore_AddMessage_ToolFields(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv1", Title: "", CreatedAt: "2026-03-09T00:00:00Z", UpdatedAt: "2026-03-09T00:00:00Z",
	}))

	// Assistant turn envelope with two tool calls at iteration 3.
	toolCalls := []ToolCall{
		{ID: "call_1", Name: "search_notes", Arguments: `{"query":"kubernetes"}`},
		{ID: "call_2", Name: "get_current_time", Arguments: `{}`},
	}
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID:             "msg_assistant",
		ConversationID: "conv1",
		Role:           "assistant",
		Content:        "looking it up",
		ToolCalls:      toolCalls,
		Iteration:      3,
		CreatedAt:      "2026-03-09T00:00:01Z",
	}))

	// Tool result message referencing call_1.
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID:             "msg_tool",
		ConversationID: "conv1",
		Role:           "tool",
		Content:        `{"results":[]}`,
		ToolCallID:     "call_1",
		ToolName:       "search_notes",
		Iteration:      3,
		CreatedAt:      "2026-03-09T00:00:02Z",
	}))

	// System marker (e.g. confirmation prompt).
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID:             "msg_system",
		ConversationID: "conv1",
		Role:           "system",
		Content:        "max iterations reached",
		Iteration:      3,
		CreatedAt:      "2026-03-09T00:00:03Z",
	}))

	_, msgs, err := store.GetConversation(ctx, db, "conv1")
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	// Ordering by created_at should match insertion order.
	require.Equal(t, "msg_assistant", msgs[0].ID)
	require.Equal(t, "assistant", msgs[0].Role)
	require.Equal(t, "looking it up", msgs[0].Content)
	require.Equal(t, 3, msgs[0].Iteration)
	require.Len(t, msgs[0].ToolCalls, 2)
	require.Equal(t, toolCalls[0], msgs[0].ToolCalls[0])
	require.Equal(t, toolCalls[1], msgs[0].ToolCalls[1])
	require.Empty(t, msgs[0].ToolCallID)
	require.Empty(t, msgs[0].ToolName)

	require.Equal(t, "msg_tool", msgs[1].ID)
	require.Equal(t, "tool", msgs[1].Role)
	require.Equal(t, `{"results":[]}`, msgs[1].Content)
	require.Equal(t, "call_1", msgs[1].ToolCallID)
	require.Equal(t, "search_notes", msgs[1].ToolName)
	require.Equal(t, 3, msgs[1].Iteration)
	require.Empty(t, msgs[1].ToolCalls)

	require.Equal(t, "msg_system", msgs[2].ID)
	require.Equal(t, "system", msgs[2].Role)
	require.Equal(t, "max iterations reached", msgs[2].Content)
	require.Equal(t, 3, msgs[2].Iteration)
	require.Empty(t, msgs[2].ToolCalls)
	require.Empty(t, msgs[2].ToolCallID)
	require.Empty(t, msgs[2].ToolName)
}

func TestStore_AddMessage_NullToolFields(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv1", Title: "", CreatedAt: "2026-03-09T00:00:00Z", UpdatedAt: "2026-03-09T00:00:00Z",
	}))

	// User message with all tool fields zero/empty.
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID:             "msg1",
		ConversationID: "conv1",
		Role:           "user",
		Content:        "hello",
		CreatedAt:      "2026-03-09T00:00:01Z",
	}))

	_, msgs, err := store.GetConversation(ctx, db, "conv1")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	got := msgs[0]
	require.Equal(t, "user", got.Role)
	require.Equal(t, "hello", got.Content)
	require.Empty(t, got.ToolCalls)
	require.Nil(t, got.ToolCalls)
	require.Equal(t, "", got.ToolCallID)
	require.Equal(t, "", got.ToolName)
	require.Equal(t, 0, got.Iteration)

	// And the row should store NULL (not empty string) for tool_call_id/tool_name.
	var toolCallID, toolName sql.NullString
	err = db.QueryRowContext(ctx,
		`SELECT tool_call_id, tool_name FROM messages WHERE id = ?`, "msg1",
	).Scan(&toolCallID, &toolName)
	require.NoError(t, err)
	require.False(t, toolCallID.Valid, "expected NULL tool_call_id")
	require.False(t, toolName.Valid, "expected NULL tool_name")
}

func TestStore_SearchMessages_ToolMessages(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv1", Title: "", CreatedAt: "2026-03-09T00:00:00Z", UpdatedAt: "2026-03-09T00:00:00Z",
	}))

	// Unique substring to make the search deterministic.
	const needle = "xylophone_needle_42"
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID:             "msg_tool",
		ConversationID: "conv1",
		Role:           "tool",
		Content:        "tool result containing " + needle + " somewhere",
		ToolCallID:     "call_abc",
		ToolName:       "search_notes",
		Iteration:      2,
		CreatedAt:      "2026-03-09T00:00:01Z",
	}))

	// Noise message that must NOT match the search.
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID:             "msg_user",
		ConversationID: "conv1",
		Role:           "user",
		Content:        "unrelated content",
		CreatedAt:      "2026-03-09T00:00:02Z",
	}))

	results, err := store.SearchMessages(ctx, db, needle, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)

	hit := results[0]
	require.Equal(t, "msg_tool", hit.ID)
	require.Equal(t, "tool", hit.Role)
	require.Equal(t, "call_abc", hit.ToolCallID)
	require.Equal(t, "search_notes", hit.ToolName)
	require.Equal(t, 2, hit.Iteration)
	require.Contains(t, hit.Content, needle)
}

func TestStore_GetFirstUserMessage(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.CreateConversation(ctx, db, Conversation{
		ID: "conv1", Title: "", CreatedAt: "2026-03-09T00:00:00Z", UpdatedAt: "2026-03-09T00:00:00Z",
	}))

	// No messages yet.
	content, err := store.GetFirstUserMessage(ctx, db, "conv1")
	require.NoError(t, err)
	require.Empty(t, content)

	// Add messages.
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID: "msg1", ConversationID: "conv1", Role: "user", Content: "First question", CreatedAt: "2026-03-09T00:00:01Z",
	}))
	require.NoError(t, store.AddMessage(ctx, db, Message{
		ID: "msg2", ConversationID: "conv1", Role: "user", Content: "Second question", CreatedAt: "2026-03-09T00:00:02Z",
	}))

	content, err = store.GetFirstUserMessage(ctx, db, "conv1")
	require.NoError(t, err)
	require.Equal(t, "First question", content)
}
