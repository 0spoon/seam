package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// VoiceTranscriber transcribes audio using an Ollama-compatible Whisper endpoint.
type VoiceTranscriber struct {
	ollamaBaseURL string
	model         string
	client        *http.Client
}

// NewVoiceTranscriber creates a new VoiceTranscriber.
func NewVoiceTranscriber(ollamaBaseURL, model string) *VoiceTranscriber {
	return &VoiceTranscriber{
		ollamaBaseURL: ollamaBaseURL,
		model:         model,
		client: &http.Client{
			Timeout: 120 * time.Second, // Transcription can take a while.
		},
	}
}

// TranscribeResult holds the result of a voice transcription.
type TranscribeResult struct {
	Text string
}

// Transcribe sends audio data to the Whisper endpoint for transcription.
// The audio parameter should contain the raw audio data (wav, mp3, etc.).
func (t *VoiceTranscriber) Transcribe(ctx context.Context, audio io.Reader, filename string) (*TranscribeResult, error) {
	if filename == "" {
		filename = "audio.wav"
	}

	// Build multipart request for Ollama audio endpoint.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the model field.
	if err := writer.WriteField("model", t.model); err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: write model field: %w", err)
	}

	// Add the audio file.
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: create form file: %w", err)
	}
	if _, err := io.Copy(part, audio); err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: copy audio: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: close writer: %w", err)
	}

	// Ollama uses /v1/audio/transcriptions (OpenAI-compatible endpoint).
	endpoint := t.ollamaBaseURL + "/v1/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: new request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: decode: %w", err)
	}

	return &TranscribeResult{Text: result.Text}, nil
}
