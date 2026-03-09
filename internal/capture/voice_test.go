package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

// fakeWhisperScript creates a shell script that mimics whisper-cli.
// It writes a JSON output file to the path derived from -of flag.
func fakeWhisperScript(t *testing.T, text string) string {
	t.Helper()

	dir := t.TempDir()
	script := filepath.Join(dir, "whisper-cli")

	// The script parses -of to find the output file base, then writes .json.
	content := `#!/bin/sh
OF=""
while [ $# -gt 0 ]; do
  case "$1" in
    -of) OF="$2"; shift 2;;
    *) shift;;
  esac
done
if [ -n "$OF" ]; then
  cat > "${OF}.json" <<'JSONEOF'
{"transcription":[{"text":"` + text + `"}]}
JSONEOF
fi
`
	err := os.WriteFile(script, []byte(content), 0o755)
	require.NoError(t, err)
	return script
}

// fakeWhisperScriptError creates a script that exits with an error.
func fakeWhisperScriptError(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	script := filepath.Join(dir, "whisper-cli")
	content := `#!/bin/sh
echo "error: model not found" >&2
exit 1
`
	err := os.WriteFile(script, []byte(content), 0o755)
	require.NoError(t, err)
	return script
}

// fakeWhisperScriptEmpty creates a script that outputs empty transcription.
func fakeWhisperScriptEmpty(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	script := filepath.Join(dir, "whisper-cli")
	content := `#!/bin/sh
OF=""
while [ $# -gt 0 ]; do
  case "$1" in
    -of) OF="$2"; shift 2;;
    *) shift;;
  esac
done
if [ -n "$OF" ]; then
  cat > "${OF}.json" <<'JSONEOF'
{"transcription":[{"text":""}]}
JSONEOF
fi
`
	err := os.WriteFile(script, []byte(content), 0o755)
	require.NoError(t, err)
	return script
}

func TestTranscribe_Success(t *testing.T) {
	script := fakeWhisperScript(t, "This is the transcribed text from the audio recording.")

	// Model path can be any file -- the fake script ignores it.
	modelPath := filepath.Join(t.TempDir(), "fake-model.bin")
	require.NoError(t, os.WriteFile(modelPath, []byte("fake"), 0o644))

	transcriber := NewVoiceTranscriber(script, modelPath)

	result, err := transcriber.Transcribe(context.Background(), bytes.NewReader([]byte("fake audio data")), "test.wav")
	require.NoError(t, err)
	require.Equal(t, "This is the transcribed text from the audio recording.", result.Text)
}

func TestTranscribe_CLIError(t *testing.T) {
	script := fakeWhisperScriptError(t)
	modelPath := filepath.Join(t.TempDir(), "fake-model.bin")
	require.NoError(t, os.WriteFile(modelPath, []byte("fake"), 0o644))

	transcriber := NewVoiceTranscriber(script, modelPath)

	_, err := transcriber.Transcribe(context.Background(), bytes.NewReader([]byte("fake audio")), "test.wav")
	require.Error(t, err)
	require.Contains(t, err.Error(), "whisper-cli")
}

func TestTranscribe_BinaryNotFound(t *testing.T) {
	transcriber := NewVoiceTranscriber("/nonexistent/whisper-cli", "/nonexistent/model.bin")

	_, err := transcriber.Transcribe(context.Background(), bytes.NewReader([]byte("audio")), "test.wav")
	require.Error(t, err)
}

func TestTranscribe_DefaultFilename(t *testing.T) {
	script := fakeWhisperScript(t, "hello")
	modelPath := filepath.Join(t.TempDir(), "fake-model.bin")
	require.NoError(t, os.WriteFile(modelPath, []byte("fake"), 0o644))

	transcriber := NewVoiceTranscriber(script, modelPath)

	result, err := transcriber.Transcribe(context.Background(), bytes.NewReader([]byte("audio")), "")
	require.NoError(t, err)
	require.Equal(t, "hello", result.Text)
}

func TestTranscribe_EmptyResult(t *testing.T) {
	script := fakeWhisperScriptEmpty(t)
	modelPath := filepath.Join(t.TempDir(), "fake-model.bin")
	require.NoError(t, os.WriteFile(modelPath, []byte("fake"), 0o644))

	transcriber := NewVoiceTranscriber(script, modelPath)

	result, err := transcriber.Transcribe(context.Background(), bytes.NewReader([]byte("")), "empty.wav")
	require.NoError(t, err)
	require.Equal(t, "", result.Text)
}

func TestTranscribe_ContextCancelled(t *testing.T) {
	// Use a real binary that would take time, but cancel immediately.
	_, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not available")
	}

	// Create a script that sleeps forever.
	dir := t.TempDir()
	script := filepath.Join(dir, "whisper-cli")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nsleep 60\n"), 0o755))
	modelPath := filepath.Join(dir, "fake-model.bin")
	require.NoError(t, os.WriteFile(modelPath, []byte("fake"), 0o644))

	transcriber := NewVoiceTranscriber(script, modelPath)
	transcriber.timeout = 1 // 1 nanosecond -- will timeout immediately

	ctx := context.Background()
	_, err = transcriber.Transcribe(ctx, bytes.NewReader([]byte("audio")), "test.wav")
	require.Error(t, err)
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

func TestNewVoiceTranscriber_DefaultBinary(t *testing.T) {
	vt := NewVoiceTranscriber("", "/some/model.bin")
	require.Equal(t, "whisper-cli", vt.binaryPath)
	require.Equal(t, "/some/model.bin", vt.modelPath)
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
