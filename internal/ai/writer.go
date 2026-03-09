package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/katata/seam/internal/userdb"
)

// Writer action types.
const (
	ActionExpand         = "expand"
	ActionSummarize      = "summarize"
	ActionExtractActions = "extract-actions"
)

// Domain errors for writer.
var (
	ErrInvalidAction = errors.New("invalid action")
	ErrEmptyInput    = errors.New("no text to process")
)

// AssistPayload is the JSON payload for assist tasks.
type AssistPayload struct {
	NoteID    string `json:"note_id"`
	Action    string `json:"action"`
	Selection string `json:"selection"`
}

// NoteBodyLoader loads a note's body by ID from a user's DB.
// This interface avoids the writer needing to know about note.Store internals
// or running raw SQL. Implemented by a thin adapter in cmd/seamd.
type NoteBodyLoader interface {
	LoadNoteBody(ctx context.Context, userID, noteID string) (string, error)
}

// NoteBodyUpdater updates a note's body in a user's DB.
type NoteBodyUpdater interface {
	UpdateNoteBody(ctx context.Context, userID, noteID, body string) error
}

// Writer provides AI writing assistance: expand, summarize, extract action items.
type Writer struct {
	mu          sync.RWMutex
	ollama      *OllamaClient
	bodyLoader  NoteBodyLoader
	bodyUpdater NoteBodyUpdater
	dbManager   userdb.Manager
	model       string
	logger      *slog.Logger
}

// NewWriter creates a new Writer.
func NewWriter(ollama *OllamaClient, dbManager userdb.Manager, model string, logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{
		ollama:    ollama,
		dbManager: dbManager,
		model:     model,
		logger:    logger,
	}
}

// SetNoteBodyLoader sets the loader for reading note bodies.
func (w *Writer) SetNoteBodyLoader(loader NoteBodyLoader) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bodyLoader = loader
}

// SetNoteBodyUpdater sets the updater for writing note bodies.
func (w *Writer) SetNoteBodyUpdater(updater NoteBodyUpdater) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bodyUpdater = updater
}

// getBodyLoader returns the current body loader under read lock.
func (w *Writer) getBodyLoader() NoteBodyLoader {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.bodyLoader
}

// getBodyUpdater returns the current body updater under read lock.
func (w *Writer) getBodyUpdater() NoteBodyUpdater {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.bodyUpdater
}

// Assist performs a writing assist action on the given text.
// If selection is non-empty, the LLM operates on the selected text.
// If selection is empty, the LLM operates on the full note body.
func (w *Writer) Assist(ctx context.Context, userID, noteID, action, selection string) (string, error) {
	// Resolve the text to operate on.
	text := selection
	if text == "" {
		// Load the full note body via the NoteBodyLoader interface (if available)
		// or fall back to direct DB query for backward compatibility.
		loader := w.getBodyLoader()
		if loader != nil {
			body, err := loader.LoadNoteBody(ctx, userID, noteID)
			if err != nil {
				return "", fmt.Errorf("ai.Writer.Assist: %w", err)
			}
			text = body
		} else {
			db, err := w.dbManager.Open(ctx, userID)
			if err != nil {
				return "", fmt.Errorf("ai.Writer.Assist: open db: %w", err)
			}
			var body string
			err = db.QueryRowContext(ctx,
				`SELECT body FROM notes WHERE id = ?`, noteID,
			).Scan(&body)
			if err != nil {
				return "", fmt.Errorf("ai.Writer.Assist: note not found: %w", err)
			}
			text = body
		}
	}

	if text == "" {
		return "", ErrEmptyInput
	}

	prompt, err := buildAssistPrompt(action, text)
	if err != nil {
		return "", err
	}

	messages := []ChatMessage{
		{Role: "system", Content: assistSystemPrompt},
		{Role: "user", Content: prompt},
	}

	resp, err := w.ollama.ChatCompletion(ctx, w.model, messages)
	if err != nil {
		return "", fmt.Errorf("ai.Writer.Assist: %w", err)
	}

	return resp.Content, nil
}

// HandleAssistTask processes an "assist" task from the queue.
func (w *Writer) HandleAssistTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload AssistPayload
	if err := unmarshalPayload(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.Writer.HandleAssistTask: %w", err)
	}

	result, err := w.Assist(ctx, task.UserID, payload.NoteID, payload.Action, payload.Selection)
	if err != nil {
		return nil, err
	}

	resultJSON, marshalErr := marshalResult(map[string]string{"result": result})
	if marshalErr != nil {
		w.logger.Error("ai.Writer.HandleAssistTask: marshal result failed", "error", marshalErr)
		return nil, marshalErr
	}
	return resultJSON, nil
}

// HandleSummarizeTranscriptTask processes a "summarize_transcript" task.
// It reads the note body, generates a summary via LLM, and prepends it.
func (w *Writer) HandleSummarizeTranscriptTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload SummarizeTranscriptPayload
	if err := unmarshalPayload(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.Writer.HandleSummarizeTranscriptTask: %w", err)
	}

	loader := w.getBodyLoader()
	var body string
	if loader != nil {
		var loadErr error
		body, loadErr = loader.LoadNoteBody(ctx, task.UserID, payload.NoteID)
		if loadErr != nil {
			return nil, fmt.Errorf("ai.Writer.HandleSummarizeTranscriptTask: %w", loadErr)
		}
	} else {
		db, err := w.dbManager.Open(ctx, task.UserID)
		if err != nil {
			return nil, fmt.Errorf("ai.Writer.HandleSummarizeTranscriptTask: open db: %w", err)
		}
		err = db.QueryRowContext(ctx,
			`SELECT body FROM notes WHERE id = ?`, payload.NoteID,
		).Scan(&body)
		if err != nil {
			return nil, fmt.Errorf("ai.Writer.HandleSummarizeTranscriptTask: note not found: %w", err)
		}
	}

	if body == "" {
		emptyResult, marshalErr := marshalResult(map[string]string{"result": ""})
		if marshalErr != nil {
			w.logger.Error("ai.Writer.HandleSummarizeTranscriptTask: marshal empty result failed", "error", marshalErr)
			return nil, marshalErr
		}
		return emptyResult, nil
	}

	messages := []ChatMessage{
		{Role: "system", Content: "You are a concise summarizer. Given a voice transcription, produce a brief summary (2-4 sentences) capturing the key points. Return only the summary text."},
		{Role: "user", Content: fmt.Sprintf("Summarize this transcription:\n\n%s", body)},
	}

	resp, err := w.ollama.ChatCompletion(ctx, w.model, messages)
	if err != nil {
		return nil, fmt.Errorf("ai.Writer.HandleSummarizeTranscriptTask: %w", err)
	}

	// Prepend summary to the note body. Use the note service via bodyUpdater
	// to ensure the .md file on disk (source of truth) is also updated.
	newBody := fmt.Sprintf("## Summary\n\n%s\n\n---\n\n%s", resp.Content, body)
	updater := w.getBodyUpdater()
	if updater != nil {
		if err := updater.UpdateNoteBody(ctx, task.UserID, payload.NoteID, newBody); err != nil {
			return nil, fmt.Errorf("ai.Writer.HandleSummarizeTranscriptTask: update note: %w", err)
		}
	} else {
		// No updater configured; direct DB writes bypass the note service and
		// would leave the .md file on disk out of sync. Log and skip.
		w.logger.Error("ai.Writer.HandleSummarizeTranscriptTask: no NoteBodyUpdater configured, skipping note update",
			"note_id", payload.NoteID, "user_id", task.UserID)
		return nil, fmt.Errorf("ai.Writer.HandleSummarizeTranscriptTask: no NoteBodyUpdater configured")
	}

	w.logger.Info("transcript summarized", "note_id", payload.NoteID, "user_id", task.UserID)
	summaryResult, marshalErr := marshalResult(map[string]string{"result": resp.Content})
	if marshalErr != nil {
		w.logger.Error("ai.Writer.HandleSummarizeTranscriptTask: marshal result failed", "error", marshalErr)
		return nil, marshalErr
	}
	return summaryResult, nil
}

const assistSystemPrompt = `You are a writing assistant for a personal knowledge management system. 
You help users improve their notes by expanding, summarizing, or extracting action items.
Always preserve the user's voice and style. Be concise and direct.
Return only the transformed text, without explanations or preambles.`

func buildAssistPrompt(action, text string) (string, error) {
	switch action {
	case ActionExpand:
		return fmt.Sprintf(`Expand the following text into well-structured paragraphs. 
Keep the core ideas but add depth, examples, and transitions.

Text to expand:
%s`, text), nil

	case ActionSummarize:
		return fmt.Sprintf(`Summarize the following text concisely, preserving key points and decisions.
Use bullet points for clarity.

Text to summarize:
%s`, text), nil

	case ActionExtractActions:
		return fmt.Sprintf(`Extract all action items and tasks from the following text.
Format each as a markdown checklist item (- [ ] task).
Include any deadlines or assignees mentioned.

Text to extract from:
%s`, text), nil

	default:
		return "", fmt.Errorf("%w: %s (valid: expand, summarize, extract-actions)", ErrInvalidAction, action)
	}
}

func unmarshalPayload(data []byte, v interface{}) error {
	if len(data) == 0 {
		return fmt.Errorf("empty payload")
	}
	return json.Unmarshal(data, v)
}

func marshalResult(v interface{}) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("ai.marshalResult: %w", err)
	}
	return json.RawMessage(b), nil
}
