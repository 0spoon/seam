package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seam: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	serverURL := flag.String("server", "http://localhost:8080", "Seam server URL")
	themeFlag := flag.String("theme", "", "TUI theme (catppuccin-mocha|catppuccin-macchiato|catppuccin-frappe|catppuccin-latte|seam)")
	assistantThemeFlag := flag.String("assistant-theme", "", "Assistant screen theme (mario|follow_global)")
	flag.Parse()

	// Resolve theme preference (flag > env > file > default) and apply
	// it before constructing any models so the initial render uses the
	// chosen palette. Apply errors are non-fatal: ResolveTUIConfig has
	// already filtered unknown values, so any failure here is a logic
	// bug and we keep running with whatever the package init set.
	tuiCfg := ResolveTUIConfig(*themeFlag, *assistantThemeFlag)
	if err := ApplyTheme(tuiCfg.Theme); err != nil {
		fmt.Fprintf(os.Stderr, "seam: apply theme: %v\n", err)
	}
	if err := ApplyAssistantTheme(tuiCfg.AssistantTheme); err != nil {
		fmt.Fprintf(os.Stderr, "seam: apply assistant theme: %v\n", err)
	}

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
	// v2: alt-screen, mouse, and keyboard enhancements are declared on the
	// View struct instead of via tea.NewProgram options. See appModel.View.
	p := tea.NewProgram(model)

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
