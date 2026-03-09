// Package chat manages chat conversation history in per-user SQLite databases.
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// DBTX is an interface satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Conversation represents a chat conversation.
type Conversation struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Citation represents a cited note in a message.
// NOTE: This type is structurally identical to ai.Citation. They are kept
// separate to avoid an import cycle (chat <-> ai), but any field changes
// must be mirrored in both packages.
type Citation struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Message represents a single message in a conversation.
type Message struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	Role           string     `json:"role"`
	Content        string     `json:"content"`
	Citations      []Citation `json:"citations,omitempty"`
	CreatedAt      string     `json:"created_at"`
}

// Store provides data access for chat conversations and messages.
type Store struct{}

// NewStore creates a new chat Store.
func NewStore() *Store {
	return &Store{}
}

// CreateConversation inserts a new conversation.
func (s *Store) CreateConversation(ctx context.Context, db DBTX, conv Conversation) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO conversations (id, title, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		conv.ID, conv.Title, conv.CreatedAt, conv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("chat.Store.CreateConversation: %w", err)
	}
	return nil
}

// ListConversations returns conversations ordered by most recent first.
// Returns the conversations and total count for pagination.
func (s *Store) ListConversations(ctx context.Context, db DBTX, limit, offset int) ([]Conversation, int, error) {
	// Get total count.
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("chat.Store.ListConversations: count: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations
		 ORDER BY updated_at DESC
		 LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("chat.Store.ListConversations: %w", err)
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("chat.Store.ListConversations: scan: %w", err)
		}
		convs = append(convs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("chat.Store.ListConversations: rows: %w", err)
	}
	return convs, total, nil
}

// GetConversation retrieves a conversation and all its messages.
func (s *Store) GetConversation(ctx context.Context, db DBTX, id string) (*Conversation, []Message, error) {
	var conv Conversation
	err := db.QueryRowContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations WHERE id = ?`, id,
	).Scan(&conv.ID, &conv.Title, &conv.CreatedAt, &conv.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("chat.Store.GetConversation: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, citations, created_at
		 FROM messages
		 WHERE conversation_id = ?
		 ORDER BY created_at ASC`,
		id,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("chat.Store.GetConversation: messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var citationsJSON sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &citationsJSON, &m.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("chat.Store.GetConversation: scan message: %w", err)
		}
		if citationsJSON.Valid && citationsJSON.String != "" {
			if err := json.Unmarshal([]byte(citationsJSON.String), &m.Citations); err != nil {
				// Log the corrupt citation data and continue.
				slog.Warn("chat.Store.GetConversation: failed to unmarshal citations",
					"message_id", m.ID, "error", err)
				m.Citations = nil
			}
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("chat.Store.GetConversation: rows: %w", err)
	}

	return &conv, msgs, nil
}

// DeleteConversation removes a conversation and its messages (via CASCADE).
func (s *Store) DeleteConversation(ctx context.Context, db DBTX, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("chat.Store.DeleteConversation: %w", err)
	}
	return nil
}

// AddMessage inserts a message into a conversation.
func (s *Store) AddMessage(ctx context.Context, db DBTX, msg Message) error {
	var citationsJSON *string
	if len(msg.Citations) > 0 {
		b, err := json.Marshal(msg.Citations)
		if err != nil {
			return fmt.Errorf("chat.Store.AddMessage: marshal citations: %w", err)
		}
		s := string(b)
		citationsJSON = &s
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO messages (id, conversation_id, role, content, citations, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ConversationID, msg.Role, msg.Content, citationsJSON, msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("chat.Store.AddMessage: %w", err)
	}

	// Update conversation's updated_at timestamp.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`UPDATE conversations SET updated_at = ? WHERE id = ?`,
		now, msg.ConversationID,
	)
	if err != nil {
		return fmt.Errorf("chat.Store.AddMessage: update conversation: %w", err)
	}

	return nil
}

// UpdateConversationTitle sets the title of a conversation.
func (s *Store) UpdateConversationTitle(ctx context.Context, db DBTX, id, title string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE conversations SET title = ? WHERE id = ?`,
		title, id,
	)
	if err != nil {
		return fmt.Errorf("chat.Store.UpdateConversationTitle: %w", err)
	}
	return nil
}

// GetFirstUserMessage returns the content of the first user message in a conversation.
func (s *Store) GetFirstUserMessage(ctx context.Context, db DBTX, conversationID string) (string, error) {
	var content string
	err := db.QueryRowContext(ctx,
		`SELECT content FROM messages
		 WHERE conversation_id = ? AND role = 'user'
		 ORDER BY created_at ASC
		 LIMIT 1`,
		conversationID,
	).Scan(&content)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("chat.Store.GetFirstUserMessage: %w", err)
	}
	return content, nil
}
