package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestStore_CreateConversation(t *testing.T) {
	db := testutil.TestUserDB(t)
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
	db := testutil.TestUserDB(t)
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
	db := testutil.TestUserDB(t)
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
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestStore_AddMessage(t *testing.T) {
	db := testutil.TestUserDB(t)
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

func TestStore_GetFirstUserMessage(t *testing.T) {
	db := testutil.TestUserDB(t)
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
