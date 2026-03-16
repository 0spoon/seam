package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/ws"
)

// jsonArrayRe matches a JSON array pattern in LLM output.
var jsonArrayRe = regexp.MustCompile(`\[[\s\S]*\]`)

// LinkSuggestion is a single auto-link suggestion.
type LinkSuggestion struct {
	TargetNoteID string `json:"target_note_id"`
	TargetTitle  string `json:"target_title"`
	Reason       string `json:"reason"`
}

// AutoLinker generates link suggestions between semantically related notes.
type AutoLinker struct {
	ollama     *OllamaClient
	chroma     *ChromaClient
	dbManager  userdb.Manager
	hub        *ws.Hub
	embedModel string
	chatModel  string
	logger     *slog.Logger
}

// NewAutoLinker creates a new AutoLinker.
func NewAutoLinker(ollama *OllamaClient, chroma *ChromaClient, dbManager userdb.Manager, embedModel, chatModel string, hub *ws.Hub, logger *slog.Logger) *AutoLinker {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoLinker{
		ollama:     ollama,
		chroma:     chroma,
		dbManager:  dbManager,
		hub:        hub,
		embedModel: embedModel,
		chatModel:  chatModel,
		logger:     logger,
	}
}

// relatedNoteInfo holds metadata about a related note for link suggestion.
type relatedNoteInfo struct {
	ID    string
	Title string
	Body  string
}

// SuggestLinks finds semantically similar notes and asks the LLM to suggest
// which ones should be linked and why.
func (l *AutoLinker) SuggestLinks(ctx context.Context, userID, noteID string) ([]LinkSuggestion, error) {
	db, err := l.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.SuggestLinks: open db: %w", err)
	}

	// Get the source note.
	var sourceTitle, sourceBody string
	err = db.QueryRowContext(ctx,
		`SELECT title, body FROM notes WHERE id = ?`, noteID,
	).Scan(&sourceTitle, &sourceBody)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.SuggestLinks: query source note: %w", err)
	}

	// Embed the source note.
	text := sourceTitle + "\n\n" + sourceBody
	if runes := []rune(text); len(runes) > 3000 {
		text = string(runes[:3000])
	}
	embedding, err := l.ollama.GenerateEmbedding(ctx, l.embedModel, text)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.SuggestLinks: embed source: %w", err)
	}

	// Find similar notes via ChromaDB.
	colName := CollectionName(userID)
	colID, err := l.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.SuggestLinks: get collection: %w", err)
	}

	chromaResults, err := l.chroma.Query(ctx, colID, embedding, 20)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.SuggestLinks: query chroma: %w", err)
	}

	// Deduplicate and exclude the source note.
	seen := map[string]bool{noteID: true}
	var related []relatedNoteInfo

	// Collect unique note IDs from ChromaDB results.
	var noteIDs []string
	for _, cr := range chromaResults {
		rid := cr.Metadata["note_id"]
		if rid == "" || seen[rid] {
			continue
		}
		seen[rid] = true
		noteIDs = append(noteIDs, rid)
		if len(noteIDs) >= 10 {
			break
		}
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
			noteMap := make(map[string]relatedNoteInfo)
			for rows.Next() {
				var id, title, body string
				if err := rows.Scan(&id, &title, &body); err != nil {
					continue
				}
				runes := []rune(body)
				if len(runes) > 500 {
					body = string(runes[:500]) + "..."
				}
				noteMap[id] = relatedNoteInfo{ID: id, Title: title, Body: body}
			}
			// Preserve the ChromaDB relevance order.
			for _, id := range noteIDs {
				if info, ok := noteMap[id]; ok {
					related = append(related, info)
				}
			}
		}
	}

	if len(related) == 0 {
		return nil, nil
	}

	// Ask the LLM for link suggestions.
	suggestions, err := l.askForSuggestions(ctx, sourceTitle, sourceBody, related)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.SuggestLinks: %w", err)
	}

	return suggestions, nil
}

func (l *AutoLinker) askForSuggestions(ctx context.Context, sourceTitle, sourceBody string, related []relatedNoteInfo) ([]LinkSuggestion, error) {
	if runes := []rune(sourceBody); len(runes) > 1000 {
		sourceBody = string(runes[:1000]) + "..."
	}

	var relatedParts []string
	for _, r := range related {
		relatedParts = append(relatedParts, fmt.Sprintf("ID: %s\nTitle: %s\nContent: %s", r.ID, r.Title, r.Body))
	}

	prompt := fmt.Sprintf(`Given this note:

Title: %s
Content: %s

And these potentially related notes:

%s

Which of these related notes should be linked from the source note? For each suggestion, explain why the link would be valuable. Only suggest links that are genuinely useful.

Respond in JSON format: [{"target_note_id": "...", "target_title": "...", "reason": "..."}]
Only output the JSON array, no other text.`, sourceTitle, sourceBody, strings.Join(relatedParts, "\n\n"))

	messages := []ChatMessage{
		{Role: "system", Content: "You are a helpful assistant that suggests links between related notes. Output only valid JSON."},
		{Role: "user", Content: prompt},
	}

	resp, err := l.ollama.ChatCompletion(ctx, l.chatModel, messages)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.askForSuggestions: %w", err)
	}

	// Parse the LLM response using a multi-strategy approach for robustness.
	var suggestions []LinkSuggestion
	content := strings.TrimSpace(resp.Content)

	// Strategy 1: Try direct unmarshal of the full response.
	if err := json.Unmarshal([]byte(content), &suggestions); err != nil {
		// Strategy 2: Try bracket-based extraction (handles markdown fences, preamble text).
		if idx := strings.Index(content, "["); idx >= 0 {
			if end := strings.LastIndex(content, "]"); end > idx {
				extracted := content[idx : end+1]
				if err2 := json.Unmarshal([]byte(extracted), &suggestions); err2 != nil {
					// Strategy 3: Regex-based extraction for nested or complex output.
					if match := jsonArrayRe.FindString(content); match != "" {
						if err3 := json.Unmarshal([]byte(match), &suggestions); err3 != nil {
							l.logger.Warn("ai.AutoLinker: failed to parse LLM suggestions after all strategies",
								"error", err3)
							return nil, nil
						}
					} else {
						l.logger.Warn("ai.AutoLinker: failed to parse LLM suggestions",
							"error", err2)
						return nil, nil // Non-fatal: LLM output was not valid JSON.
					}
				}
			}
		} else {
			l.logger.Warn("ai.AutoLinker: LLM response contains no JSON array",
				"error", err)
			return nil, nil
		}
	}

	// Filter to only suggestions referencing known note IDs.
	var valid []LinkSuggestion
	knownIDs := make(map[string]bool)
	for _, r := range related {
		knownIDs[r.ID] = true
	}
	for _, s := range suggestions {
		if knownIDs[s.TargetNoteID] {
			valid = append(valid, s)
		}
	}

	return valid, nil
}

// HandleAutolinkTask is a TaskHandler for autolink tasks.
func (l *AutoLinker) HandleAutolinkTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload AutolinkPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.HandleAutolinkTask: unmarshal payload: %w", err)
	}

	suggestions, err := l.SuggestLinks(ctx, task.UserID, payload.NoteID)
	if err != nil {
		return nil, err
	}

	resultJSON, err := json.Marshal(suggestions)
	if err != nil {
		return nil, fmt.Errorf("ai.AutoLinker.HandleAutolinkTask: marshal result: %w", err)
	}

	// Send note.link_suggestions WS message so the frontend can display them.
	if l.hub != nil && len(suggestions) > 0 {
		wsPayload, _ := json.Marshal(map[string]interface{}{
			"note_id":     payload.NoteID,
			"suggestions": suggestions,
		})
		l.hub.Send(task.UserID, ws.Message{
			Type:    ws.MsgTypeLinkSuggestions,
			Payload: wsPayload,
		})
	}

	return resultJSON, nil
}
