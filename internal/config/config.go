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
	Listen        string           `yaml:"listen"`
	DataDir       string           `yaml:"data_dir"`
	JWTSecret     string           `yaml:"jwt_secret"`
	OllamaBaseURL string           `yaml:"ollama_base_url"`
	ChromaDBURL   string           `yaml:"chromadb_url"`
	LogLevel      string           `yaml:"log_level"`    // "debug", "info", "warn", "error"; default "info"
	CORSOrigins   []string         `yaml:"cors_origins"` // allowed CORS origins; default localhost
	Models        ModelsConfig     `yaml:"models"`
	LLM           LLMConfig        `yaml:"llm"`
	Embeddings    EmbeddingsConfig `yaml:"embeddings"`
	Whisper       WhisperConfig    `yaml:"whisper"`
	Auth          AuthConfig       `yaml:"auth"`
	AI            AIConfig         `yaml:"ai"`
	Assistant     AssistantConfig  `yaml:"assistant"`
	UserDB        UserDBConfig     `yaml:"userdb"`
	Watcher       WatcherConfig    `yaml:"watcher"`
	Scheduler     SchedulerConfig  `yaml:"scheduler"`
	MCP           MCPConfig        `yaml:"mcp"`
	Usage         UsageConfig      `yaml:"usage"`
	WebDistDir    string           `yaml:"web_dist_dir"` // path to web/dist for SPA serving; empty uses default
}

// MCPConfig holds MCP server settings.
type MCPConfig struct {
	// APIKey is an optional static bearer token for MCP access.
	// When set, clients can authenticate with "Authorization: Bearer <api_key>"
	// instead of a short-lived JWT. Useful for AI coding agents (Claude Code,
	// Cursor, etc.) that need long-lived access. Generate with:
	//   openssl rand -hex 32
	// env: SEAM_MCP_API_KEY
	APIKey string `yaml:"api_key"`
}

// SchedulerConfig controls the cron-based scheduler that runs proactive
// background jobs (daily briefings, automations, reminders).
type SchedulerConfig struct {
	// Enabled toggles the scheduler at startup. Default: true.
	Enabled *bool `yaml:"enabled"`

	// TickInterval controls how often the scheduler polls for due jobs.
	// Default: 1 minute. Smaller values are useful in tests; values
	// below 1 second are coerced back to 1 second.
	TickInterval Duration `yaml:"tick_interval"`

	// DailyBriefing controls the auto-provisioned daily briefing
	// schedule. When Enabled is true and no schedule with the same name
	// exists yet, the server creates one on startup.
	DailyBriefing DailyBriefingConfig `yaml:"daily_briefing"`
}

// DailyBriefingConfig configures the auto-provisioned daily briefing job.
type DailyBriefingConfig struct {
	// Enabled controls whether the scheduler auto-creates the default
	// daily briefing schedule on first startup. Default: true.
	Enabled *bool `yaml:"enabled"`

	// CronExpr is the cron expression that drives the briefing.
	// Default: "0 8 * * *" (08:00 every day, server-local time).
	CronExpr string `yaml:"cron_expr"`

	// ProjectSlug is the project where briefing notes are filed.
	// Default: "briefings".
	ProjectSlug string `yaml:"project_slug"`

	// LookbackHours bounds the "recent activity" window. Default: 24.
	LookbackHours int `yaml:"lookback_hours"`
}

// AssistantConfig specifies agentic assistant parameters.
type AssistantConfig struct {
	// MaxIterations is the maximum number of tool-use loop iterations.
	// Default: 10.
	MaxIterations int `yaml:"max_iterations"`

	// ConfirmationRequired lists tool names that require user approval.
	// Default: every persistent-state-mutating assistant tool
	// (create_note, update_note, append_to_note, create_project,
	// save_memory, update_profile). Anything that can affect future
	// system prompts -- in particular update_profile and save_memory --
	// MUST be in this list to defend against persistent prompt injection:
	// an attacker-controlled note can otherwise pivot the assistant into
	// writing malicious "instructions" into the user profile or memory,
	// which then get injected verbatim into every subsequent system
	// prompt. See docs/security.md > "Assistant safety".
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

// EmbeddingsConfig selects the embedding backend, independent of llm.provider.
// A user with Anthropic chat may legitimately want OpenAI or Ollama embeddings
// (Anthropic ships no embedding model). Defaults to "ollama".
type EmbeddingsConfig struct {
	// Provider selects the embedding backend: "ollama" (default) or "openai".
	Provider string `yaml:"provider"`

	// OpenAI settings (used when provider is "openai"). When APIKey or BaseURL
	// are empty, they fall back to llm.openai.api_key / llm.openai.base_url so
	// users with a single OpenAI key only need to specify it once.
	OpenAI EmbeddingsOpenAIConfig `yaml:"openai"`
}

// EmbeddingsOpenAIConfig holds OpenAI embedding API settings.
type EmbeddingsOpenAIConfig struct {
	// APIKey is the OpenAI API key. env: SEAM_EMBEDDINGS_OPENAI_API_KEY
	// Falls back to llm.openai.api_key when empty.
	APIKey string `yaml:"api_key"`

	// BaseURL overrides the default API endpoint. Useful for Azure OpenAI
	// or OpenAI-compatible services. env: SEAM_EMBEDDINGS_OPENAI_BASE_URL
	// Falls back to llm.openai.base_url, then to https://api.openai.com/v1.
	BaseURL string `yaml:"base_url"`

	// Dimensions optionally truncates the embedding vector. Zero means
	// "use the model's native dimension" and omits the field from the
	// request. text-embedding-3-large defaults to 3072 dims; setting this
	// to 1024 trades a small amount of quality for 3x lower storage.
	Dimensions int `yaml:"dimensions"`
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

// UsageConfig specifies token usage tracking parameters.
type UsageConfig struct {
	// TrackingEnabled toggles token usage recording. Default: true.
	TrackingEnabled *bool `yaml:"tracking_enabled"`
	// TrackLocal toggles tracking of local Ollama calls. Default: true.
	TrackLocal *bool `yaml:"track_local"`
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
	if v := os.Getenv("SEAM_EMBEDDINGS_PROVIDER"); v != "" {
		cfg.Embeddings.Provider = v
	}
	if v := os.Getenv("SEAM_EMBEDDINGS_OPENAI_API_KEY"); v != "" {
		cfg.Embeddings.OpenAI.APIKey = v
	}
	if v := os.Getenv("SEAM_EMBEDDINGS_OPENAI_BASE_URL"); v != "" {
		cfg.Embeddings.OpenAI.BaseURL = v
	}
	if v := os.Getenv("SEAM_MCP_API_KEY"); v != "" {
		cfg.MCP.APIKey = v
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
	if cfg.Embeddings.Provider == "" {
		cfg.Embeddings.Provider = "ollama"
	}
	// API key fallback: when running OpenAI on both chat and embeddings,
	// the user only needs to set the key in one place.
	if cfg.Embeddings.Provider == "openai" {
		if cfg.Embeddings.OpenAI.APIKey == "" {
			cfg.Embeddings.OpenAI.APIKey = cfg.LLM.OpenAI.APIKey
		}
		if cfg.Embeddings.OpenAI.BaseURL == "" {
			cfg.Embeddings.OpenAI.BaseURL = cfg.LLM.OpenAI.BaseURL
		}
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
	if cfg.Scheduler.Enabled == nil {
		t := true
		cfg.Scheduler.Enabled = &t
	}
	if cfg.Scheduler.TickInterval.Duration == 0 {
		cfg.Scheduler.TickInterval.Duration = time.Minute
	}
	if cfg.Scheduler.TickInterval.Duration < time.Second {
		cfg.Scheduler.TickInterval.Duration = time.Second
	}
	if cfg.Scheduler.DailyBriefing.Enabled == nil {
		t := true
		cfg.Scheduler.DailyBriefing.Enabled = &t
	}
	if strings.TrimSpace(cfg.Scheduler.DailyBriefing.CronExpr) == "" {
		cfg.Scheduler.DailyBriefing.CronExpr = "0 8 * * *"
	}
	if strings.TrimSpace(cfg.Scheduler.DailyBriefing.ProjectSlug) == "" {
		cfg.Scheduler.DailyBriefing.ProjectSlug = "briefings"
	}
	if cfg.Scheduler.DailyBriefing.LookbackHours <= 0 {
		cfg.Scheduler.DailyBriefing.LookbackHours = 24
	}
	if cfg.Usage.TrackingEnabled == nil {
		t := true
		cfg.Usage.TrackingEnabled = &t
	}
	if cfg.Usage.TrackLocal == nil {
		t := true
		cfg.Usage.TrackLocal = &t
	}
	if cfg.Assistant.MaxIterations <= 0 {
		cfg.Assistant.MaxIterations = 10
	}
	if len(cfg.Assistant.ConfirmationRequired) == 0 {
		// H-5: Every assistant tool that mutates persistent state must
		// require user confirmation, otherwise a single prompt-injected
		// note can pivot to persistent control of the assistant via
		// update_profile (which writes Instructions into every future
		// system prompt) or save_memory (which is loaded into the
		// system prompt by loadContext). append_to_note is also a
		// silent mutation surface and is gated for the same reason.
		cfg.Assistant.ConfirmationRequired = []string{
			"create_note",
			"update_note",
			"append_to_note",
			"create_project",
			"save_memory",
			"update_profile",
		}
	}
	// C-34: Apply default Whisper binary path if model path is set but binary is not.
	if cfg.Whisper.ModelPath != "" && cfg.Whisper.BinaryPath == "" {
		cfg.Whisper.BinaryPath = "whisper-cli"
	}
	if cfg.WebDistDir == "" {
		cfg.WebDistDir = defaultWebDistDir()
	}
	// Warn if the configured WebDistDir does not exist. Empty is fine
	// (autodetect failed) -- the server simply won't mount the SPA.
	if cfg.WebDistDir != "" {
		if _, err := os.Stat(cfg.WebDistDir); os.IsNotExist(err) {
			slog.Warn("web dist directory does not exist, SPA will not be served",
				"path", cfg.WebDistDir)
		}
	}
}

// defaultWebDistDir tries to locate the built React SPA without relying on
// the user's data_dir, which may live anywhere on disk. The candidates,
// in priority order, are:
//
//  1. <dir of running binary>/web/dist          -- bundled deploys
//  2. <parent of bin dir>/web/dist              -- repo layout: bin/seamd + web/dist siblings
//  3. <cwd>/web/dist                            -- `go run ./cmd/seamd` from the repo root
//
// The first candidate that resolves to an existing directory wins. When
// nothing matches, the function returns "" so the server skips the SPA
// handler entirely (the API still works).
func defaultWebDistDir() string {
	var candidates []string

	if exe, err := os.Executable(); err == nil {
		// Resolve symlinks so a launchd plist pointing at bin/seamd
		// still walks up to the real repo, not the symlink farm.
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "web", "dist"),
			filepath.Join(filepath.Dir(exeDir), "web", "dist"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "web", "dist"))
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
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
	cfg.Embeddings.OpenAI.BaseURL = strings.TrimRight(cfg.Embeddings.OpenAI.BaseURL, "/")
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

	// Catch obvious model/provider mismatches when the user is hitting the
	// canonical OpenAI/Anthropic endpoint. Skip the OpenAI check when
	// base_url is set, because OpenAI-compatible APIs (Together, Groq,
	// Azure, Anyscale) accept arbitrary model names like
	// "meta-llama/Llama-3.1-70B-Instruct".
	if cfg.LLM.Provider == "openai" && cfg.LLM.OpenAI.BaseURL == "" {
		if err := validateModelNameForProvider("openai", cfg.Models.Chat, "models.chat"); err != nil {
			errs = append(errs, err)
		}
		if err := validateModelNameForProvider("openai", cfg.Models.Background, "models.background"); err != nil {
			errs = append(errs, err)
		}
	}
	if cfg.LLM.Provider == "anthropic" {
		// Anthropic has no base_url override in this config.
		if err := validateModelNameForProvider("anthropic", cfg.Models.Chat, "models.chat"); err != nil {
			errs = append(errs, err)
		}
		if err := validateModelNameForProvider("anthropic", cfg.Models.Background, "models.background"); err != nil {
			errs = append(errs, err)
		}
	}

	// Validate the embedding provider. The check is only meaningful when
	// Chroma is configured -- without Chroma the embedder is never invoked,
	// so we leave the operator alone.
	if cfg.ChromaDBURL != "" {
		switch cfg.Embeddings.Provider {
		case "ollama":
			if cfg.OllamaBaseURL == "" {
				errs = append(errs, errors.New("embeddings.provider=\"ollama\" requires ollama_base_url to be set"))
			}
			if cfg.Models.Embeddings == "" {
				errs = append(errs, errors.New("models.embeddings is required when embeddings.provider=\"ollama\""))
			}
		case "openai":
			if cfg.Embeddings.OpenAI.APIKey == "" {
				errs = append(errs, errors.New("embeddings.openai.api_key is required when embeddings.provider=\"openai\" (set in config, or SEAM_EMBEDDINGS_OPENAI_API_KEY env var, or fall back to llm.openai.api_key)"))
			}
			if cfg.Models.Embeddings == "" {
				errs = append(errs, errors.New("models.embeddings is required when embeddings.provider=\"openai\" (e.g. \"text-embedding-3-large\")"))
			}
		default:
			errs = append(errs, fmt.Errorf("embeddings.provider must be \"ollama\" or \"openai\" (got %q)", cfg.Embeddings.Provider))
		}
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

// validateModelNameForProvider returns an error if model is not a plausible
// model name for the given provider's canonical endpoint. The check is
// intentionally permissive (prefix-based) rather than a closed allowlist,
// because new model versions ship faster than we can update Seam.
//
// An empty model is not flagged here -- the missing-value error is reported
// elsewhere by the model-name-required checks.
func validateModelNameForProvider(provider, model, fieldName string) error {
	if model == "" {
		return nil
	}
	m := strings.ToLower(model)
	var ok bool
	switch provider {
	case "openai":
		// gpt-*, chatgpt-*, o1/o2/o3/.../o9/...
		ok = strings.HasPrefix(m, "gpt-") ||
			strings.HasPrefix(m, "chatgpt-") ||
			(len(m) >= 2 && m[0] == 'o' && m[1] >= '1' && m[1] <= '9')
	case "anthropic":
		ok = strings.HasPrefix(m, "claude-")
	}
	if !ok {
		return fmt.Errorf("%s=%q does not look like a %s model name; if you are using an OpenAI-compatible API set llm.openai.base_url, otherwise pick a real %s model", fieldName, model, provider, provider)
	}
	return nil
}
