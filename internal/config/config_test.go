package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "seam-server.yaml")
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SEAM_LISTEN", "SEAM_DATA_DIR", "SEAM_JWT_SECRET",
		"SEAM_OLLAMA_URL", "SEAM_CHROMADB_URL",
		"SEAM_LLM_PROVIDER", "SEAM_OPENAI_API_KEY",
		"SEAM_OPENAI_BASE_URL", "SEAM_ANTHROPIC_API_KEY",
	} {
		t.Setenv(key, "")
	}
}

const validConfig = `
listen: ":9090"
data_dir: "/var/seam"
jwt_secret: "test-secret-key-1234567890abcdef"
ollama_base_url: "http://localhost:11434"
chromadb_url: "http://localhost:8000"
models:
  embeddings: "qwen3-embedding:8b"
  background: "qwen3:32b"
  chat: "qwen3:32b"
whisper:
  model_path: "/path/to/ggml-base.en.bin"
  binary_path: "/usr/local/bin/whisper-cli"
auth:
  access_token_ttl: "30m"
  refresh_token_ttl: "48h"
  bcrypt_cost: 10
ai:
  queue_workers: 2
  embedding_timeout: "60s"
  chat_timeout: "180s"
userdb:
  eviction_timeout: "1h"
watcher:
  debounce_interval: "500ms"
`

func TestLoad_ValidConfig(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, validConfig)

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, ":9090", cfg.Listen)
	require.Equal(t, "/var/seam", cfg.DataDir)
	require.Equal(t, "test-secret-key-1234567890abcdef", cfg.JWTSecret)
	require.Equal(t, "http://localhost:11434", cfg.OllamaBaseURL)
	require.Equal(t, "http://localhost:8000", cfg.ChromaDBURL)

	require.Equal(t, "qwen3-embedding:8b", cfg.Models.Embeddings)
	require.Equal(t, "qwen3:32b", cfg.Models.Background)
	require.Equal(t, "qwen3:32b", cfg.Models.Chat)

	require.Equal(t, "/path/to/ggml-base.en.bin", cfg.Whisper.ModelPath)
	require.Equal(t, "/usr/local/bin/whisper-cli", cfg.Whisper.BinaryPath)

	require.Equal(t, 30*time.Minute, cfg.Auth.AccessTokenTTL.Duration)
	require.Equal(t, 48*time.Hour, cfg.Auth.RefreshTokenTTL.Duration)
	require.Equal(t, 10, cfg.Auth.BcryptCost)

	require.Equal(t, 2, cfg.AI.QueueWorkers)
	require.Equal(t, 60*time.Second, cfg.AI.EmbeddingTimeout.Duration)
	require.Equal(t, 180*time.Second, cfg.AI.ChatTimeout.Duration)

	require.Equal(t, 1*time.Hour, cfg.UserDB.EvictionTimeout.Duration)
	require.Equal(t, 500*time.Millisecond, cfg.Watcher.DebounceInterval.Duration)
}

func TestLoad_DefaultsApplied(t *testing.T) {
	clearEnv(t)
	// Minimal config with only required fields.
	minimalConfig := `
data_dir: "/var/seam"
jwt_secret: "test-secret-key-that-is-32-chars!"
models:
  embeddings: "embed-model"
  background: "bg-model"
  chat: "chat-model"
`
	path := writeConfig(t, minimalConfig)

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, ":8080", cfg.Listen)
	require.Empty(t, cfg.OllamaBaseURL)
	require.Equal(t, 15*time.Minute, cfg.Auth.AccessTokenTTL.Duration)
	require.Equal(t, 168*time.Hour, cfg.Auth.RefreshTokenTTL.Duration)
	require.Equal(t, 12, cfg.Auth.BcryptCost)
	require.Equal(t, 1, cfg.AI.QueueWorkers)
	require.Equal(t, 60*time.Second, cfg.AI.EmbeddingTimeout.Duration)
	require.Equal(t, 5*time.Minute, cfg.AI.ChatTimeout.Duration)
	require.Equal(t, 30*time.Minute, cfg.UserDB.EvictionTimeout.Duration)
	require.Equal(t, 200*time.Millisecond, cfg.Watcher.DebounceInterval.Duration)
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	clearEnv(t)

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "missing data_dir",
			config:  `jwt_secret: "s"` + "\n" + `models: {embeddings: "e", background: "b", chat: "c"}`,
			wantErr: "data_dir is required",
		},
		{
			name:    "missing jwt_secret",
			config:  `data_dir: "/d"` + "\n" + `models: {embeddings: "e", background: "b", chat: "c"}`,
			wantErr: "jwt_secret is required",
		},
		{
			name:    "missing models.embeddings",
			config:  `data_dir: "/d"` + "\n" + `jwt_secret: "test-secret-key-that-is-32-chars!"` + "\n" + `ollama_base_url: "http://localhost:11434"` + "\n" + `models: {background: "b", chat: "c"}`,
			wantErr: "models.embeddings is required",
		},
		{
			name:    "missing models.background",
			config:  `data_dir: "/d"` + "\n" + `jwt_secret: "test-secret-key-that-is-32-chars!"` + "\n" + `ollama_base_url: "http://localhost:11434"` + "\n" + `models: {embeddings: "e", chat: "c"}`,
			wantErr: "models.background is required",
		},
		{
			name:    "missing models.chat",
			config:  `data_dir: "/d"` + "\n" + `jwt_secret: "test-secret-key-that-is-32-chars!"` + "\n" + `ollama_base_url: "http://localhost:11434"` + "\n" + `models: {embeddings: "e", background: "b"}`,
			wantErr: "models.chat is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.config)
			_, err := Load(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, validConfig)

	t.Setenv("SEAM_LISTEN", ":3000")
	t.Setenv("SEAM_DATA_DIR", "/tmp/seam-data")
	t.Setenv("SEAM_JWT_SECRET", "env-secret-that-is-at-least-32-c")
	t.Setenv("SEAM_OLLAMA_URL", "http://gpu-host:11434")
	t.Setenv("SEAM_CHROMADB_URL", "http://chroma-host:8000")

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, ":3000", cfg.Listen)
	require.Equal(t, "/tmp/seam-data", cfg.DataDir)
	require.Equal(t, "env-secret-that-is-at-least-32-c", cfg.JWTSecret)
	require.Equal(t, "http://gpu-host:11434", cfg.OllamaBaseURL)
	require.Equal(t, "http://chroma-host:8000", cfg.ChromaDBURL)
}

func TestLoad_EnvOverridePrecedence(t *testing.T) {
	clearEnv(t)
	// YAML sets jwt_secret to one value, env sets to another.
	// env should win.
	minConfig := `
data_dir: "/d"
jwt_secret: "yaml-secret-must-be-32-chars-long"
models: {embeddings: "e", background: "b", chat: "c"}
`
	path := writeConfig(t, minConfig)
	t.Setenv("SEAM_JWT_SECRET", "env-secret-that-is-at-least-32-c")

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "env-secret-that-is-at-least-32-c", cfg.JWTSecret)
}

func TestLoad_EmptyEnvDoesNotOverride(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, validConfig)

	// Set env vars to empty strings -- they should NOT override YAML values.
	t.Setenv("SEAM_LISTEN", "")
	t.Setenv("SEAM_DATA_DIR", "")

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, ":9090", cfg.Listen)
	require.Equal(t, "/var/seam", cfg.DataDir)
}

func TestLoad_PathNormalization(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/var/seam/"
jwt_secret: "test-secret-key-that-is-32-chars!"
ollama_base_url: "http://localhost:11434/"
chromadb_url: "http://localhost:8000/"
models: {embeddings: "e", background: "b", chat: "c"}
`
	path := writeConfig(t, config)

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, "/var/seam", cfg.DataDir)
	require.Equal(t, "http://localhost:11434", cfg.OllamaBaseURL)
	require.Equal(t, "http://localhost:8000", cfg.ChromaDBURL)
}

func TestLoad_FileNotFound(t *testing.T) {
	clearEnv(t)
	_, err := Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "validation failed")
}

func TestLoad_InvalidYAML(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, "invalid: yaml: [")
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse yaml")
}

func TestLoad_InvalidDuration(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "s"
models: {embeddings: "e", background: "b", chat: "c"}
auth:
  access_token_ttl: "not-a-duration"
`
	path := writeConfig(t, config)
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoad_LLMDefaultsToOllama(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
`
	path := writeConfig(t, config)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "ollama", cfg.LLM.Provider)
}

func TestLoad_LLMOpenAI(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
ollama_base_url: "http://localhost:11434"
models:
  embeddings: "qwen3-embedding:8b"
  background: "gpt-4o"
  chat: "gpt-4o"
llm:
  provider: "openai"
  openai:
    api_key: "sk-test-key"
    base_url: "https://api.openai.com/v1/"
`
	path := writeConfig(t, config)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "openai", cfg.LLM.Provider)
	require.Equal(t, "sk-test-key", cfg.LLM.OpenAI.APIKey)
	// Trailing slash should be normalized.
	require.Equal(t, "https://api.openai.com/v1", cfg.LLM.OpenAI.BaseURL)
}

func TestLoad_LLMOpenAI_MissingKey(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
llm:
  provider: "openai"
`
	path := writeConfig(t, config)
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "llm.openai.api_key is required")
}

func TestLoad_LLMAnthropic(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
ollama_base_url: "http://localhost:11434"
models:
  embeddings: "qwen3-embedding:8b"
  background: "claude-sonnet-4-20250514"
  chat: "claude-sonnet-4-20250514"
llm:
  provider: "anthropic"
  anthropic:
    api_key: "sk-ant-test"
`
	path := writeConfig(t, config)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "anthropic", cfg.LLM.Provider)
	require.Equal(t, "sk-ant-test", cfg.LLM.Anthropic.APIKey)
}

func TestLoad_LLMAnthropic_MissingKey(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
llm:
  provider: "anthropic"
`
	path := writeConfig(t, config)
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "llm.anthropic.api_key is required")
}

func TestLoad_LLMInvalidProvider(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
llm:
  provider: "gemini"
`
	path := writeConfig(t, config)
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "llm.provider must be")
}

func TestLoad_LLMEnvOverrides(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
ollama_base_url: "http://localhost:11434"
models:
  embeddings: "qwen3-embedding:8b"
  background: "gpt-4o"
  chat: "gpt-4o"
`
	path := writeConfig(t, config)

	t.Setenv("SEAM_LLM_PROVIDER", "openai")
	t.Setenv("SEAM_OPENAI_API_KEY", "sk-from-env")
	t.Setenv("SEAM_OPENAI_BASE_URL", "https://custom.endpoint.com/v1")

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "openai", cfg.LLM.Provider)
	require.Equal(t, "sk-from-env", cfg.LLM.OpenAI.APIKey)
	require.Equal(t, "https://custom.endpoint.com/v1", cfg.LLM.OpenAI.BaseURL)
}

func TestLoad_LLMAnthropicEnvOverride(t *testing.T) {
	clearEnv(t)
	config := `
data_dir: "/d"
jwt_secret: "test-secret-key-that-is-32-chars!"
models:
  chat: "claude-sonnet-4-20250514"
  background: "claude-sonnet-4-20250514"
`
	path := writeConfig(t, config)

	t.Setenv("SEAM_LLM_PROVIDER", "anthropic")
	t.Setenv("SEAM_ANTHROPIC_API_KEY", "sk-ant-from-env")

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "anthropic", cfg.LLM.Provider)
	require.Equal(t, "sk-ant-from-env", cfg.LLM.Anthropic.APIKey)
}
