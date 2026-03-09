package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/katata/seam/internal/userdb"
)

const maxConversationTurns = 5

// ChatService handles RAG-powered chat using note context.
type ChatService struct {
	ollama     *OllamaClient
	chroma     *ChromaClient
	dbManager  userdb.Manager
	embedModel string
	chatModel  string
	logger     *slog.Logger
}

// NewChatService creates a new ChatService.
func NewChatService(ollama *OllamaClient, chroma *ChromaClient, dbManager userdb.Manager, embedModel, chatModel string, logger *slog.Logger) *ChatService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChatService{
		ollama:     ollama,
		chroma:     chroma,
		dbManager:  dbManager,
		embedModel: embedModel,
		chatModel:  chatModel,
		logger:     logger,
	}
}

// ChatResult contains the response and cited note IDs.
type ChatResult struct {
	Response  string   `json:"response"`
	Citations []string `json:"citations"`
}

// noteSnippet holds retrieved note content for prompt construction.
type noteSnippet struct {
	Title string
	Body  string
}

// Ask handles a chat question by retrieving relevant notes and generating
// a response grounded in the user's knowledge base.
func (c *ChatService) Ask(ctx context.Context, userID, query string, history []ChatMessage) (*ChatResult, error) {
	contexts, citations, err := c.retrieveContext(ctx, userID, query)
	if err != nil {
		return nil, err
	}

	messages := BuildChatMessages(query, contexts, history)

	resp, err := c.ollama.ChatCompletion(ctx, c.chatModel, messages)
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
func (c *ChatService) AskStream(ctx context.Context, userID, query string, history []ChatMessage) (<-chan string, []string, <-chan error) {
	tokenCh := make(chan string, 64)
	errCh := make(chan error, 1)

	contexts, citations, err := c.retrieveContext(ctx, userID, query)
	if err != nil {
		close(tokenCh)
		errCh <- err
		close(errCh)
		return tokenCh, nil, errCh
	}

	messages := BuildChatMessages(query, contexts, history)

	ollamaTokenCh, ollamaErrCh := c.ollama.ChatCompletionStream(ctx, c.chatModel, messages)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		for token := range ollamaTokenCh {
			select {
			case tokenCh <- token:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
		for err := range ollamaErrCh {
			if err != nil {
				errCh <- err
			}
		}
	}()

	return tokenCh, citations, errCh
}

// retrieveContext embeds the query, retrieves relevant chunks from ChromaDB,
// and fetches the note content from the user's database.
func (c *ChatService) retrieveContext(ctx context.Context, userID, query string) ([]noteSnippet, []string, error) {
	queryEmbedding, err := c.ollama.GenerateEmbedding(ctx, c.embedModel, query)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: embed query: %w", err)
	}

	colName := CollectionName(userID)
	colID, err := c.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: get collection: %w", err)
	}

	chromaResults, err := c.chroma.Query(ctx, colID, queryEmbedding, 10)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: query chroma: %w", err)
	}

	db, err := c.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("ai.ChatService.retrieveContext: open db: %w", err)
	}

	seen := make(map[string]bool)
	var contexts []noteSnippet
	var citations []string

	for _, cr := range chromaResults {
		noteID := cr.Metadata["note_id"]
		if noteID == "" || seen[noteID] {
			continue
		}
		seen[noteID] = true

		var title, body string
		qErr := db.QueryRowContext(ctx,
			`SELECT title, body FROM notes WHERE id = ?`, noteID,
		).Scan(&title, &body)
		if qErr != nil {
			continue
		}

		// Truncate long bodies to avoid blowing up context window.
		if len(body) > 2000 {
			body = body[:2000] + "..."
		}

		contexts = append(contexts, noteSnippet{Title: title, Body: body})
		citations = append(citations, noteID)
	}

	return contexts, citations, nil
}

// BuildChatMessages constructs the messages for the RAG chat prompt.
// Exported for testing.
func BuildChatMessages(query string, noteContexts []noteSnippet, history []ChatMessage) []ChatMessage {
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

	var messages []ChatMessage
	messages = append(messages, ChatMessage{
		Role:    "system",
		Content: systemPrompt + contextStr,
	})

	// Add conversation history (limited to last N turns).
	historyStart := 0
	if len(history) > maxConversationTurns*2 {
		historyStart = len(history) - maxConversationTurns*2
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

// HandleChatTask is a TaskHandler for chat tasks.
func (c *ChatService) HandleChatTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload ChatPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.ChatService.HandleChatTask: unmarshal payload: %w", err)
	}

	result, err := c.Ask(ctx, task.UserID, payload.Query, payload.History)
	if err != nil {
		return nil, err
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("ai.ChatService.HandleChatTask: marshal result: %w", err)
	}

	return resultJSON, nil
}
