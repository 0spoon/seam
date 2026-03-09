package capture

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/katata/seam/internal/note"
)

// SummarizeFunc is a callback that enqueues a background summarization task
// for a voice-captured note. Injected from main to avoid import cycle with ai.
type SummarizeFunc func(ctx context.Context, userID, noteID string)

// Service coordinates URL capture and voice transcription, creating notes
// from captured content.
type Service struct {
	noteSvc     *note.Service
	urlFetcher  *URLFetcher
	transcriber *VoiceTranscriber // nil if transcription model not configured
	onSummarize SummarizeFunc     // nil if AI not configured
	logger      *slog.Logger
}

// NewService creates a new capture Service.
func NewService(
	noteSvc *note.Service,
	urlFetcher *URLFetcher,
	transcriber *VoiceTranscriber,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		noteSvc:     noteSvc,
		urlFetcher:  urlFetcher,
		transcriber: transcriber,
		logger:      logger,
	}
}

// SetSummarizeFunc sets the callback for background summarization of voice captures.
// Called during server startup after the AI queue is initialized.
func (s *Service) SetSummarizeFunc(fn SummarizeFunc) {
	s.onSummarize = fn
}

// CaptureURL fetches the given URL, extracts content, and creates a note
// in the user's Inbox.
func (s *Service) CaptureURL(ctx context.Context, userID, rawURL string) (*note.Note, error) {
	content, err := s.urlFetcher.FetchURL(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("capture.Service.CaptureURL: %w", err)
	}

	// Build the note body with source attribution.
	body := fmt.Sprintf("Source: %s\n\n---\n\n%s", content.URL, content.Body)

	// Truncate body if excessively long (>50K chars).
	if len(body) > 50000 {
		body = body[:50000] + "\n\n[Content truncated]"
	}

	req := note.CreateNoteReq{
		Title:     content.Title,
		Body:      body,
		SourceURL: content.URL,
		// ProjectID intentionally empty = inbox
	}

	n, err := s.noteSvc.Create(ctx, userID, req)
	if err != nil {
		return nil, fmt.Errorf("capture.Service.CaptureURL: create note: %w", err)
	}

	s.logger.Info("URL captured",
		"user_id", userID,
		"note_id", n.ID,
		"url", rawURL,
		"title", content.Title,
	)

	return n, nil
}

// CaptureVoice transcribes audio and creates a note in the user's Inbox.
func (s *Service) CaptureVoice(ctx context.Context, userID string, audio io.Reader, filename string) (*note.Note, error) {
	if s.transcriber == nil {
		return nil, fmt.Errorf("capture.Service.CaptureVoice: voice transcription not configured (models.transcription not set)")
	}

	result, err := s.transcriber.Transcribe(ctx, audio, filename)
	if err != nil {
		return nil, fmt.Errorf("capture.Service.CaptureVoice: %w", err)
	}

	if result.Text == "" {
		return nil, fmt.Errorf("capture.Service.CaptureVoice: empty transcription")
	}

	// Generate a title from the first line or first 60 chars.
	title := generateTitle(result.Text)

	req := note.CreateNoteReq{
		Title:            title,
		Body:             result.Text,
		TranscriptSource: true,
		// ProjectID intentionally empty = inbox
	}

	n, err := s.noteSvc.Create(ctx, userID, req)
	if err != nil {
		return nil, fmt.Errorf("capture.Service.CaptureVoice: create note: %w", err)
	}

	// Enqueue background summarization task if AI is configured.
	if s.onSummarize != nil {
		s.onSummarize(ctx, userID, n.ID)
	}

	s.logger.Info("voice captured",
		"user_id", userID,
		"note_id", n.ID,
		"title", title,
	)

	return n, nil
}

// generateTitle creates a title from the transcription text.
func generateTitle(text string) string {
	// Use "Voice Note" with timestamp as default.
	title := fmt.Sprintf("Voice Note - %s", time.Now().UTC().Format("2006-01-02 15:04"))

	// Try to extract a meaningful first line.
	lines := splitLines(text)
	for _, line := range lines {
		line = trimLine(line)
		if line != "" {
			if len(line) > 60 {
				// Truncate at word boundary.
				line = line[:60]
				if idx := lastSpace(line); idx > 20 {
					line = line[:idx]
				}
				line += "..."
			}
			title = line
			break
		}
	}

	return title
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimLine(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func lastSpace(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ' ' {
			return i
		}
	}
	return -1
}
