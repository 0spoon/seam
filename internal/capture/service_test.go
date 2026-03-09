package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewService
// ---------------------------------------------------------------------------

func TestNewService_NilLogger_UsesDefault(t *testing.T) {
	svc := NewService(nil, nil, nil, nil)
	require.NotNil(t, svc)
	require.NotNil(t, svc.logger)
}

// ---------------------------------------------------------------------------
// SetSummarizeFunc
// ---------------------------------------------------------------------------

func TestService_SetSummarizeFunc_SetsCallback(t *testing.T) {
	svc := NewService(nil, nil, nil, nil)
	require.Nil(t, svc.onSummarize)

	called := false
	svc.SetSummarizeFunc(func(_ context.Context, _, _ string) {
		called = true
	})
	require.NotNil(t, svc.onSummarize)

	svc.onSummarize(context.Background(), "u", "n")
	require.True(t, called)
}

// ---------------------------------------------------------------------------
// CaptureVoice -- error paths reachable without real dependencies
// ---------------------------------------------------------------------------

func TestService_CaptureVoice_NilTranscriber_ReturnsError(t *testing.T) {
	svc := NewService(nil, nil, nil, nil) // transcriber is nil
	_, err := svc.CaptureVoice(context.Background(), "user1", strings.NewReader("audio"), "test.wav")
	require.Error(t, err)
	require.Contains(t, err.Error(), "voice transcription not configured")
}

// ---------------------------------------------------------------------------
// generateTitle
// ---------------------------------------------------------------------------

func TestGenerateTitle_ShortFirstLine(t *testing.T) {
	title := generateTitle("Hello world")
	require.Equal(t, "Hello world", title)
}

func TestGenerateTitle_UsesFirstNonEmptyLine(t *testing.T) {
	title := generateTitle("\n\n  Good morning  \nSecond line")
	require.Equal(t, "Good morning", title)
}

func TestGenerateTitle_TruncatesLongLine_AtWordBoundary(t *testing.T) {
	// Build a line >60 chars with spaces after position 20.
	// "The quick brown fox jumps over the lazy dog and keeps on running forever more"
	long := "The quick brown fox jumps over the lazy dog and keeps on running forever more"
	require.Greater(t, len(long), 60)

	title := generateTitle(long)
	require.True(t, strings.HasSuffix(title, "..."), "should end with ellipsis")
	// The title (without "...") should be at most 60 chars.
	withoutEllipsis := strings.TrimSuffix(title, "...")
	require.LessOrEqual(t, len(withoutEllipsis), 60)
}

func TestGenerateTitle_TruncatesLongLine_NoWordBoundaryAfter20(t *testing.T) {
	// A single 70-char "word" with no spaces after position 20.
	long := strings.Repeat("a", 70)
	title := generateTitle(long)
	// lastSpace returns -1 (no space at all), so idx <= 20 means the
	// truncation falls back to the first 60 chars + "..."
	require.True(t, strings.HasSuffix(title, "..."))
	withoutEllipsis := strings.TrimSuffix(title, "...")
	require.Equal(t, 60, len(withoutEllipsis))
}

func TestGenerateTitle_AllBlankLines_FallsBackToVoiceNote(t *testing.T) {
	title := generateTitle("\n  \n\t\n")
	require.Contains(t, title, "Voice Note")
}

func TestGenerateTitle_EmptyText_FallsBackToVoiceNote(t *testing.T) {
	title := generateTitle("")
	require.Contains(t, title, "Voice Note")
}

// ---------------------------------------------------------------------------
// splitLines
// ---------------------------------------------------------------------------

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"no newline", "abc", []string{"abc"}},
		{"single newline", "a\nb", []string{"a", "b"}},
		{"trailing newline", "a\n", []string{"a"}},
		{"multiple newlines", "a\nb\nc", []string{"a", "b", "c"}},
		{"blank lines", "a\n\nb", []string{"a", "", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitLines(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// trimLine
// ---------------------------------------------------------------------------

func TestTrimLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no whitespace", "hello", "hello"},
		{"leading spaces", "   hello", "hello"},
		{"trailing spaces", "hello   ", "hello"},
		{"leading and trailing", "  hello  ", "hello"},
		{"tabs", "\thello\t", "hello"},
		{"carriage return", "\r hello \r", "hello"},
		{"only whitespace", "   \t\r  ", ""},
		{"empty string", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := trimLine(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// lastSpace
// ---------------------------------------------------------------------------

func TestLastSpace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"no spaces", "abc", -1},
		{"single space at end", "ab ", 2},
		{"space in middle", "a b c", 3},
		{"multiple spaces returns last", "a b c d", 5},
		{"all spaces", "   ", 2},
		{"empty string", "", -1},
		{"space at start only", " abc", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lastSpace(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}
