package chat

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/userdb"
)

// maxTitleLen is the maximum length of an auto-generated conversation title.
const maxTitleLen = 80

// ErrNotFound is returned when a conversation is not found.
var ErrNotFound = errors.New("conversation not found")

// ErrInvalidRole is returned when a message role is not 'user' or 'assistant'.
var ErrInvalidRole = errors.New("invalid message role")

// Service handles chat history business logic.
type Service struct {
	store         *Store
	userDBManager userdb.Manager
	logger        *slog.Logger
}

// NewService creates a new chat Service.
func NewService(store *Store, userDBManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:         store,
		userDBManager: userDBManager,
		logger:        logger,
	}
}

// CreateConversation creates a new conversation and returns it.
func (s *Service) CreateConversation(ctx context.Context, userID string) (*Conversation, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("chat.Service.CreateConversation: open db: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	conv := Conversation{
		ID:        ulid.MustNew(ulid.Now(), rand.Reader).String(),
		Title:     "",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.CreateConversation(ctx, db, conv); err != nil {
		return nil, fmt.Errorf("chat.Service.CreateConversation: %w", err)
	}

	s.logger.Debug("conversation created", "user_id", userID, "conversation_id", conv.ID)
	return &conv, nil
}

// ListConversations returns conversations for a user, most recent first.
func (s *Service) ListConversations(ctx context.Context, userID string, limit, offset int) ([]Conversation, int, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("chat.Service.ListConversations: open db: %w", err)
	}

	// Enforce limits.
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	convs, total, err := s.store.ListConversations(ctx, db, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("chat.Service.ListConversations: %w", err)
	}
	return convs, total, nil
}

// GetConversation retrieves a conversation and all its messages.
func (s *Service) GetConversation(ctx context.Context, userID, conversationID string) (*Conversation, []Message, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("chat.Service.GetConversation: open db: %w", err)
	}

	conv, msgs, err := s.store.GetConversation(ctx, db, conversationID)
	if err != nil {
		return nil, nil, fmt.Errorf("chat.Service.GetConversation: %w", err)
	}
	if conv == nil {
		return nil, nil, ErrNotFound
	}
	return conv, msgs, nil
}

// DeleteConversation removes a conversation and all its messages.
func (s *Service) DeleteConversation(ctx context.Context, userID, conversationID string) error {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("chat.Service.DeleteConversation: open db: %w", err)
	}

	if err := s.store.DeleteConversation(ctx, db, conversationID); err != nil {
		return fmt.Errorf("chat.Service.DeleteConversation: %w", err)
	}

	s.logger.Debug("conversation deleted", "user_id", userID, "conversation_id", conversationID)
	return nil
}

// AddMessage adds a message to a conversation. If the message is from the
// assistant and the conversation has no title, auto-titles from the first
// user message (truncated to maxTitleLen at a word boundary).
func (s *Service) AddMessage(ctx context.Context, userID string, msg Message) error {
	if msg.Role != "user" && msg.Role != "assistant" {
		return fmt.Errorf("chat.Service.AddMessage: role %q: %w", msg.Role, ErrInvalidRole)
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("chat.Service.AddMessage: open db: %w", err)
	}

	// Generate ID and timestamp if not set.
	if msg.ID == "" {
		msg.ID = ulid.MustNew(ulid.Now(), rand.Reader).String()
	}
	if msg.CreatedAt == "" {
		msg.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	if err := s.store.AddMessage(ctx, db, msg); err != nil {
		return fmt.Errorf("chat.Service.AddMessage: %w", err)
	}

	// Auto-title: when an assistant message is added and the conversation
	// has no title, set it from the first user message.
	if msg.Role == "assistant" {
		conv, _, err := s.store.GetConversation(ctx, db, msg.ConversationID)
		if err == nil && conv != nil && conv.Title == "" {
			firstMsg, fErr := s.store.GetFirstUserMessage(ctx, db, msg.ConversationID)
			if fErr == nil && firstMsg != "" {
				title := truncateToWord(firstMsg, maxTitleLen)
				if tErr := s.store.UpdateConversationTitle(ctx, db, msg.ConversationID, title); tErr != nil {
					s.logger.Warn("failed to auto-title conversation", "conversation_id", msg.ConversationID, "error", tErr)
				}
			}
		}
	}

	return nil
}

// truncateToWord truncates s to at most maxLen runes (not bytes),
// trimming to the last word boundary. If the string is short enough,
// returns it as-is. This is safe for multi-byte UTF-8 (CJK, emoji).
func truncateToWord(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	// Convert to runes to slice on character boundaries.
	runes := []rune(s)
	truncated := string(runes[:maxLen])
	// Find the last space to avoid cutting mid-word.
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLen/2 {
		truncated = truncated[:lastSpace]
	}
	return strings.TrimSpace(truncated)
}
