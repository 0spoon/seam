package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/katata/seam/internal/userdb"
)

// ErrInvalidRole is returned when a chat history message has an invalid role.
var ErrInvalidRole = errors.New("invalid message role in history: only 'user' and 'assistant' are allowed")

// maxRecentMessages is the maximum number of raw history messages
// (user + assistant combined) included verbatim in the chat prompt.
// Older turns are expected to be folded into a conversation summary
// passed alongside the history. This replaces the previous hard cap of
// 5 turns and is the upper bound when no summary is supplied.
const maxRecentMessages = 20

// Default chat retrieval parameters.
const (
	defaultRetrievalLimit  = 10   // number of ChromaDB chunks to retrieve
	defaultBodyTruncateLen = 2000 // max note body length for context
)

// ChatService handles RAG-powered chat using note context.
type ChatService struct {
	embedder        EmbeddingGenerator
	chat            ChatCompleter
	chroma          *ChromaClient
	dbManager       userdb.Manager
	embedModel      string
	chatModel       string
	logger          *slog.Logger
	retrievalLimit  int // number of ChromaDB chunks to retrieve
	bodyTruncateLen int // max note body chars included in context
}

// NewChatService creates a new ChatService. Optional configuration functions
// can be passed to set retrieval limit and body truncation length.
func NewChatService(embedder EmbeddingGenerator, chat ChatCompleter, chroma *ChromaClient, dbManager userdb.Manager, embedModel, chatModel string, logger *slog.Logger, opts ...func(*ChatService)) *ChatService {
	if logger == nil {
		logger = slog.Default()
	}
	cs := &ChatService{
		embedder:        embedder,
		chat:            chat,
		chroma:          chroma,
		dbManager:       dbManager,
		embedModel:      embedModel,
		chatModel:       chatModel,
		logger:          logger,
		retrievalLimit:  defaultRetrievalLimit,
		bodyTruncateLen: defaultBodyTruncateLen,
	}
	for _, opt := range opts {
		opt(cs)
	}
	return cs
}

// WithRetrievalLimit returns an option that sets the number of chunks to retrieve.
func WithRetrievalLimit(limit int) func(*ChatService) {
	return func(cs *ChatService) {
		if limit > 0 {
			cs.retrievalLimit = limit
		}
	}
}

// WithBodyTruncateLen returns an option that sets the max body length in context.
func WithBodyTruncateLen(length int) func(*ChatService) {
	return func(cs *ChatService) {
		if length > 0 {
			cs.bodyTruncateLen = length
		}
	}
}

// Citation represents a cited note with its ID and title.
type Citation struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ChatResult contains the response and cited notes.
type ChatResult struct {
	Response  string     `json:"response"`
	Citations []Citation `json:"citations"`
}

// noteSnippet holds retrieved note content for prompt construction.
type noteSnippet struct {
	Title string
	Body  string
}

// validateHistoryRoles checks that all history messages have valid roles
// (only "user" or "assistant"). Returns ErrInvalidRole if any message has
// an invalid role, preventing prompt injection via "system" messages.
func validateHistoryRoles(history []ChatMessage) error {
	for _, msg := range history {
		if msg.Role != "user" && msg.Role != "assistant" {
			return ErrInvalidRole
		}
	}
	return nil
}

// Ask handles a chat question by retrieving relevant notes and generating
// a response grounded in the user's knowledge base. The summary
// argument, if non-empty, is a previously-generated summary of older
// conversation turns that no longer fit in the recent-message window;
// it is included in the system prompt so the model retains long-form
// context across long conversations.
func (c *ChatService) Ask(ctx context.Context, userID, query string, history []ChatMessage, summary string) (*ChatResult, error) {
	if err := validateHistoryRoles(history); err != nil {
		return nil, fmt.Errorf("ai.ChatService.Ask: %w", err)
	}

	contexts, citations, err := c.retrieveContext(ctx, userID, query)
	if err != nil {
		return nil, err
	}

	messages := BuildChatMessages(query, contexts, history, summary)

	resp, err := c.chat.ChatCompletion(ctx, c.chatModel, messages)
	if err != nil {
		return nil, fmt.Errorf("ai.ChatService.Ask: chat completion: %w", err)
	}

	return &ChatResult{
		Response:  resp.Content,
		Citations: citations,
	}, nil
}

// AskStream is like Ask but returns a streaming response.
// Returns token channel, citations list, and error channel.
func (c *ChatService) AskStream(ctx context.Context, userID, query string, history []ChatMessage, summary string) (<-chan string, []Citation, <-chan error) {
	tokenCh := make(chan string, 64)
	errCh := make(chan error, 1)

	if err := validateHistoryRoles(history); err != nil {
		close(tokenCh)
		errCh <- fmt.Errorf("ai.ChatService.AskStream: %w", err)
		close(errCh)
		return tokenCh, nil, errCh
	}

	contexts, citations, err := c.retrieveContext(ctx, userID, query)
	if err != nil {
		close(tokenCh)
		errCh <- err
		close(errCh)
		return tokenCh, nil, errCh
	}

	messages := BuildChatMessages(query, contexts, history, summary)

	llmTokenCh, llmErrCh := c.chat.ChatCompletionStream(ctx, c.chatModel, messages)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		for token := range llmTokenCh {
			select {
			case tokenCh <- token:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
		for err := range llmErrCh {
			if err != nil {
				errCh <- err
			}
		}
	}()

	return tokenCh, citations, errCh
}

// retrieveContext embeds the query, retrieves relevant chunks from ChromaDB,
// and fetches the note content from the user's database.
func (c *ChatService) retrieveContext(ctx context.Context, userID, query string) ([]noteSnippet, []Citation, error) {
	queryEmbedding, err := c.embedder.GenerateEmbedding(ctx, c.embedModel, query)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: embed query: %w", err)
	}

	colName := CollectionName(userID)
	colID, err := c.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: get collection: %w", err)
	}

	chromaResults, err := c.chroma.Query(ctx, colID, queryEmbedding, c.retrievalLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: query chroma: %w", err)
	}

	db, err := c.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: open db: %w", err)
	}

	seen := make(map[string]bool)
	var contexts []noteSnippet
	var citations []Citation

	// Collect unique note IDs from ChromaDB results.
	var noteIDs []string
	for _, cr := range chromaResults {
		noteID := cr.Metadata["note_id"]
		if noteID == "" || seen[noteID] {
			continue
		}
		seen[noteID] = true
		noteIDs = append(noteIDs, noteID)
	}

	// Batch-load note data to avoid N+1 queries.
	if len(noteIDs) > 0 {
		placeholders := strings.Repeat("?,", len(noteIDs))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]interface{}, len(noteIDs))
		for i, id := range noteIDs {
			args[i] = id
		}
		rows, qErr := db.QueryContext(ctx,
			`SELECT id, title, body FROM notes WHERE id IN (`+placeholders+`)`, args...)
		if qErr == nil {
			defer rows.Close()
			type noteData struct {
				title string
				body  string
			}
			noteMap := make(map[string]noteData)
			for rows.Next() {
				var id, title, body string
				if err := rows.Scan(&id, &title, &body); err != nil {
					continue
				}
				noteMap[id] = noteData{title: title, body: body}
			}
			if rErr := rows.Err(); rErr != nil {
				slog.Warn("ai.ChatService.retrieveContext: rows iteration error", "error", rErr)
			}
			// Preserve ChromaDB relevance order.
			for _, id := range noteIDs {
				nd, ok := noteMap[id]
				if !ok {
					continue
				}
				body := nd.body
				runes := []rune(body)
				if len(runes) > c.bodyTruncateLen {
					body = string(runes[:c.bodyTruncateLen]) + "..."
				}
				contexts = append(contexts, noteSnippet{Title: nd.title, Body: body})
				citations = append(citations, Citation{ID: id, Title: nd.title})
			}
		}
	}

	return contexts, citations, nil
}

// BuildChatMessages constructs the messages for the RAG chat prompt.
// The summary argument, if non-empty, is a digest of older
// conversation turns that have been folded out of the verbatim history
// window; it is appended to the system prompt so the model retains
// long-form context. The recent-message window is capped at
// maxRecentMessages entries (combined user + assistant messages).
// Exported for testing.
func BuildChatMessages(query string, noteContexts []noteSnippet, history []ChatMessage, summary string) []ChatMessage {
	systemPrompt := `You are Seam, an AI assistant that answers questions using the user's personal notes.
You ONLY answer based on the provided note context. If the notes do not contain relevant information,
say so clearly. Do not make up information.

When referencing a note, mention its title. Be concise and helpful.`

	var contextParts []string
	for _, nc := range noteContexts {
		contextParts = append(contextParts, fmt.Sprintf("--- Note: %s ---\n%s", nc.Title, nc.Body))
	}

	contextStr := ""
	if len(contextParts) > 0 {
		contextStr = "\n\nRelevant notes from the user's knowledge base:\n\n" + strings.Join(contextParts, "\n\n")
	} else {
		contextStr = "\n\nNo relevant notes were found in the user's knowledge base."
	}

	systemContent := systemPrompt + contextStr
	if strings.TrimSpace(summary) != "" {
		systemContent += "\n\nSummary of earlier conversation turns (older context that no longer fits the verbatim window):\n" + summary
	}

	var messages []ChatMessage
	messages = append(messages, ChatMessage{
		Role:    "system",
		Content: systemContent,
	})

	// Add the most recent N raw history messages. Older turns are
	// expected to have been folded into the summary above.
	historyStart := 0
	if len(history) > maxRecentMessages {
		historyStart = len(history) - maxRecentMessages
	}
	for _, h := range history[historyStart:] {
		messages = append(messages, h)
	}

	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: query,
	})

	return messages
}

// SummarizeHistory produces a concise digest of older conversation
// turns suitable for inclusion in a future chat prompt as the summary
// argument to BuildChatMessages. When existingSummary is non-empty,
// the model is asked to extend it incrementally rather than start
// from scratch, so summaries can be refreshed cheaply over time.
//
// The history slice should contain the messages being folded out of
// the recent-message window, in chronological order. Returns an empty
// string with no error if history is empty.
func (c *ChatService) SummarizeHistory(ctx context.Context, history []ChatMessage, existingSummary string) (string, error) {
	if len(history) == 0 {
		return strings.TrimSpace(existingSummary), nil
	}
	if err := validateHistoryRoles(history); err != nil {
		return "", fmt.Errorf("ai.ChatService.SummarizeHistory: %w", err)
	}

	var transcriptB strings.Builder
	for _, m := range history {
		transcriptB.WriteString(strings.ToUpper(m.Role))
		transcriptB.WriteString(": ")
		transcriptB.WriteString(m.Content)
		transcriptB.WriteString("\n\n")
	}
	transcript := strings.TrimSpace(transcriptB.String())

	systemPrompt := `You compress conversations between a user and an AI assistant.
Produce a concise running summary that preserves: the user's goals,
decisions made, facts established, open questions, and any commitments.
Use neutral past-tense prose. Avoid copying long verbatim quotes. Aim
for 5-12 sentences total. Do not invent details that are not in the
source material.`

	var userContent strings.Builder
	if strings.TrimSpace(existingSummary) != "" {
		userContent.WriteString("Existing summary of earlier turns:\n")
		userContent.WriteString(strings.TrimSpace(existingSummary))
		userContent.WriteString("\n\nNew transcript to fold in:\n")
		userContent.WriteString(transcript)
		userContent.WriteString("\n\nProduce an updated combined summary.")
	} else {
		userContent.WriteString("Transcript to summarize:\n")
		userContent.WriteString(transcript)
		userContent.WriteString("\n\nProduce a summary.")
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent.String()},
	}

	resp, err := c.chat.ChatCompletion(ctx, c.chatModel, messages)
	if err != nil {
		return "", fmt.Errorf("ai.ChatService.SummarizeHistory: chat completion: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// HandleChatTask is a TaskHandler for chat tasks.
func (c *ChatService) HandleChatTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload ChatPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.ChatService.HandleChatTask: unmarshal payload: %w", err)
	}

	result, err := c.Ask(ctx, task.UserID, payload.Query, payload.History, payload.Summary)
	if err != nil {
		return nil, err
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("ai.ChatService.HandleChatTask: marshal result: %w", err)
	}

	return resultJSON, nil
}
