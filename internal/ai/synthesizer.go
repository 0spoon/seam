package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/katata/seam/internal/userdb"
)

// Synthesizer generates summaries and synthesis across notes.
type Synthesizer struct {
	ollama    *OllamaClient
	dbManager userdb.Manager
	chatModel string
	logger    *slog.Logger
}

// NewSynthesizer creates a new Synthesizer.
func NewSynthesizer(ollama *OllamaClient, dbManager userdb.Manager, chatModel string, logger *slog.Logger) *Synthesizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Synthesizer{
		ollama:    ollama,
		dbManager: dbManager,
		chatModel: chatModel,
		logger:    logger,
	}
}

// SynthesisResult contains the synthesis output.
type SynthesisResult struct {
	Response string `json:"response"`
}

// Synthesize generates a synthesis across notes matching the given scope.
func (s *Synthesizer) Synthesize(ctx context.Context, userID string, payload SynthesizePayload) (*SynthesisResult, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ai.Synthesizer.Synthesize: open db: %w", err)
	}

	// Retrieve notes matching the scope.
	var rows []struct {
		Title string
		Body  string
	}

	switch payload.Scope {
	case "project":
		if payload.ProjectID == "" {
			return nil, fmt.Errorf("ai.Synthesizer.Synthesize: project_id required for project scope")
		}
		dbRows, err := db.QueryContext(ctx,
			`SELECT title, body FROM notes WHERE project_id = ? ORDER BY updated_at DESC LIMIT 50`,
			payload.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.Synthesize: query notes: %w", err)
		}
		defer dbRows.Close()
		for dbRows.Next() {
			var r struct{ Title, Body string }
			if err := dbRows.Scan(&r.Title, &r.Body); err != nil {
				return nil, fmt.Errorf("ai.Synthesizer.Synthesize: scan: %w", err)
			}
			rows = append(rows, r)
		}
		if err := dbRows.Err(); err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.Synthesize: rows iteration: %w", err)
		}

	case "tag":
		if payload.Tag == "" {
			return nil, fmt.Errorf("ai.Synthesizer.Synthesize: tag required for tag scope")
		}
		dbRows, err := db.QueryContext(ctx,
			`SELECT n.title, n.body FROM notes n
			 JOIN note_tags nt ON n.id = nt.note_id
			 JOIN tags t ON nt.tag_id = t.id
			 WHERE t.name = ?
			 ORDER BY n.updated_at DESC LIMIT 50`,
			payload.Tag)
		if err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.Synthesize: query notes by tag: %w", err)
		}
		defer dbRows.Close()
		for dbRows.Next() {
			var r struct{ Title, Body string }
			if err := dbRows.Scan(&r.Title, &r.Body); err != nil {
				return nil, fmt.Errorf("ai.Synthesizer.Synthesize: scan: %w", err)
			}
			rows = append(rows, r)
		}
		if err := dbRows.Err(); err != nil {
			return nil, fmt.Errorf("ai.Synthesizer.Synthesize: rows iteration: %w", err)
		}

	default:
		return nil, fmt.Errorf("ai.Synthesizer.Synthesize: invalid scope %q, must be 'project' or 'tag'", payload.Scope)
	}

	if len(rows) == 0 {
		return &SynthesisResult{Response: "No notes found matching the given scope."}, nil
	}

	// Build the synthesis prompt.
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

		db, err := s.dbManager.Open(ctx, userID)
		if err != nil {
			errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: open db: %w", err)
			return
		}

		// Retrieve notes matching the scope (same logic as Synthesize).
		var rows []struct{ Title, Body string }

		switch payload.Scope {
		case "project":
			if payload.ProjectID == "" {
				errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: project_id required")
				return
			}
			dbRows, qErr := db.QueryContext(ctx,
				`SELECT title, body FROM notes WHERE project_id = ? ORDER BY updated_at DESC LIMIT 50`,
				payload.ProjectID)
			if qErr != nil {
				errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: query: %w", qErr)
				return
			}
			defer dbRows.Close()
			for dbRows.Next() {
				var r struct{ Title, Body string }
				if sErr := dbRows.Scan(&r.Title, &r.Body); sErr != nil {
					errCh <- sErr
					return
				}
				rows = append(rows, r)
			}
			if rErr := dbRows.Err(); rErr != nil {
				errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: rows iteration: %w", rErr)
				return
			}
		case "tag":
			if payload.Tag == "" {
				errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: tag required")
				return
			}
			dbRows, qErr := db.QueryContext(ctx,
				`SELECT n.title, n.body FROM notes n
				 JOIN note_tags nt ON n.id = nt.note_id
				 JOIN tags t ON nt.tag_id = t.id
				 WHERE t.name = ?
				 ORDER BY n.updated_at DESC LIMIT 50`,
				payload.Tag)
			if qErr != nil {
				errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: query by tag: %w", qErr)
				return
			}
			defer dbRows.Close()
			for dbRows.Next() {
				var r struct{ Title, Body string }
				if sErr := dbRows.Scan(&r.Title, &r.Body); sErr != nil {
					errCh <- sErr
					return
				}
				rows = append(rows, r)
			}
			if rErr := dbRows.Err(); rErr != nil {
				errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: rows iteration: %w", rErr)
				return
			}
		default:
			errCh <- fmt.Errorf("ai.Synthesizer.SynthesizeStream: invalid scope %q", payload.Scope)
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
		body := n.Body
		if len(body) > 1500 {
			body = body[:1500] + "..."
		}
		noteParts = append(noteParts, fmt.Sprintf("--- %s ---\n%s", n.Title, body))
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
