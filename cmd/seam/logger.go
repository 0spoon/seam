package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
)

// logger is the TUI's file-backed structured logger. It starts as a
// no-op so code paths that run before initLogger (or on a machine
// where the log file can't be created) don't have to guard against
// nil. initLogger replaces it with a real handler on success.
var logger = slog.New(slog.NewTextHandler(io.Discard, nil))

// initLogger opens the TUI log file and swaps the package logger to
// point at it. Returns a closer to defer from main.
//
// The TUI can't log to stdout/stderr at runtime because Bubble Tea
// owns the alternate screen and anything written there gets clobbered
// by renders. So we log to a file and surface that file through
// `make logs` alongside seamd's own logs.
//
// Path layout matches where `scripts/service.sh logs` tails from:
//
//	macOS: $HOME/Library/Logs/seam/seam-tui.log
//	Linux: ${XDG_STATE_HOME:-$HOME/.local/state}/seam/seam-tui.log
//
// Any failure along the way (no HOME, unwritable dir, etc.) leaves
// the no-op logger in place -- the TUI should never refuse to start
// just because it can't log.
func initLogger() func() {
	path, err := tuiLogPath()
	if err != nil {
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return func() {}
	}
	logger = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("seam TUI started", "pid", os.Getpid())
	return func() {
		logger.Info("seam TUI exiting", "pid", os.Getpid())
		_ = f.Close()
	}
}

// tuiLogPath returns the absolute path to the TUI log file for the
// current OS. Callers should not assume the parent directory exists.
func tuiLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs", "seam", "seam-tui.log"), nil
	default:
		// $XDG_STATE_HOME is the XDG Base Directory spec location for
		// "state data that should persist between application runs but
		// is not important or portable enough to be in config or
		// data" -- logs fit that bill exactly.
		state := os.Getenv("XDG_STATE_HOME")
		if state == "" {
			state = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(state, "seam", "seam-tui.log"), nil
	}
}

// logError prints a human-readable error line to stderr (so the user
// sees it in their terminal immediately) AND writes a structured entry
// to the log file (so `make logs` can surface it later). Use this for
// errors that happen outside the Bubble Tea render loop -- pre-start,
// post-exit, and anything that bubbles up through run().
func logError(msg string, err error) {
	fmt.Fprintf(os.Stderr, "seam: %s: %v\n", msg, err)
	logger.Error(msg, "error", err)
}

// recoverAndLog is deferred from main() so a panic inside the TUI
// (a programmer bug, a nil-pointer in a model.Update, etc.) leaves a
// stack trace behind in the log file instead of vanishing into the
// alternate-screen void. After logging, we re-raise with os.Exit(2)
// so the process still crashes visibly; returning from the deferred
// function would swallow the panic.
func recoverAndLog() {
	r := recover()
	if r == nil {
		return
	}
	stack := string(debug.Stack())
	logger.Error("TUI panic", "value", fmt.Sprintf("%v", r), "stack", stack)
	fmt.Fprintf(os.Stderr, "seam crashed: %v\n\n%s\n", r, stack)
	os.Exit(2)
}
