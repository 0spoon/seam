// Package chat manages chat conversation history in per-user SQLite databases.
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// DBTX is an interface satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// TxBeginner is satisfied by *sql.DB and allows starting transactions.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
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

// ToolCall represents a tool invocation an assistant turn requested.
// NOTE: This type is structurally identical to ai.ToolCall and is kept
// separate to avoid coupling the chat store to the ai package (matching
// the Citation precedent above). Any field changes must be mirrored in
// both packages.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message represents a single message in a conversation.
//
// The agentic assistant uses the extended tool fields:
//   - role='assistant' with non-empty ToolCalls is a request to invoke tools.
//   - role='tool' with ToolCallID + ToolName is the result of one such call.
//   - Iteration is the position in the agentic loop that produced the message.
//
// RAG chat (chat.ask) leaves the new fields zero-valued.
type Message struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	Role           string     `json:"role"`
	Content        string     `json:"content"`
	Citations      []Citation `json:"citations,omitempty"`
	ToolCalls      []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID     string     `json:"tool_call_id,omitempty"`
	ToolName       string     `json:"tool_name,omitempty"`
	Iteration      int        `json:"iteration,omitempty"`
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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("chat.Store.GetConversation: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, citations,
		        tool_calls, tool_call_id, tool_name, iteration, created_at
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
		m, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, nil, fmt.Errorf("chat.Store.GetConversation: scan message: %w", scanErr)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("chat.Store.GetConversation: rows: %w", err)
	}

	return &conv, msgs, nil
}

// scanMessage decodes a single message row that includes the extended
// agentic columns. It is shared by GetConversation and SearchMessages so
// the column list and JSON-decoding logic stay in one place.
func scanMessage(rows *sql.Rows) (Message, error) {
	var m Message
	var citationsJSON, toolCallsJSON, toolCallID, toolName sql.NullString
	if err := rows.Scan(
		&m.ID, &m.ConversationID, &m.Role, &m.Content,
		&citationsJSON, &toolCallsJSON, &toolCallID, &toolName,
		&m.Iteration, &m.CreatedAt,
	); err != nil {
		return Message{}, err
	}
	if citationsJSON.Valid && citationsJSON.String != "" {
		if err := json.Unmarshal([]byte(citationsJSON.String), &m.Citations); err != nil {
			slog.Warn("chat.scanMessage: failed to unmarshal citations",
				"message_id", m.ID, "error", err)
			m.Citations = nil
		}
	}
	if toolCallsJSON.Valid && toolCallsJSON.String != "" {
		if err := json.Unmarshal([]byte(toolCallsJSON.String), &m.ToolCalls); err != nil {
			slog.Warn("chat.scanMessage: failed to unmarshal tool_calls",
				"message_id", m.ID, "error", err)
			m.ToolCalls = nil
		}
	}
	if toolCallID.Valid {
		m.ToolCallID = toolCallID.String
	}
	if toolName.Valid {
		m.ToolName = toolName.String
	}
	return m, nil
}

// DeleteConversation removes a conversation and its messages (via CASCADE).
// Returns ErrNotFound if no conversation with the given ID exists.
func (s *Store) DeleteConversation(ctx context.Context, db DBTX, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("chat.Store.DeleteConversation: %w", err)
	}
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("chat.Store.DeleteConversation: rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("chat.Store.DeleteConversation: %w", ErrNotFound)
	}
	return nil
}

// AddMessage inserts a message into a conversation and updates the
// conversation timestamp atomically within a transaction.
func (s *Store) AddMessage(ctx context.Context, db DBTX, msg Message) error {
	var citationsJSON *string
	if len(msg.Citations) > 0 {
		b, err := json.Marshal(msg.Citations)
		if err != nil {
			return fmt.Errorf("chat.Store.AddMessage: marshal citations: %w", err)
		}
		str := string(b)
		citationsJSON = &str
	}

	var toolCallsJSON *string
	if len(msg.ToolCalls) > 0 {
		b, err := json.Marshal(msg.ToolCalls)
		if err != nil {
			return fmt.Errorf("chat.Store.AddMessage: marshal tool_calls: %w", err)
		}
		str := string(b)
		toolCallsJSON = &str
	}

	// If the caller passed a *sql.DB, wrap in a transaction for atomicity.
	// If the caller already passed a *sql.Tx, use it directly.
	var txDB DBTX = db
	if sqlDB, ok := db.(TxBeginner); ok {
		tx, txErr := sqlDB.BeginTx(ctx, nil)
		if txErr != nil {
			return fmt.Errorf("chat.Store.AddMessage: begin tx: %w", txErr)
		}
		defer tx.Rollback() //nolint:errcheck
		txDB = tx

		// Run both operations, then commit.
		if err := s.addMessageInTx(ctx, txDB, msg, citationsJSON, toolCallsJSON); err != nil {
			return err
		}
		return tx.Commit()
	}

	// Fallback: caller passed a tx or non-DB, run directly.
	return s.addMessageInTx(ctx, txDB, msg, citationsJSON, toolCallsJSON)
}

func (s *Store) addMessageInTx(ctx context.Context, db DBTX, msg Message, citationsJSON, toolCallsJSON *string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO messages (
		    id, conversation_id, role, content, citations,
		    tool_calls, tool_call_id, tool_name, iteration, created_at
		 )
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ConversationID, msg.Role, msg.Content, citationsJSON,
		toolCallsJSON, nullString(msg.ToolCallID), nullString(msg.ToolName),
		msg.Iteration, msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("chat.Store.AddMessage: %w", err)
	}

	// Update conversation's updated_at timestamp. Check RowsAffected to
	// detect inserts referencing a non-existent conversation.
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx,
		`UPDATE conversations SET updated_at = ? WHERE id = ?`,
		now, msg.ConversationID,
	)
	if err != nil {
		return fmt.Errorf("chat.Store.AddMessage: update conversation: %w", err)
	}
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("chat.Store.AddMessage: rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("chat.Store.AddMessage: conversation %s: %w", msg.ConversationID, ErrNotFound)
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

// SearchMessages searches across all conversation messages using LIKE.
// Returns matching messages with their conversation IDs.
func (s *Store) SearchMessages(ctx context.Context, db DBTX, query string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Use LIKE for basic substring matching. FTS on messages could be added later.
	// Escape SQL LIKE wildcards plus the escape character itself. Order
	// matters: backslash must be doubled first so the wildcard escapes
	// inserted next don't get re-escaped.
	escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query)
	rows, err := db.QueryContext(ctx,
		`SELECT m.id, m.conversation_id, m.role, m.content, m.citations,
		        m.tool_calls, m.tool_call_id, m.tool_name, m.iteration, m.created_at
		 FROM messages m
		 WHERE m.content LIKE ? ESCAPE '\'
		 ORDER BY m.created_at DESC
		 LIMIT ?`,
		"%"+escaped+"%", limit,
	)
	if err != nil {
		return nil, fmt.Errorf("chat.Store.SearchMessages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		msg, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("chat.Store.SearchMessages: scan: %w", scanErr)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("chat.Store.SearchMessages: rows: %w", err)
	}
	return messages, nil
}

// nullString returns nil for an empty string so SQLite stores NULL instead
// of "" for the optional tool_call_id / tool_name columns. This keeps the
// "no value" sentinel uniform with the JSON-encoded columns above.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
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
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("chat.Store.GetFirstUserMessage: %w", err)
	}
	return content, nil
}
