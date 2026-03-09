package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seam: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	serverURL := flag.String("server", "http://localhost:8080", "Seam server URL")
	flag.Parse()

	client := NewAPIClient(*serverURL)

	// Try loading saved auth.
	authenticated := false
	username := ""
	auth, err := LoadAuth()
	if err == nil && auth != nil && auth.AccessToken != "" {
		client.AccessToken = auth.AccessToken
		client.RefreshToken = auth.RefreshToken
		username = auth.Username

		// Use the saved server URL if the flag was not explicitly set.
		if auth.ServerURL != "" && !isFlagSet("server") {
			client.BaseURL = auth.ServerURL
		}

		// Try to refresh the token to verify it is still valid.
		// Use a timeout to avoid blocking the terminal on a slow or
		// unreachable server.
		refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 5*time.Second)
		tokens, refreshErr := client.RefreshCtx(refreshCtx)
		refreshCancel()
		if refreshErr == nil {
			client.AccessToken = tokens.AccessToken
			client.RefreshToken = tokens.RefreshToken
			authenticated = true

			// Update saved tokens.
			auth.AccessToken = tokens.AccessToken
			auth.RefreshToken = tokens.RefreshToken
			_ = SaveAuth(auth)
		}
		// If refresh fails or times out, fall through to login screen.
	}

	model := newAppModel(client, authenticated, username)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}

	return nil
}

// isFlagSet returns true if the named flag was explicitly provided on the
// command line.
func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
