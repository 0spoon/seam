// Package config loads and validates server configuration from a YAML file
// with environment variable overrides and sensible defaults.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the complete server configuration.
type Config struct {
	Listen        string        `yaml:"listen"`
	DataDir       string        `yaml:"data_dir"`
	JWTSecret     string        `yaml:"jwt_secret"`
	OllamaBaseURL string        `yaml:"ollama_base_url"`
	ChromaDBURL   string        `yaml:"chromadb_url"`
	Models        ModelsConfig  `yaml:"models"`
	Whisper       WhisperConfig `yaml:"whisper"`
	Auth          AuthConfig    `yaml:"auth"`
	AI            AIConfig      `yaml:"ai"`
	UserDB        UserDBConfig  `yaml:"userdb"`
	Watcher       WatcherConfig `yaml:"watcher"`
}

// ModelsConfig specifies the AI model names.
type ModelsConfig struct {
	Embeddings string `yaml:"embeddings"`
	Background string `yaml:"background"`
	Chat       string `yaml:"chat"`
}

// WhisperConfig specifies local whisper.cpp transcription settings.
type WhisperConfig struct {
	// ModelPath is the path to the ggml model file (e.g. ggml-base.en.bin).
	ModelPath string `yaml:"model_path"`
	// BinaryPath is the path to the whisper-cli executable.
	// Defaults to "whisper-cli" (looked up on PATH).
	BinaryPath string `yaml:"binary_path"`
}

// AuthConfig specifies authentication parameters.
type AuthConfig struct {
	AccessTokenTTL  Duration `yaml:"access_token_ttl"`
	RefreshTokenTTL Duration `yaml:"refresh_token_ttl"`
	BcryptCost      int      `yaml:"bcrypt_cost"`
}

// AIConfig specifies AI task queue parameters.
type AIConfig struct {
	QueueWorkers     int      `yaml:"queue_workers"`
	EmbeddingTimeout Duration `yaml:"embedding_timeout"`
	ChatTimeout      Duration `yaml:"chat_timeout"`
}

// UserDBConfig specifies per-user database manager parameters.
type UserDBConfig struct {
	EvictionTimeout Duration `yaml:"eviction_timeout"`
}

// WatcherConfig specifies file watcher parameters.
type WatcherConfig struct {
	DebounceInterval Duration `yaml:"debounce_interval"`
}

// Duration wraps time.Duration with YAML string unmarshaling support.
// Accepts Go duration strings like "15m", "168h", "200ms".
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// Load reads configuration from the given YAML file path, applies environment
// variable overrides, fills in defaults, and validates required fields.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config.Load: read file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config.Load: parse yaml: %w", err)
	}

	applyEnvOverrides(cfg)
	applyDefaults(cfg)
	normalizePaths(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}

	return cfg, nil
}

// applyEnvOverrides overrides config values from environment variables.
// Only non-empty env values take effect.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SEAM_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("SEAM_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("SEAM_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("SEAM_OLLAMA_URL"); v != "" {
		cfg.OllamaBaseURL = v
	}
	if v := os.Getenv("SEAM_CHROMADB_URL"); v != "" {
		cfg.ChromaDBURL = v
	}
}

// applyDefaults sets default values for fields that are empty/zero.
func applyDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.OllamaBaseURL == "" {
		cfg.OllamaBaseURL = "http://localhost:11434"
	}
	if cfg.Auth.AccessTokenTTL.Duration == 0 {
		cfg.Auth.AccessTokenTTL.Duration = 15 * time.Minute
	}
	if cfg.Auth.RefreshTokenTTL.Duration == 0 {
		cfg.Auth.RefreshTokenTTL.Duration = 168 * time.Hour
	}
	if cfg.Auth.BcryptCost == 0 {
		cfg.Auth.BcryptCost = 12
	}
	if cfg.AI.QueueWorkers == 0 {
		cfg.AI.QueueWorkers = 1
	}
	if cfg.AI.EmbeddingTimeout.Duration == 0 {
		cfg.AI.EmbeddingTimeout.Duration = 30 * time.Second
	}
	if cfg.AI.ChatTimeout.Duration == 0 {
		cfg.AI.ChatTimeout.Duration = 120 * time.Second
	}
	if cfg.UserDB.EvictionTimeout.Duration == 0 {
		cfg.UserDB.EvictionTimeout.Duration = 30 * time.Minute
	}
	if cfg.Watcher.DebounceInterval.Duration == 0 {
		cfg.Watcher.DebounceInterval.Duration = 200 * time.Millisecond
	}
}

// normalizePaths strips trailing slashes from paths and URLs.
func normalizePaths(cfg *Config) {
	cfg.DataDir = strings.TrimRight(cfg.DataDir, "/")
	cfg.OllamaBaseURL = strings.TrimRight(cfg.OllamaBaseURL, "/")
	cfg.ChromaDBURL = strings.TrimRight(cfg.ChromaDBURL, "/")
}

// validate checks that all required fields are present and warns about
// optional fields that will be needed for later phases.
func validate(cfg *Config) error {
	var errs []error

	if cfg.Listen == "" {
		errs = append(errs, errors.New("listen is required"))
	}
	if cfg.DataDir == "" {
		errs = append(errs, errors.New("data_dir is required"))
	}
	if cfg.JWTSecret == "" {
		errs = append(errs, errors.New("jwt_secret is required (set in config or SEAM_JWT_SECRET env var)"))
	}
	if cfg.OllamaBaseURL == "" {
		errs = append(errs, errors.New("ollama_base_url is required"))
	}
	if cfg.Models.Embeddings == "" {
		errs = append(errs, errors.New("models.embeddings is required"))
	}
	if cfg.Models.Background == "" {
		errs = append(errs, errors.New("models.background is required"))
	}
	if cfg.Models.Chat == "" {
		errs = append(errs, errors.New("models.chat is required"))
	}

	// Warn about optional fields needed for later phases.
	if cfg.ChromaDBURL == "" {
		slog.Warn("chromadb_url not configured; semantic search (Phase 2) will not be available")
	}
	if cfg.Whisper.ModelPath == "" {
		slog.Warn("whisper.model_path not configured; voice capture will not be available")
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %w", errors.Join(errs...))
	}
	return nil
}
