package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AuthData holds persisted authentication tokens.
type AuthData struct {
	ServerURL    string `json:"server_url"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Username     string `json:"username"`
}

// authConfigDir returns the directory used for storing auth data.
func authConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "seam"), nil
}

// authConfigPath returns the full path to the auth JSON file.
func authConfigPath() (string, error) {
	dir, err := authConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

// LoadAuth loads saved authentication data from disk.
// Returns nil with no error if the file does not exist.
func LoadAuth() (*AuthData, error) {
	p, err := authConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read auth file: %w", err)
	}

	var auth AuthData
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("parse auth file: %w", err)
	}
	return &auth, nil
}

// SaveAuth writes authentication data to disk.
func SaveAuth(auth *AuthData) error {
	dir, err := authConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth data: %w", err)
	}

	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("write auth file: %w", err)
	}
	return nil
}
