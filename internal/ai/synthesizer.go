package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/katata/seam/internal/userdb"
)

// defaultNoteLimit is the default maximum number of notes retrieved for synthesis.
const defaultNoteLimit = 50

// Synthesizer generates summaries and synthesis across notes.
type Synthesizer struct {
	ollama    *OllamaClient
	dbManager userdb.Manager
	chatModel string
	logger    *slog.Logger
	noteLimit int // maximum notes to retrieve for synthesis
}

// NewSynthesizer creates a new Synthesizer. Optional noteLimit can be set;
// zero uses the default (50).
func NewSynthesizer(ollama *OllamaClient, dbManager userdb.Manager, chatModel string, logger *slog.Logger, opts ...func(*Synthesizer)) *Synthesizer {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Synthesizer{
		ollama:    ollama,
		dbManager: dbManager,
		chatModel: chatModel,
		logger:    logger,
		noteLimit: defaultNoteLimit,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithNoteLimit returns an option that sets the maximum number of notes for synthesis.
func WithNoteLimit(limit int) func(*Synthesizer) {
	return func(s *Synthesizer) {
		if limit > 0 {
			s.noteLimit = limit
		}
	}
}

// SynthesisResult contains the synthesis output.
type SynthesisResult struct {
	Response string `json:"response"`
}

// synthesisBodyMaxLen is the maximum body length loaded from SQL for synthesis.
// Using SUBSTR in the query avoids loading full note bodies into memory.
const synthesisBodyMaxLen = 1500

// retrieveNotes fetches notes matching the given scope from the user's database.
// Bodies are truncated at the SQL level to avoid loading excessive data into memory.
func (s *Synthesizer) retrieveNotes(ctx context.Context, userID string, payload SynthesizePayload) ([]struct{ Title, Body string }, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: open db: %w", err)
	}

	var rows []struct{ Title, Body string }

	switch payload.Scope {
	case "project":
		if payload.ProjectID == "" {
			return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: project_id required for project scope")
		}
		dbRows, err := db.QueryContext(ctx,
			`SELECT title, SUBSTR(body, 1, ?) FROM notes WHERE project_id = ? ORDER BY updated_at DESC LIMIT ?`,
			synthesisBodyMaxLen, payload.ProjectID, s.noteLimit)
		if err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: query notes: %w", err)
		}
		defer dbRows.Close()
		for dbRows.Next() {
			var r struct{ Title, Body string }
			if err := dbRows.Scan(&r.Title, &r.Body); err != nil {
				return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: scan: %w", err)
			}
			rows = append(rows, r)
		}
		if err := dbRows.Err(); err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: rows iteration: %w", err)
		}

	case "tag":
		if payload.Tag == "" {
			return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: tag required for tag scope")
		}
		dbRows, err := db.QueryContext(ctx,
			`SELECT n.title, SUBSTR(n.body, 1, ?) FROM notes n
			 JOIN note_tags nt ON n.id = nt.note_id
			 JOIN tags t ON nt.tag_id = t.id
			 WHERE t.name = ?
			 ORDER BY n.updated_at DESC LIMIT ?`,
			synthesisBodyMaxLen, payload.Tag, s.noteLimit)
		if err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: query notes by tag: %w", err)
		}
		defer dbRows.Close()
		for dbRows.Next() {
			var r struct{ Title, Body string }
			if err := dbRows.Scan(&r.Title, &r.Body); err != nil {
				return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: scan: %w", err)
			}
			rows = append(rows, r)
		}
		if err := dbRows.Err(); err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: rows iteration: %w", err)
		}

	default:
		return nil, fmt.Errorf("ai.Synthesizer.retrieveNotes: invalid scope %q, must be 'project' or 'tag'", payload.Scope)
	}

	return rows, nil
}

// Synthesize generates a synthesis across notes matching the given scope.
func (s *Synthesizer) Synthesize(ctx context.Context, userID string, payload SynthesizePayload) (*SynthesisResult, error) {
	rows, err := s.retrieveNotes(ctx, userID, payload)
	if err != nil {
		return nil, fmt.Errorf("ai.Synthesizer.Synthesize: %w", err)
	}

	if len(rows) == 0 {
		return &SynthesisResult{Response: "No notes found matching the given scope."}, nil
	}

	messages := BuildSynthesisMessages(payload.Prompt, rows)

	resp, err := s.ollama.ChatCompletion(ctx, s.chatModel, messages)
	if err != nil {
		return nil, fmt.Errorf("ai.Synthesizer.Synthesize: chat completion: %w", err)
	}

	return &SynthesisResult{Response: resp.Content}, nil
}

// SynthesizeStream is like Synthesize but returns a true streaming response,
// yielding tokens as they arrive from Ollama.
func (s *Synthesizer) SynthesizeStream(ctx context.Context, userID string, payload SynthesizePayload) (<-chan string, <-chan error) {
	tokenCh := make(chan string, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		rows, err := s.retrieveNotes(ctx, userID, payload)
		if err != nil {
			errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: %w", err)
			return
		}

		if len(rows) == 0 {
			tokenCh <- "No notes found matching the given scope."
			return
		}

		messages := BuildSynthesisMessages(payload.Prompt, rows)
		ollamaTokenCh, ollamaErrCh := s.ollama.ChatCompletionStream(ctx, s.chatModel, messages)

		for token := range ollamaTokenCh {
			select {
			case tokenCh <- token:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
		for streamErr := range ollamaErrCh {
			if streamErr != nil {
				errCh <- streamErr
			}
		}
	}()

	return tokenCh, errCh
}

// BuildSynthesisMessages constructs the messages for synthesis.
// Exported for testing.
func BuildSynthesisMessages(prompt string, notes []struct{ Title, Body string }) []ChatMessage {
	systemPrompt := `You are Seam, an AI assistant that synthesizes information from the user's notes. 
Given a collection of notes and a prompt, provide a thoughtful synthesis that identifies patterns, 
key themes, and important connections across the notes. Be specific and reference note titles when relevant.`

	var noteParts []string
	for _, n := range notes {
		// Bodies are already truncated at the SQL level (SUBSTR) in retrieveNotes.
		noteParts = append(noteParts, fmt.Sprintf("--- %s ---\n%s", n.Title, n.Body))
	}

	noteContext := strings.Join(noteParts, "\n\n")

	return []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Here are the notes:\n\n%s\n\nPrompt: %s", noteContext, prompt)},
	}
}

// HandleSynthesizeTask is a TaskHandler for synthesis tasks.
func (s *Synthesizer) HandleSynthesizeTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload SynthesizePayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.Synthesizer.HandleSynthesizeTask: unmarshal payload: %w", err)
	}

	result, err := s.Synthesize(ctx, task.UserID, payload)
	if err != nil {
		return nil, err
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("ai.Synthesizer.HandleSynthesizeTask: marshal result: %w", err)
	}

	return resultJSON, nil
}
