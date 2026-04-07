package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// maxRecordingDuration is the maximum allowed recording duration.
const maxRecordingDuration = 5 * 60 // 5 minutes in seconds

// voiceCaptureModel handles voice capture in the TUI.
// It uses the system sox command (rec) to record audio, then
// uploads the recording to the capture endpoint.
type voiceCaptureModel struct {
	client    *APIClient
	recording bool
	err       string
	loading   bool
	done      bool
	created   bool
	seconds   int
	cmd       *exec.Cmd
	audioFile string
	width     int
	height    int
}

// voiceTickMsg increments the recording timer.
type voiceTickMsg struct{}

// voiceRecordDoneMsg is sent when recording finishes.
type voiceRecordDoneMsg struct {
	audioFile string
}

// voiceUploadDoneMsg is sent when the recording is uploaded.
type voiceUploadDoneMsg struct {
	note *Note
}

func newVoiceCaptureModel(client *APIClient, width, height int) voiceCaptureModel {
	return voiceCaptureModel{
		client: client,
		width:  width,
		height: height,
	}
}

func (m voiceCaptureModel) Init() tea.Cmd {
	return nil
}

func (m voiceCaptureModel) Update(msg tea.Msg) (voiceCaptureModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case voiceTickMsg:
		if m.recording {
			m.seconds++
			// Auto-stop at maximum duration.
			if m.seconds >= maxRecordingDuration {
				if m.cmd != nil && m.cmd.Process != nil {
					_ = m.cmd.Process.Kill()
				}
				m.recording = false
				audioFile := m.audioFile
				return m, func() tea.Msg {
					return voiceRecordDoneMsg{audioFile: audioFile}
				}
			}
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return voiceTickMsg{}
			})
		}
		return m, nil

	case voiceRecordDoneMsg:
		m.recording = false
		m.loading = true
		m.audioFile = msg.audioFile
		client := m.client
		audioFile := msg.audioFile
		return m, func() tea.Msg {
			note, err := uploadVoice(client, audioFile)
			if err != nil {
				return apiErrorMsg{err: err}
			}
			return voiceUploadDoneMsg{note: note}
		}

	case voiceUploadDoneMsg:
		m.loading = false
		m.done = true
		m.created = true
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.recording = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyPressMsg:
		m.err = ""
		switch msg.String() {
		case "esc":
			if m.recording {
				// Stop recording and cancel.
				if m.cmd != nil && m.cmd.Process != nil {
					_ = m.cmd.Process.Kill()
				}
				m.recording = false
			}
			// Clean up temp file on cancel.
			if m.audioFile != "" {
				_ = os.Remove(m.audioFile)
			}
			m.done = true
			return m, nil

		case "enter", "space":
			if m.loading {
				return m, nil
			}
			if m.recording {
				// Stop recording.
				if m.cmd != nil && m.cmd.Process != nil {
					_ = m.cmd.Process.Kill()
				}
				m.recording = false
				audioFile := m.audioFile
				return m, func() tea.Msg {
					return voiceRecordDoneMsg{audioFile: audioFile}
				}
			}
			// Start recording.
			return m.startRecording()
		}
	}

	return m, nil
}

func (m voiceCaptureModel) startRecording() (voiceCaptureModel, tea.Cmd) {
	// Create a temporary file for the recording in the user config dir
	// to avoid TOCTOU issues in shared /tmp.
	tmpDir, _ := os.UserConfigDir()
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	tmpFile, err := os.CreateTemp(tmpDir, "seam-voice-*.wav")
	if err != nil {
		m.err = fmt.Sprintf("failed to create temp file: %v", err)
		return m, nil
	}
	audioFile := tmpFile.Name()
	tmpFile.Close()
	m.audioFile = audioFile

	// Try to find a recording command: sox/rec, arecord, parecord, or ffmpeg.
	// The ffmpeg input format is OS-specific:
	//   macOS: -f avfoundation -i ":0"
	//   Linux: -f alsa -i default (or -f pulse -i default)
	var cmd *exec.Cmd
	if _, lookErr := exec.LookPath("rec"); lookErr == nil {
		// sox rec command: records to wav (cross-platform)
		cmd = exec.Command("rec", audioFile, "rate", "16000", "channels", "1")
	} else if runtime.GOOS == "linux" {
		// Linux-specific recording tools.
		if _, lookErr := exec.LookPath("arecord"); lookErr == nil {
			cmd = exec.Command("arecord", "-f", "cd", "-t", "wav", audioFile)
		} else if _, lookErr := exec.LookPath("parecord"); lookErr == nil {
			cmd = exec.Command("parecord", "--file-format=wav", audioFile)
		} else if _, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
			cmd = exec.Command("ffmpeg", "-y", "-f", "alsa", "-i", "default", audioFile)
		}
	} else if runtime.GOOS == "darwin" {
		// macOS-specific recording tools.
		if _, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
			cmd = exec.Command("ffmpeg", "-y", "-f", "avfoundation", "-i", ":0", audioFile)
		}
	}
	if cmd == nil {
		// Fallback: try ffmpeg with a generic input.
		if _, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
			cmd = exec.Command("ffmpeg", "-y", "-f", "alsa", "-i", "default", audioFile)
		}
	}
	if cmd == nil {
		_ = os.Remove(audioFile)
		m.err = "no audio recording tool found (install sox, arecord, parecord, or ffmpeg)"
		return m, nil
	}

	// Suppress stdout/stderr to avoid corrupting the TUI.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		m.err = fmt.Sprintf("failed to start recording: %v", err)
		return m, nil
	}

	m.cmd = cmd
	m.recording = true
	m.seconds = 0

	return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
		return voiceTickMsg{}
	})
}

// uploadVoice reads the audio file and uploads it via multipart form.
func uploadVoice(client *APIClient, audioFile string) (*Note, error) {
	data, err := os.ReadFile(audioFile)
	if err != nil {
		return nil, fmt.Errorf("read audio file: %w", err)
	}
	defer func() {
		// Clean up the temp file.
		_ = os.Remove(audioFile)
	}()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("audio", "recording.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}
	writer.Close()

	req, err := http.NewRequest("POST", client.BaseURL+"/api/capture", &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if client.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+client.AccessToken)
	}

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Read body but do not expose raw server error details to user.
		io.ReadAll(resp.Body) //nolint:errcheck // best-effort read
		return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	var note Note
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if err := json.Unmarshal(respBody, &note); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &note, nil
}

func (m voiceCaptureModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(styleTitle.Render("Voice Capture"))
	b.WriteString("\n\n")

	if m.recording {
		remaining := maxRecordingDuration - m.seconds
		b.WriteString(styleError.Render("  Recording..."))
		b.WriteString(fmt.Sprintf("  %02d:%02d / %02d:%02d", m.seconds/60, m.seconds%60, maxRecordingDuration/60, maxRecordingDuration%60))
		if remaining <= 30 {
			b.WriteString(styleError.Render(fmt.Sprintf("  (%ds left)", remaining)))
		}
		b.WriteString("\n\n")
		b.WriteString(styleMuted.Render("Press Enter or Space to stop recording"))
	} else if m.loading {
		b.WriteString(styleMuted.Render("  Transcribing audio..."))
	} else {
		b.WriteString(styleMuted.Render("  Press Enter or Space to start recording"))
	}

	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(styleError.Render(m.err))
		b.WriteString("\n\n")
	}

	help := styleMuted.Render("Enter/Space: start/stop | Esc: cancel")
	b.WriteString(help)

	content := b.String()
	formWidth := 54
	box := lipgloss.NewStyle().
		Width(formWidth).
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary)

	rendered := box.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
