package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

	case tea.KeyMsg:
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
			m.done = true
			return m, nil

		case "enter", " ":
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
	// Try to find a recording command: sox/rec, arecord, or ffmpeg.
	audioFile := fmt.Sprintf("/tmp/seam-voice-%d.wav", time.Now().UnixNano())
	m.audioFile = audioFile

	var cmd *exec.Cmd
	if _, err := exec.LookPath("rec"); err == nil {
		// sox rec command: records to wav
		cmd = exec.Command("rec", audioFile, "rate", "16000", "channels", "1")
	} else if _, err := exec.LookPath("arecord"); err == nil {
		// ALSA arecord
		cmd = exec.Command("arecord", "-f", "cd", "-t", "wav", audioFile)
	} else if _, err := exec.LookPath("ffmpeg"); err == nil {
		// ffmpeg with default audio input
		cmd = exec.Command("ffmpeg", "-y", "-f", "avfoundation", "-i", ":0", audioFile)
	} else {
		m.err = "no audio recording tool found (install sox, arecord, or ffmpeg)"
		return m, nil
	}

	// Suppress stdout/stderr to avoid corrupting the TUI.
	cmd.Stdout = nil
	cmd.Stderr = nil

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
	// Read the file data via exec since we cannot import os directly
	// for TUI simplicity. Actually we can use os.Open.
	data, err := exec.Command("cat", audioFile).Output()
	if err != nil {
		return nil, fmt.Errorf("read audio file: %w", err)
	}
	defer func() {
		// Clean up the temp file.
		_ = exec.Command("rm", "-f", audioFile).Run()
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
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
		b.WriteString(styleError.Render("  Recording..."))
		b.WriteString(fmt.Sprintf("  %02d:%02d", m.seconds/60, m.seconds%60))
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
