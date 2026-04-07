// Package config loads and validates server configuration from a YAML file
// with environment variable overrides and sensible defaults.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the complete server configuration.
type Config struct {
	Listen        string          `yaml:"listen"`
	DataDir       string          `yaml:"data_dir"`
	JWTSecret     string          `yaml:"jwt_secret"`
	OllamaBaseURL string          `yaml:"ollama_base_url"`
	ChromaDBURL   string          `yaml:"chromadb_url"`
	LogLevel      string          `yaml:"log_level"`    // "debug", "info", "warn", "error"; default "info"
	CORSOrigins   []string        `yaml:"cors_origins"` // allowed CORS origins; default localhost
	Models        ModelsConfig    `yaml:"models"`
	LLM           LLMConfig       `yaml:"llm"`
	Whisper       WhisperConfig   `yaml:"whisper"`
	Auth          AuthConfig      `yaml:"auth"`
	AI            AIConfig        `yaml:"ai"`
	Assistant     AssistantConfig `yaml:"assistant"`
	UserDB        UserDBConfig    `yaml:"userdb"`
	Watcher       WatcherConfig   `yaml:"watcher"`
	WebDistDir    string          `yaml:"web_dist_dir"` // path to web/dist for SPA serving; empty uses default
}

// AssistantConfig specifies agentic assistant parameters.
type AssistantConfig struct {
	// MaxIterations is the maximum number of tool-use loop iterations.
	// Default: 10.
	MaxIterations int `yaml:"max_iterations"`

	// ConfirmationRequired lists tool names that require user approval.
	// Default: ["create_note", "update_note", "create_project"]
	ConfirmationRequired []string `yaml:"confirmation_required"`

	// Model overrides the chat model for the assistant.
	// When empty, defaults to models.chat.
	Model string `yaml:"model"`
}

// ModelsConfig specifies the AI model names.
type ModelsConfig struct {
	Embeddings string `yaml:"embeddings"`
	Background string `yaml:"background"`
	Chat       string `yaml:"chat"`
}

// LLMConfig specifies which LLM provider to use for chat completions.
// Embeddings always use the local Ollama instance regardless of this setting.
type LLMConfig struct {
	// Provider selects the LLM backend: "ollama" (default), "openai", or "anthropic".
	Provider string `yaml:"provider"`

	// OpenAI settings (used when provider is "openai").
	OpenAI OpenAIConfig `yaml:"openai"`

	// Anthropic settings (used when provider is "anthropic").
	Anthropic AnthropicConfig `yaml:"anthropic"`
}

// OpenAIConfig holds OpenAI API settings.
type OpenAIConfig struct {
	// APIKey is the OpenAI API key. env: SEAM_OPENAI_API_KEY
	APIKey string `yaml:"api_key"`

	// BaseURL overrides the default API endpoint. Useful for Azure OpenAI
	// or OpenAI-compatible services (e.g., Together, Groq).
	// Defaults to "https://api.openai.com/v1" when empty.
	BaseURL string `yaml:"base_url"`
}

// AnthropicConfig holds Anthropic API settings.
type AnthropicConfig struct {
	// APIKey is the Anthropic API key. env: SEAM_ANTHROPIC_API_KEY
	APIKey string `yaml:"api_key"`

	// MaxTokens is the maximum number of output tokens per request.
	// Anthropic requires this field. Defaults to 4096 when zero.
	MaxTokens int `yaml:"max_tokens"`
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
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("config.Load: read file: %w", err)
		}
		// YAML file does not exist; proceed with defaults + env overrides.
		slog.Info("config file not found, using defaults and env overrides", "path", path)
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("config.Load: parse yaml: %w", err)
		}
		// Warn if YAML file contains API keys and has overly permissive permissions.
		warnIfInsecureConfigFile(path, cfg)
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
	if v := os.Getenv("SEAM_LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("SEAM_OPENAI_API_KEY"); v != "" {
		cfg.LLM.OpenAI.APIKey = v
	}
	if v := os.Getenv("SEAM_OPENAI_BASE_URL"); v != "" {
		cfg.LLM.OpenAI.BaseURL = v
	}
	if v := os.Getenv("SEAM_ANTHROPIC_API_KEY"); v != "" {
		cfg.LLM.Anthropic.APIKey = v
	}
	if v := os.Getenv("SEAM_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("SEAM_CORS_ORIGINS"); v != "" {
		cfg.CORSOrigins = strings.Split(v, ",")
	}
}

// applyDefaults sets default values for fields that are empty/zero.
func applyDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	// No default for OllamaBaseURL: when empty, AI features are disabled
	// and model names are not required.
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "ollama"
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
		cfg.AI.EmbeddingTimeout.Duration = 60 * time.Second
	}
	if cfg.AI.ChatTimeout.Duration == 0 {
		cfg.AI.ChatTimeout.Duration = 5 * time.Minute
	}
	if cfg.UserDB.EvictionTimeout.Duration == 0 {
		cfg.UserDB.EvictionTimeout.Duration = 30 * time.Minute
	}
	if cfg.Watcher.DebounceInterval.Duration == 0 {
		cfg.Watcher.DebounceInterval.Duration = 200 * time.Millisecond
	}
	if cfg.Assistant.MaxIterations <= 0 {
		cfg.Assistant.MaxIterations = 10
	}
	if len(cfg.Assistant.ConfirmationRequired) == 0 {
		cfg.Assistant.ConfirmationRequired = []string{"create_note", "update_note", "create_project"}
	}
	// C-34: Apply default Whisper binary path if model path is set but binary is not.
	if cfg.Whisper.ModelPath != "" && cfg.Whisper.BinaryPath == "" {
		cfg.Whisper.BinaryPath = "whisper-cli"
	}
	if cfg.WebDistDir == "" && cfg.DataDir != "" {
		cfg.WebDistDir = filepath.Join(filepath.Dir(cfg.DataDir), "web", "dist")
	}
	// Warn if the computed WebDistDir does not exist.
	if cfg.WebDistDir != "" {
		if _, err := os.Stat(cfg.WebDistDir); os.IsNotExist(err) {
			slog.Warn("web dist directory does not exist, SPA will not be served",
				"path", cfg.WebDistDir)
		}
	}
}

// normalizePaths strips trailing slashes from paths and URLs, and resolves
// DataDir to an absolute path so that relative paths like "./data" work
// consistently regardless of working directory.
func normalizePaths(cfg *Config) {
	if cfg.DataDir != "" {
		if abs, err := filepath.Abs(cfg.DataDir); err == nil {
			cfg.DataDir = abs
		}
	}
	cfg.DataDir = strings.TrimRight(cfg.DataDir, "/")
	cfg.OllamaBaseURL = strings.TrimRight(cfg.OllamaBaseURL, "/")
	cfg.ChromaDBURL = strings.TrimRight(cfg.ChromaDBURL, "/")
	cfg.LLM.OpenAI.BaseURL = strings.TrimRight(cfg.LLM.OpenAI.BaseURL, "/")
}

// warnIfInsecureConfigFile logs a warning if the YAML config file contains
// API keys and has world-readable permissions (mode includes o+r).
func warnIfInsecureConfigFile(path string, cfg *Config) {
	hasSecrets := cfg.LLM.OpenAI.APIKey != "" || cfg.LLM.Anthropic.APIKey != ""
	if !hasSecrets {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	mode := info.Mode().Perm()
	// Warn if group-readable or world-readable.
	if mode&0o044 != 0 {
		slog.Warn("config file contains API keys but has permissive file permissions; "+
			"consider restricting to owner-only (chmod 600) or using environment variables instead",
			"path", path, "mode", fmt.Sprintf("%04o", mode))
	}
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
	} else if len(cfg.JWTSecret) < 32 {
		errs = append(errs, errors.New("jwt_secret must be at least 32 characters"))
	}
	// Validate AI model names. Embeddings require Ollama; chat/background
	// require either Ollama or an external provider.
	if cfg.OllamaBaseURL != "" {
		if cfg.Models.Embeddings == "" {
			errs = append(errs, errors.New("models.embeddings is required when ollama_base_url is set"))
		}
	}
	// Chat and background models are needed whenever any LLM provider is active.
	hasLLMProvider := cfg.OllamaBaseURL != "" || cfg.LLM.Provider != "ollama"
	if hasLLMProvider {
		if cfg.Models.Chat == "" {
			errs = append(errs, errors.New("models.chat is required when an LLM provider is configured"))
		}
		if cfg.Models.Background == "" {
			errs = append(errs, errors.New("models.background is required when an LLM provider is configured"))
		}
	}
	// Validate LLM provider and required credentials.
	switch cfg.LLM.Provider {
	case "ollama":
		// No extra config needed; uses OllamaBaseURL.
	case "openai":
		if cfg.LLM.OpenAI.APIKey == "" {
			errs = append(errs, errors.New("llm.openai.api_key is required when llm.provider is \"openai\" (set in config or SEAM_OPENAI_API_KEY env var)"))
		}
	case "anthropic":
		if cfg.LLM.Anthropic.APIKey == "" {
			errs = append(errs, errors.New("llm.anthropic.api_key is required when llm.provider is \"anthropic\" (set in config or SEAM_ANTHROPIC_API_KEY env var)"))
		}
	default:
		errs = append(errs, fmt.Errorf("llm.provider must be \"ollama\", \"openai\", or \"anthropic\" (got %q)", cfg.LLM.Provider))
	}
	// External providers still need Ollama for embeddings.
	if cfg.LLM.Provider != "ollama" && cfg.OllamaBaseURL == "" {
		slog.Warn("ollama_base_url not configured; embeddings will not be available even with an external LLM provider")
	}

	if cfg.Auth.BcryptCost < 4 || cfg.Auth.BcryptCost > 14 {
		errs = append(errs, fmt.Errorf("auth.bcrypt_cost must be between 4 and 14 (got %d)", cfg.Auth.BcryptCost))
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
