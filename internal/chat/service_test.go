package chat

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/katata/seam/migrations"
)

// mockDBManager implements userdb.Manager for tests.
type mockDBManager struct {
	mu     sync.Mutex
	dbs    map[string]*sql.DB
	prefix string
}

var mockCounter uint64
var mockCounterMu sync.Mutex

func newMockDBManager() *mockDBManager {
	mockCounterMu.Lock()
	mockCounter++
	id := mockCounter
	mockCounterMu.Unlock()
	return &mockDBManager{
		dbs:    make(map[string]*sql.DB),
		prefix: fmt.Sprintf("chatmock_%d", id),
	}
}

func (m *mockDBManager) Open(_ context.Context, userID string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if db, ok := m.dbs[userID]; ok {
		return db, nil
	}

	name := fmt.Sprintf("file:%s_%s?mode=memory&cache=shared", m.prefix, userID)
	db, err := sql.Open("sqlite", name)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	db.Exec(migrations.InitialSQL)

	m.dbs[userID] = db
	return db, nil
}

func (m *mockDBManager) Close(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.dbs[userID]; ok {
		db.Close()
		delete(m.dbs, userID)
	}
	return nil
}

func (m *mockDBManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, db := range m.dbs {
		db.Close()
	}
	m.dbs = nil
	return nil
}

func (m *mockDBManager) UserNotesDir(userID string) string {
	return "/tmp/test-notes/" + userID
}

func (m *mockDBManager) UserDataDir(userID string) string {
	return "/tmp/test-data/" + userID
}

func (m *mockDBManager) ListUsers(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockDBManager) EnsureUserDirs(userID string) error {
	return nil
}

func TestService_CreateConversation(t *testing.T) {
	mgr := newMockDBManager()
	defer mgr.CloseAll()
	svc := NewService(NewStore(), mgr, nil)

	conv, err := svc.CreateConversation(context.Background(), "user1")
	require.NoError(t, err)
	require.NotEmpty(t, conv.ID)
	require.Empty(t, conv.Title)
}

func TestService_ListConversations(t *testing.T) {
	mgr := newMockDBManager()
	defer mgr.CloseAll()
	svc := NewService(NewStore(), mgr, nil)
	ctx := context.Background()

	_, err := svc.CreateConversation(ctx, "user1")
	require.NoError(t, err)
	_, err = svc.CreateConversation(ctx, "user1")
	require.NoError(t, err)

	convs, total, err := svc.ListConversations(ctx, "user1", 10, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, convs, 2)
}

func TestService_ListConversations_EnforcesLimits(t *testing.T) {
	mgr := newMockDBManager()
	defer mgr.CloseAll()
	svc := NewService(NewStore(), mgr, nil)

	// Negative limit should use default.
	_, _, err := svc.ListConversations(context.Background(), "user1", -1, 0)
	require.NoError(t, err)

	// Over 100 should be capped.
	_, _, err = svc.ListConversations(context.Background(), "user1", 200, 0)
	require.NoError(t, err)
}

func TestService_GetConversation_NotFound(t *testing.T) {
	mgr := newMockDBManager()
	defer mgr.CloseAll()
	svc := NewService(NewStore(), mgr, nil)

	_, _, err := svc.GetConversation(context.Background(), "user1", "nonexistent")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_DeleteConversation(t *testing.T) {
	mgr := newMockDBManager()
	defer mgr.CloseAll()
	svc := NewService(NewStore(), mgr, nil)
	ctx := context.Background()

	conv, err := svc.CreateConversation(ctx, "user1")
	require.NoError(t, err)

	err = svc.DeleteConversation(ctx, "user1", conv.ID)
	require.NoError(t, err)

	// Should be gone.
	_, _, err = svc.GetConversation(ctx, "user1", conv.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_AddMessage_InvalidRole(t *testing.T) {
	mgr := newMockDBManager()
	defer mgr.CloseAll()
	svc := NewService(NewStore(), mgr, nil)

	err := svc.AddMessage(context.Background(), "user1", Message{
		ConversationID: "conv1",
		Role:           "system",
		Content:        "bad",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidRole)
}

func TestService_AddMessage_AutoTitle(t *testing.T) {
	mgr := newMockDBManager()
	defer mgr.CloseAll()
	svc := NewService(NewStore(), mgr, nil)
	ctx := context.Background()

	conv, err := svc.CreateConversation(ctx, "user1")
	require.NoError(t, err)
	require.Empty(t, conv.Title)

	// Add user message.
	err = svc.AddMessage(ctx, "user1", Message{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "What is caching and how does it work?",
	})
	require.NoError(t, err)

	// Title should still be empty (auto-title only on assistant message).
	got, _, err := svc.GetConversation(ctx, "user1", conv.ID)
	require.NoError(t, err)
	require.Empty(t, got.Title)

	// Add assistant message -- triggers auto-title.
	err = svc.AddMessage(ctx, "user1", Message{
		ConversationID: conv.ID,
		Role:           "assistant",
		Content:        "Caching is...",
	})
	require.NoError(t, err)

	got, _, err = svc.GetConversation(ctx, "user1", conv.ID)
	require.NoError(t, err)
	require.Equal(t, "What is caching and how does it work?", got.Title)
}

func TestTruncateToWord(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 80, "hello"},
		{"exact", "hello world", 11, "hello world"},
		{"truncate_at_word", "hello world foo bar", 15, "hello world"},
		{"long_single_word", "abcdefghijklmnopqrstuvwxyz", 10, "abcdefghij"},
		{"whitespace", "  hello  ", 80, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToWord(tt.input, tt.maxLen)
			require.Equal(t, tt.want, got)
		})
	}
}
