package assistant

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/ws"
	"github.com/oklog/ulid/v2"
)

// ServiceDeps holds the dependencies for the assistant service.
type ServiceDeps struct {
	Store        *Store
	MemoryStore  *MemoryStore
	ProfileStore *ProfileStore
	Registry     *ToolRegistry
	LLM          ai.ToolChatCompleter
	// Summarizer is used to fold older conversation turns into a
	// running summary memory. It can point at the same provider as
	// LLM (all built-in providers implement both interfaces). When
	// nil, conversation summarization is disabled.
	Summarizer    ai.ChatCompleter
	ChatModel     string
	UserDBManager userdb.Manager
	Hub           *ws.Hub
	Logger        *slog.Logger
	Config        ServiceConfig
}

// ServiceConfig holds configurable assistant parameters.
type ServiceConfig struct {
	MaxIterations        int      `json:"max_iterations"`
	ConfirmationRequired []string `json:"confirmation_required"` // tool names requiring confirmation
}

// Service implements the agentic assistant loop.
type Service struct {
	store         *Store
	memoryStore   *MemoryStore
	profileStore  *ProfileStore
	registry      *ToolRegistry
	llm           ai.ToolChatCompleter
	summarizer    ai.ChatCompleter
	chatModel     string
	userDBManager userdb.Manager
	hub           *ws.Hub
	logger        *slog.Logger
	config        ServiceConfig
	confirmation  *ConfirmationManager
}

// NewService creates a new assistant Service.
func NewService(deps ServiceDeps) *Service {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Config.MaxIterations <= 0 {
		deps.Config.MaxIterations = 10
	}
	return &Service{
		store:         deps.Store,
		memoryStore:   deps.MemoryStore,
		profileStore:  deps.ProfileStore,
		registry:      deps.Registry,
		llm:           deps.LLM,
		summarizer:    deps.Summarizer,
		chatModel:     deps.ChatModel,
		userDBManager: deps.UserDBManager,
		hub:           deps.Hub,
		logger:        deps.Logger,
		config:        deps.Config,
		confirmation:  NewConfirmationManager(deps.Config.ConfirmationRequired),
	}
}

// maxAssistantRecentMessages is the upper bound on raw history
// messages included verbatim in an assistant LLM prompt. Older turns
// are folded into a per-conversation summary memory.
const maxAssistantRecentMessages = 20

// summaryRefreshThreshold is the number of additional history
// messages beyond the recent window that must accumulate before a
// background summary refresh is triggered. The buffer prevents a
// refresh on every single call once a conversation is just over the
// recent window.
const summaryRefreshThreshold = 10

// ChatRequest is the input for an assistant chat interaction.
type ChatRequest struct {
	UserID         string           `json:"-"`
	ConversationID string           `json:"conversation_id"`
	Message        string           `json:"message"`
	History        []ai.ToolMessage `json:"history,omitempty"`
}

// ChatResponse is the output of an assistant chat interaction.
type ChatResponse struct {
	Response     string              `json:"response"`
	ToolsUsed    []ToolResult        `json:"tools_used,omitempty"`
	Iterations   int                 `json:"iterations"`
	Confirmation *ConfirmationPrompt `json:"confirmation,omitempty"`
}

// ConfirmationPrompt is returned when the assistant needs user approval
// before executing a write operation.
type ConfirmationPrompt struct {
	ActionID  string          `json:"action_id"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
	Message   string          `json:"message"`
}

// Chat executes the agentic tool-use loop for a user message.
func (s *Service) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	db, err := s.userDBManager.Open(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("assistant.Service.Chat: open db: %w", err)
	}

	// Load profile and recent memories for system prompt context.
	profile, memories := s.loadContext(ctx, db, req.Message)

	// For long conversations, fold older history into a running
	// summary memory and only include the recent window verbatim.
	conversationSummary, recentHistory := s.applyConversationSummary(ctx, db, req.ConversationID, req.History)

	systemPrompt := buildSystemPrompt(profile, memories, conversationSummary)

	messages := []ai.ToolMessage{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, recentHistory...)
	messages = append(messages, ai.ToolMessage{
		Role:    "user",
		Content: req.Message,
	})

	// After we return, schedule a background refresh of the summary
	// if the conversation has grown well past the recent window. The
	// refresh runs against a fresh background context so it survives
	// the request being canceled by the client.
	defer s.maybeRefreshConversationSummary(req.UserID, req.ConversationID, req.History)

	toolDefs := s.registry.Definitions()
	var toolsUsed []ToolResult

	for iteration := 0; iteration < s.config.MaxIterations; iteration++ {
		resp, llmErr := s.llm.ChatCompletionWithTools(ctx, s.chatModel, messages, toolDefs)
		if llmErr != nil {
			return nil, fmt.Errorf("assistant.Service.Chat: llm call (iteration %d): %w", iteration, llmErr)
		}

		// If no tool calls, the LLM produced a final response.
		if len(resp.ToolCalls) == 0 {
			return &ChatResponse{
				Response:   resp.Content,
				ToolsUsed:  toolsUsed,
				Iterations: iteration + 1,
			}, nil
		}

		// Add the assistant message with tool calls to history.
		assistantMsg := ai.ToolMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// Execute each tool call.
		for _, tc := range resp.ToolCalls {
			tool, toolErr := s.registry.Get(tc.Name)
			if toolErr != nil {
				messages = append(messages, ai.ToolMessage{
					Role:       "tool",
					Content:    fmt.Sprintf("Error: tool %q not found", tc.Name),
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
				toolsUsed = append(toolsUsed, ToolResult{
					ToolName:  tc.Name,
					Arguments: json.RawMessage(tc.Arguments),
					Error:     toolErr.Error(),
				})
				continue
			}

			// Check if confirmation is needed for write operations.
			if !tool.ReadOnly && s.confirmation.RequiresConfirmation(tc.Name) {
				// Record a pending action and return a confirmation prompt to the client.
				actionID, genErr := generateULID()
				if genErr != nil {
					s.logger.Warn("assistant.Service.Chat: failed to generate action ID", "error", genErr)
					actionID = fmt.Sprintf("pending_%d", time.Now().UnixNano())
				}

				now := time.Now().UTC()
				action := &Action{
					ID:             actionID,
					ConversationID: req.ConversationID,
					ToolName:       tc.Name,
					Arguments:      tc.Arguments,
					Status:         ActionStatusPending,
					CreatedAt:      now,
				}
				if recordErr := s.store.RecordAction(ctx, db, action); recordErr != nil {
					s.logger.Warn("assistant.Service.Chat: failed to record pending action",
						"error", recordErr)
				}

				return &ChatResponse{
					Response:   resp.Content,
					ToolsUsed:  toolsUsed,
					Iterations: iteration + 1,
					Confirmation: &ConfirmationPrompt{
						ActionID:  actionID,
						ToolName:  tc.Name,
						Arguments: json.RawMessage(tc.Arguments),
						Message:   fmt.Sprintf("The assistant wants to use %q. Approve this action?", tc.Name),
					},
				}, nil
			}

			// Execute the tool.
			tr := s.executeTool(ctx, req.UserID, db, req.ConversationID, tool, tc)
			toolsUsed = append(toolsUsed, tr)

			if tr.Error != "" {
				messages = append(messages, ai.ToolMessage{
					Role:       "tool",
					Content:    fmt.Sprintf("Error: %s", tr.Error),
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			} else {
				resultStr := truncateResult(string(tr.Result))
				messages = append(messages, ai.ToolMessage{
					Role:       "tool",
					Content:    resultStr,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			}
		}
	}

	return nil, ErrMaxIterations
}

// ApproveAction approves and executes a previously pending action.
func (s *Service) ApproveAction(ctx context.Context, userID, actionID string) (*ToolResult, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("assistant.Service.ApproveAction: open db: %w", err)
	}

	// Load the pending action.
	action, err := s.store.GetAction(ctx, db, actionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("assistant.Service.ApproveAction: load action: %w", err)
	}
	if action.Status != ActionStatusPending {
		return nil, fmt.Errorf("assistant.Service.ApproveAction: action is %s, not pending", action.Status)
	}

	// Look up the tool.
	tool, err := s.registry.Get(action.ToolName)
	if err != nil {
		return nil, fmt.Errorf("assistant.Service.ApproveAction: %w", err)
	}

	// Execute the tool.
	start := time.Now()
	result, execErr := tool.Func(ctx, userID, json.RawMessage(action.Arguments))
	duration := time.Since(start)

	tr := ToolResult{
		ToolName:   action.ToolName,
		Arguments:  json.RawMessage(action.Arguments),
		DurationMs: duration.Milliseconds(),
	}

	if execErr != nil {
		tr.Error = execErr.Error()
		if updateErr := s.store.UpdateActionStatus(ctx, db, actionID, ActionStatusFailed, execErr.Error()); updateErr != nil {
			s.logger.Warn("assistant.Service.ApproveAction: failed to update action status",
				"error", updateErr, "action_id", actionID)
		}
	} else {
		tr.Result = result
		resultStr := truncateResult(string(result))
		if updateErr := s.store.UpdateActionStatus(ctx, db, actionID, ActionStatusExecuted, resultStr); updateErr != nil {
			s.logger.Warn("assistant.Service.ApproveAction: failed to update action status",
				"error", updateErr, "action_id", actionID)
		}
	}

	return &tr, nil
}

// RejectAction rejects a previously pending action.
func (s *Service) RejectAction(ctx context.Context, userID, actionID string) error {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("assistant.Service.RejectAction: open db: %w", err)
	}
	return s.store.UpdateActionStatus(ctx, db, actionID, ActionStatusRejected, "")
}

// executeTool runs a tool and records the action.
func (s *Service) executeTool(ctx context.Context, userID string, db *sql.DB, conversationID string, tool *Tool, tc ai.ToolCall) ToolResult {
	start := time.Now()
	result, execErr := tool.Func(ctx, userID, json.RawMessage(tc.Arguments))
	duration := time.Since(start)

	tr := ToolResult{
		ToolName:   tc.Name,
		Arguments:  json.RawMessage(tc.Arguments),
		DurationMs: duration.Milliseconds(),
	}

	if execErr != nil {
		tr.Error = execErr.Error()
		s.recordAction(ctx, db, conversationID, tc.Name, tc.Arguments, execErr.Error(), ActionStatusFailed)
	} else {
		tr.Result = result
		resultStr := truncateResult(string(result))
		s.recordAction(ctx, db, conversationID, tc.Name, tc.Arguments, resultStr, ActionStatusExecuted)
	}

	return tr
}

// ChatStream executes the agentic loop and streams events as they happen.
// Tool use events are emitted as each tool executes; text events are emitted
// when the LLM produces its final response.
func (s *Service) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	eventCh := make(chan StreamEvent, 64)

	go func() {
		defer close(eventCh)

		db, err := s.userDBManager.Open(ctx, req.UserID)
		if err != nil {
			eventCh <- StreamEvent{Type: StreamEventError, Error: fmt.Sprintf("open db: %s", err)}
			return
		}

		profile, memories := s.loadContext(ctx, db, req.Message)
		conversationSummary, recentHistory := s.applyConversationSummary(ctx, db, req.ConversationID, req.History)
		systemPrompt := buildSystemPrompt(profile, memories, conversationSummary)
		messages := []ai.ToolMessage{
			{Role: "system", Content: systemPrompt},
		}
		messages = append(messages, recentHistory...)
		messages = append(messages, ai.ToolMessage{
			Role:    "user",
			Content: req.Message,
		})

		// Schedule background summary refresh after the stream
		// completes so the next call benefits from a fresh digest.
		defer s.maybeRefreshConversationSummary(req.UserID, req.ConversationID, req.History)

		toolDefs := s.registry.Definitions()

		for iteration := 0; iteration < s.config.MaxIterations; iteration++ {
			select {
			case <-ctx.Done():
				eventCh <- StreamEvent{Type: StreamEventError, Error: "context cancelled"}
				return
			default:
			}

			resp, llmErr := s.llm.ChatCompletionWithTools(ctx, s.chatModel, messages, toolDefs)
			if llmErr != nil {
				eventCh <- StreamEvent{Type: StreamEventError, Error: fmt.Sprintf("llm call failed: %s", llmErr)}
				return
			}

			// No tool calls -- final response.
			if len(resp.ToolCalls) == 0 {
				if resp.Content != "" {
					eventCh <- StreamEvent{
						Type:    StreamEventText,
						Content: resp.Content,
					}
				}
				eventCh <- StreamEvent{
					Type:       StreamEventDone,
					Iterations: iteration + 1,
				}
				return
			}

			// Add assistant message with tool calls.
			messages = append(messages, ai.ToolMessage{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			// Execute each tool call and stream the events.
			for _, tc := range resp.ToolCalls {
				tool, toolErr := s.registry.Get(tc.Name)
				if toolErr != nil {
					messages = append(messages, ai.ToolMessage{
						Role:       "tool",
						Content:    fmt.Sprintf("Error: tool %q not found", tc.Name),
						ToolCallID: tc.ID,
						Name:       tc.Name,
					})
					eventCh <- StreamEvent{
						Type:     StreamEventToolUse,
						ToolName: tc.Name,
						Error:    toolErr.Error(),
					}
					continue
				}

				// Confirmation check -- for streaming, return a confirmation event.
				if !tool.ReadOnly && s.confirmation.RequiresConfirmation(tc.Name) {
					actionID, genErr := generateULID()
					if genErr != nil {
						s.logger.Warn("assistant.Service.ChatStream: failed to generate action ID", "error", genErr)
						actionID = fmt.Sprintf("pending_%d", time.Now().UnixNano())
					}
					now := time.Now().UTC()
					action := &Action{
						ID:             actionID,
						ConversationID: req.ConversationID,
						ToolName:       tc.Name,
						Arguments:      tc.Arguments,
						Status:         ActionStatusPending,
						CreatedAt:      now,
					}
					if recordErr := s.store.RecordAction(ctx, db, action); recordErr != nil {
						s.logger.Warn("assistant.Service.ChatStream: failed to record pending action",
							"error", recordErr)
					}
					eventCh <- StreamEvent{
						Type:     StreamEventConfirmation,
						ToolName: tc.Name,
						Content:  actionID,
					}
					// Stop the loop -- client must approve and re-invoke.
					eventCh <- StreamEvent{
						Type:       StreamEventDone,
						Iterations: iteration + 1,
					}
					return
				}

				// Execute the tool and emit the event immediately.
				tr := s.executeTool(ctx, req.UserID, db, req.ConversationID, tool, tc)

				if tr.Error != "" {
					messages = append(messages, ai.ToolMessage{
						Role:       "tool",
						Content:    fmt.Sprintf("Error: %s", tr.Error),
						ToolCallID: tc.ID,
						Name:       tc.Name,
					})
					eventCh <- StreamEvent{
						Type:     StreamEventToolUse,
						ToolName: tc.Name,
						Error:    tr.Error,
					}
				} else {
					resultStr := truncateResult(string(tr.Result))
					messages = append(messages, ai.ToolMessage{
						Role:       "tool",
						Content:    resultStr,
						ToolCallID: tc.ID,
						Name:       tc.Name,
					})
					eventCh <- StreamEvent{
						Type:     StreamEventToolUse,
						ToolName: tc.Name,
						Content:  resultStr,
					}
				}
			}
		}

		eventCh <- StreamEvent{Type: StreamEventError, Error: "max iterations reached"}
	}()

	return eventCh, nil
}

// StreamEvent represents a streaming event from the assistant.
type StreamEvent struct {
	Type       string `json:"type"`
	Content    string `json:"content,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Error      string `json:"error,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
}

// Stream event types.
const (
	StreamEventText         = "text"
	StreamEventToolUse      = "tool_use"
	StreamEventError        = "error"
	StreamEventDone         = "done"
	StreamEventConfirmation = "confirmation"
)

// GetProfile returns the user's profile.
func (s *Service) GetProfile(ctx context.Context, userID string) (*UserProfile, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("assistant.Service.GetProfile: open db: %w", err)
	}
	return s.profileStore.GetProfile(ctx, db)
}

// UpdateProfile updates the user's profile.
func (s *Service) UpdateProfile(ctx context.Context, userID string, profile *UserProfile) error {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("assistant.Service.UpdateProfile: open db: %w", err)
	}
	return s.profileStore.SaveProfile(ctx, db, profile)
}

// ListMemories returns memories with optional category filter.
func (s *Service) ListMemories(ctx context.Context, userID, category string, limit, offset int) ([]*Memory, int, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("assistant.Service.ListMemories: open db: %w", err)
	}
	return s.memoryStore.ListMemories(ctx, db, category, limit, offset)
}

// CreateMemory creates a new memory entry.
func (s *Service) CreateMemory(ctx context.Context, userID string, m *Memory) error {
	if m.ID == "" {
		id, genErr := generateULID()
		if genErr != nil {
			return fmt.Errorf("assistant.Service.CreateMemory: generate id: %w", genErr)
		}
		m.ID = id
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.Confidence == 0 {
		m.Confidence = 1.0
	}
	if !ValidMemoryCategories[m.Category] {
		m.Category = MemoryCategoryFact
	}
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("assistant.Service.CreateMemory: open db: %w", err)
	}
	return s.memoryStore.SaveMemory(ctx, db, m)
}

// DeleteMemory deletes a memory by ID.
func (s *Service) DeleteMemory(ctx context.Context, userID, memoryID string) error {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("assistant.Service.DeleteMemory: open db: %w", err)
	}
	return s.memoryStore.DeleteMemory(ctx, db, memoryID)
}

// SearchMemories searches memories by content.
func (s *Service) SearchMemories(ctx context.Context, userID, query string, limit int) ([]*Memory, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("assistant.Service.SearchMemories: open db: %w", err)
	}
	return s.memoryStore.SearchMemories(ctx, db, query, limit)
}

// ListActions returns the tool actions for a conversation.
func (s *Service) ListActions(ctx context.Context, userID, conversationID string) ([]*Action, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("assistant.Service.ListActions: open db: %w", err)
	}
	return s.store.ListActions(ctx, db, conversationID, 100)
}

func (s *Service) recordAction(ctx context.Context, db *sql.DB, conversationID, toolName, arguments, result, status string) {
	// Best-effort audit logging -- failures are logged but do not abort the loop.
	actionID, err := generateULID()
	if err != nil {
		s.logger.Warn("assistant.Service.recordAction: failed to generate ID", "error", err)
		return
	}
	now := time.Now().UTC()
	action := &Action{
		ID:             actionID,
		ConversationID: conversationID,
		ToolName:       toolName,
		Arguments:      arguments,
		Result:         result,
		Status:         status,
		CreatedAt:      now,
	}
	// Only mark execution time on successful runs. Failed actions are
	// audited with status=failed and a null executed_at so consumers
	// can distinguish "we tried" from "the side effect actually ran".
	if status == ActionStatusExecuted {
		action.ExecutedAt = now
	}
	if recordErr := s.store.RecordAction(ctx, db, action); recordErr != nil {
		s.logger.Warn("assistant.Service.recordAction: failed to record action",
			"error", recordErr, "tool", toolName)
	}
}

// applyConversationSummary returns the running conversation summary
// (if any) and the recent-window slice of history that should be sent
// to the LLM verbatim. When the history fits within the recent
// window, the full history is returned and the summary is empty.
//
// Errors loading the summary degrade gracefully: a missing summary
// returns an empty string, and the call still slices history.
func (s *Service) applyConversationSummary(ctx context.Context, db *sql.DB, conversationID string, history []ai.ToolMessage) (string, []ai.ToolMessage) {
	if len(history) <= maxAssistantRecentMessages {
		return "", history
	}

	start := safeRecentBoundary(history, len(history)-maxAssistantRecentMessages)
	recent := history[start:]
	if conversationID == "" || s.memoryStore == nil {
		return "", recent
	}

	summary, err := s.memoryStore.GetConversationSummary(ctx, db, conversationID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Debug("assistant.Service.applyConversationSummary: load summary",
				"error", err, "conversation_id", conversationID)
		}
		return "", recent
	}
	return summary.Content, recent
}

// maybeRefreshConversationSummary spawns a background goroutine that
// folds older history into the conversation summary memory when the
// conversation has grown enough to warrant it. The goroutine uses a
// fresh background context so it survives the request being canceled.
//
// No-op when the summarizer is not configured, the conversation has
// no ID, or the history is short enough that the recent window
// already covers everything.
func (s *Service) maybeRefreshConversationSummary(userID, conversationID string, history []ai.ToolMessage) {
	if s.summarizer == nil || s.memoryStore == nil {
		return
	}
	if conversationID == "" || userID == "" {
		return
	}
	if len(history) < maxAssistantRecentMessages+summaryRefreshThreshold {
		return
	}

	// Snapshot the older portion so the goroutine doesn't observe
	// later mutations to the slice.
	cut := len(history) - maxAssistantRecentMessages
	older := make([]ai.ToolMessage, cut)
	copy(older, history[:cut])

	go s.refreshConversationSummary(userID, conversationID, older)
}

// refreshConversationSummary calls the summarizer LLM with the older
// portion of a conversation and stores the result as the canonical
// summary memory for the conversation. Runs in a goroutine spawned by
// maybeRefreshConversationSummary; errors are logged and swallowed.
func (s *Service) refreshConversationSummary(userID, conversationID string, older []ai.ToolMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		s.logger.Warn("assistant.Service.refreshConversationSummary: open db",
			"error", err, "conversation_id", conversationID)
		return
	}

	var existing string
	if prev, err := s.memoryStore.GetConversationSummary(ctx, db, conversationID); err == nil {
		existing = prev.Content
	} else if !errors.Is(err, ErrNotFound) {
		s.logger.Debug("assistant.Service.refreshConversationSummary: load existing",
			"error", err, "conversation_id", conversationID)
	}

	summary, err := s.summarizeMessages(ctx, older, existing)
	if err != nil {
		s.logger.Warn("assistant.Service.refreshConversationSummary: summarize",
			"error", err, "conversation_id", conversationID)
		return
	}
	if strings.TrimSpace(summary) == "" {
		return
	}
	if err := s.memoryStore.SaveConversationSummary(ctx, db, conversationID, summary); err != nil {
		s.logger.Warn("assistant.Service.refreshConversationSummary: save",
			"error", err, "conversation_id", conversationID)
	}
}

// summarizeMessages calls the summarizer LLM to fold a batch of older
// turns into a running summary. When existing is non-empty, the model
// is asked to extend it incrementally. Tool messages and other
// non-user/assistant roles are skipped so the transcript only contains
// content the model can meaningfully summarize.
func (s *Service) summarizeMessages(ctx context.Context, older []ai.ToolMessage, existing string) (string, error) {
	if s.summarizer == nil {
		return "", fmt.Errorf("assistant.Service.summarizeMessages: no summarizer configured")
	}

	var transcriptB strings.Builder
	for _, m := range older {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		transcriptB.WriteString(strings.ToUpper(m.Role))
		transcriptB.WriteString(": ")
		transcriptB.WriteString(m.Content)
		transcriptB.WriteString("\n\n")
	}
	transcript := strings.TrimSpace(transcriptB.String())
	if transcript == "" {
		return strings.TrimSpace(existing), nil
	}

	systemPrompt := `You compress conversations between a user and an AI assistant.
Produce a concise running summary that preserves: the user's goals,
decisions made, facts established, open questions, and any commitments.
Use neutral past-tense prose. Avoid copying long verbatim quotes. Aim
for 5-12 sentences total. Do not invent details that are not in the
source material.`

	var userContent strings.Builder
	if strings.TrimSpace(existing) != "" {
		userContent.WriteString("Existing summary of earlier turns:\n")
		userContent.WriteString(strings.TrimSpace(existing))
		userContent.WriteString("\n\nNew transcript to fold in:\n")
		userContent.WriteString(transcript)
		userContent.WriteString("\n\nProduce an updated combined summary.")
	} else {
		userContent.WriteString("Transcript to summarize:\n")
		userContent.WriteString(transcript)
		userContent.WriteString("\n\nProduce a summary.")
	}

	resp, err := s.summarizer.ChatCompletion(ctx, s.chatModel, []ai.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent.String()},
	})
	if err != nil {
		return "", fmt.Errorf("assistant.Service.summarizeMessages: chat completion: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// loadContext retrieves the user profile and relevant memories for the system prompt.
// Errors are logged but never propagated -- degraded context is better than failure.
func (s *Service) loadContext(ctx context.Context, db *sql.DB, userMessage string) (*UserProfile, []*Memory) {
	var profile *UserProfile
	var memories []*Memory

	if s.profileStore != nil {
		p, err := s.profileStore.GetProfile(ctx, db)
		if err != nil {
			s.logger.Warn("assistant.Service.loadContext: failed to load profile", "error", err)
		} else {
			profile = p
		}
	}

	if s.memoryStore != nil {
		// Try to find memories relevant to the user's message via FTS.
		// Only the relevance path bumps last_accessed -- the recency
		// fallback below must NOT touch, or every chat turn would
		// inflate decay scores for whatever is newest and starve older
		// memories regardless of actual recall relevance.
		fromSearch := false
		if userMessage != "" {
			hits, err := s.memoryStore.SearchMemories(ctx, db, userMessage, 5)
			if err != nil {
				s.logger.Debug("assistant.Service.loadContext: memory search failed", "error", err)
			} else if len(hits) > 0 {
				memories = hits
				fromSearch = true
			}
		}
		// If no relevant memories found, fall back to recent memories.
		if len(memories) == 0 {
			recent, err := s.memoryStore.GetRecentMemories(ctx, db, 5)
			if err != nil {
				s.logger.Debug("assistant.Service.loadContext: recent memories failed", "error", err)
			} else {
				memories = recent
			}
		}
		// Bump last_accessed only on memories that came from the
		// relevance path so frequently-recalled items stay ranked
		// highly by the decay scoring in future searches.
		if fromSearch && len(memories) > 0 {
			ids := make([]string, len(memories))
			for i, m := range memories {
				ids[i] = m.ID
			}
			if err := s.memoryStore.TouchMemories(ctx, db, ids); err != nil {
				s.logger.Debug("assistant.Service.loadContext: touch memories failed", "error", err)
			}
		}
	}

	return profile, memories
}

func buildSystemPrompt(profile *UserProfile, memories []*Memory, conversationSummary string) string {
	now := time.Now()
	var sb strings.Builder

	fmt.Fprintf(&sb, `You are Seam, an intelligent personal AI assistant that manages the user's knowledge base.
You have access to tools that let you search, read, create, and modify notes, tasks, and projects.
You can also save and recall memories about the user to maintain continuity across conversations.

Current date and time: %s (%s)`,
		now.Format("2006-01-02 15:04:05 MST"),
		now.Weekday().String(),
	)

	// Include user profile if available.
	if profile != nil && !profile.IsEmpty() {
		sb.WriteString("\n\n## User Profile\n")
		sb.WriteString(profile.FormatForPrompt())
	}

	// Include relevant memories.
	if len(memories) > 0 {
		sb.WriteString("\n\n## Relevant Memories\n")
		for _, m := range memories {
			fmt.Fprintf(&sb, "- [%s] %s\n", m.Category, m.Content)
		}
	}

	// Include the running conversation summary, if any. This carries
	// long-form context across turns that no longer fit in the
	// verbatim history window.
	if strings.TrimSpace(conversationSummary) != "" {
		sb.WriteString("\n\n## Earlier Conversation Summary\n")
		sb.WriteString(strings.TrimSpace(conversationSummary))
	}

	sb.WriteString(`

## Guidelines
- Use tools to find information before answering questions about the user's notes.
- When creating or modifying content, be precise and preserve existing formatting.
- For complex requests, break them down into steps: search first, read relevant notes, then act.
- Always cite specific notes when referencing information from the knowledge base.
- If you cannot find relevant information, say so honestly.
- Be concise and helpful. Prefer structured output (lists, headers) for complex responses.
- When you learn important facts, preferences, or decisions from the user, use the save_memory tool to remember them.
- Reference relevant memories when they help answer the user's question.`)

	return sb.String()
}

func generateULID() (string, error) {
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// safeRecentBoundary adjusts a positional slice boundary so it never
// lands inside a tool-use group. A tool-use group is an assistant
// message with ToolCalls followed by one or more "tool" role messages
// that reference those calls. If the proposed boundary points at a
// "tool" message, or at an assistant message whose tool result(s) live
// behind the boundary, walk backwards until both halves of every
// group are either fully inside or fully outside the recent window.
//
// This prevents emitting an orphan tool_call_id with no matching
// tool_use block, which OpenAI/Anthropic both reject with a 400.
func safeRecentBoundary(history []ai.ToolMessage, start int) int {
	if start <= 0 || start >= len(history) {
		return start
	}
	for start > 0 {
		// If the boundary points at a "tool" message, the corresponding
		// assistant tool_use lives behind the boundary -- back up.
		if history[start].Role == "tool" {
			start--
			continue
		}
		// If the boundary points just past an assistant message that
		// issued tool calls, the assistant tool_use is behind the
		// boundary but the "tool" results are inside. Back up so the
		// assistant message is also inside the window.
		prev := history[start-1]
		if prev.Role == "assistant" && len(prev.ToolCalls) > 0 {
			start--
			continue
		}
		break
	}
	return start
}

func truncateResult(s string) string {
	runes := []rune(s)
	if len(runes) > 4000 {
		return string(runes[:4000]) + "... (truncated)"
	}
	return s
}
