package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

func TestTranscribe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
		require.True(t, strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data"))

		// Verify multipart form contains model and file.
		err := r.ParseMultipartForm(10 << 20)
		require.NoError(t, err)
		require.Equal(t, "whisper", r.FormValue("model"))

		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		require.Equal(t, "test.wav", header.Filename)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"text": "This is the transcribed text from the audio recording.",
		})
	}))
	defer srv.Close()

	transcriber := NewVoiceTranscriber(srv.URL, "whisper")
	transcriber.client = srv.Client()

	result, err := transcriber.Transcribe(context.Background(), strings.NewReader("fake audio data"), "test.wav")
	require.NoError(t, err)
	require.Equal(t, "This is the transcribed text from the audio recording.", result.Text)
}

func TestTranscribe_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("model not loaded"))
	}))
	defer srv.Close()

	transcriber := NewVoiceTranscriber(srv.URL, "whisper")
	transcriber.client = srv.Client()

	_, err := transcriber.Transcribe(context.Background(), strings.NewReader("fake audio"), "test.wav")
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 500")
}

func TestTranscribe_DefaultFilename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(10 << 20)
		require.NoError(t, err)

		_, header, err := r.FormFile("file")
		require.NoError(t, err)
		require.Equal(t, "audio.wav", header.Filename)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": "hello"})
	}))
	defer srv.Close()

	transcriber := NewVoiceTranscriber(srv.URL, "whisper")
	transcriber.client = srv.Client()

	result, err := transcriber.Transcribe(context.Background(), strings.NewReader("audio"), "")
	require.NoError(t, err)
	require.Equal(t, "hello", result.Text)
}

func TestGenerateTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short text becomes title",
			input:    "Quick meeting notes",
			expected: "Quick meeting notes",
		},
		{
			name:     "long text is truncated",
			input:    "This is a very long transcription that goes on and on and on about many different topics that were discussed during the meeting",
			expected: "This is a very long transcription that goes on and on and...",
		},
		{
			name:     "multiline uses first line",
			input:    "First line here\nSecond line here",
			expected: "First line here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateTitle(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTranscribe_EmptyAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/audio/transcriptions", r.URL.Path)

		err := r.ParseMultipartForm(10 << 20)
		require.NoError(t, err)

		file, _, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": ""})
	}))
	defer srv.Close()

	transcriber := NewVoiceTranscriber(srv.URL, "whisper")
	transcriber.client = srv.Client()

	result, err := transcriber.Transcribe(context.Background(), strings.NewReader(""), "empty.wav")
	require.NoError(t, err)
	require.Equal(t, "", result.Text)
}

func TestService_SetSummarizeFunc(t *testing.T) {
	var called bool
	var capturedUserID, capturedNoteID string

	svc := NewService(nil, nil, nil, nil)
	svc.SetSummarizeFunc(func(ctx context.Context, userID, noteID string) {
		called = true
		capturedUserID = userID
		capturedNoteID = noteID
	})

	require.NotNil(t, svc.onSummarize)

	svc.onSummarize(context.Background(), "user1", "note1")

	require.True(t, called)
	require.Equal(t, "user1", capturedUserID)
	require.Equal(t, "note1", capturedNoteID)
}

func TestHandler_VoiceCapture_TranscriberNotConfigured(t *testing.T) {
	// Service with nil transcriber -- voice capture should fail.
	svc := NewService(nil, nil, nil, nil)
	handler := NewHandler(svc, nil)

	// Build a valid multipart form with an audio field.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("audio", "recording.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake audio data"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/capture/", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user-id")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/api/capture/", handler.capture)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp["error"], "voice capture failed")
}
