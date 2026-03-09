package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// VoiceTranscriber transcribes audio using a local whisper.cpp binary.
type VoiceTranscriber struct {
	binaryPath string // path to whisper-cli binary
	modelPath  string // path to ggml model file
	timeout    time.Duration
}

// NewVoiceTranscriber creates a new VoiceTranscriber that shells out to whisper-cli.
// binaryPath is the path to the whisper-cli executable (or just "whisper-cli" if on PATH).
// modelPath is the path to the ggml model file (e.g. ggml-base.en.bin).
func NewVoiceTranscriber(binaryPath, modelPath string) *VoiceTranscriber {
	if binaryPath == "" {
		binaryPath = "whisper-cli"
	}
	return &VoiceTranscriber{
		binaryPath: binaryPath,
		modelPath:  modelPath,
		timeout:    120 * time.Second,
	}
}

// TranscribeResult holds the result of a voice transcription.
type TranscribeResult struct {
	Text string
}

// whisperSegment represents a single segment in whisper-cli JSON output.
type whisperSegment struct {
	Text string `json:"text"`
}

// whisperOutput represents the top-level whisper-cli JSON output.
type whisperOutput struct {
	Transcription []whisperSegment `json:"transcription"`
}

// whisperNativeFormats are the audio formats whisper-cli supports directly.
var whisperNativeFormats = map[string]bool{
	".wav":  true,
	".mp3":  true,
	".ogg":  true,
	".flac": true,
}

// Transcribe writes audio data to a temp file, runs whisper-cli, and returns the text.
// If the audio format is not natively supported by whisper-cli (e.g. .webm),
// it is first converted to WAV using ffmpeg.
func (t *VoiceTranscriber) Transcribe(ctx context.Context, audio io.Reader, filename string) (*TranscribeResult, error) {
	if filename == "" {
		filename = "audio.wav"
	}

	// Determine file extension from filename.
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = ".wav"
	}

	// Write audio to a temp file (whisper-cli needs a file path).
	tmpFile, err := os.CreateTemp("", "seam-voice-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, audio); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: close temp file: %w", err)
	}

	// Convert to WAV if not a native whisper-cli format.
	audioPath := tmpPath
	if !whisperNativeFormats[ext] {
		wavPath := tmpPath + ".wav"
		defer os.Remove(wavPath)

		convertCtx, convertCancel := context.WithTimeout(ctx, 30*time.Second)
		defer convertCancel()

		ffCmd := exec.CommandContext(convertCtx, "ffmpeg",
			"-y",          // overwrite output
			"-i", tmpPath, // input file
			"-ar", "16000", // 16kHz sample rate (optimal for Whisper)
			"-ac", "1", // mono
			"-f", "wav", // output format
			wavPath,
		)
		ffOut, ffErr := ffCmd.CombinedOutput()
		if ffErr != nil {
			return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: ffmpeg convert: %w: %s", ffErr, string(ffOut))
		}
		audioPath = wavPath
	}

	// Build output file path (whisper-cli appends .json to --output-file).
	outBase := tmpPath + "-out"
	outJSON := outBase + ".json"
	defer os.Remove(outJSON)

	// Run whisper-cli with JSON output.
	whisperCtx, whisperCancel := context.WithTimeout(ctx, t.timeout)
	defer whisperCancel()

	cmd := exec.CommandContext(whisperCtx, t.binaryPath,
		"-m", t.modelPath,
		"-l", "en",
		"--no-prints",
		"-oj",
		"-of", outBase,
		audioPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: whisper-cli: %w: %s", err, string(output))
	}

	// Read the JSON output file.
	jsonData, err := os.ReadFile(outJSON)
	if err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: read output: %w", err)
	}

	var result whisperOutput
	if err := json.Unmarshal(jsonData, &result); err != nil {
		return nil, fmt.Errorf("capture.VoiceTranscriber.Transcribe: parse output: %w", err)
	}

	// Concatenate all segment texts.
	var parts []string
	for _, seg := range result.Transcription {
		text := strings.TrimSpace(seg.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}

	return &TranscribeResult{Text: strings.Join(parts, " ")}, nil
}
