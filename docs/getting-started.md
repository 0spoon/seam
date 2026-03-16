# Getting Started

## Prerequisites

| Requirement | Version | Required? |
|---|---|---|
| [Go](https://go.dev) | 1.25+ | Yes |
| [Node.js](https://nodejs.org) | 22+ | For web frontend |
| [Ollama](https://ollama.com) | Latest | For AI features |
| [ChromaDB](https://www.trychroma.com) | Latest | For semantic search |

Seam works without AI -- you get a solid markdown note system with full-text search. Add Ollama when you want AI features. Add ChromaDB for semantic search. Add OpenAI or Anthropic when your GPU starts crying.

## Installation

```bash
git clone https://github.com/katata/seam.git
cd seam
make build          # builds bin/seamd (server) + bin/seam (TUI)
```

## Initial Setup

```bash
make init           # interactive setup: generates JWT secret, asks for data dir, LLM provider
```

If using Ollama, pull the default models:

```bash
ollama pull qwen3:32b
ollama pull qwen3-embedding:8b
```

## Running

```bash
# Start the server
make run                                          # builds and starts seamd on :8080

# TUI client (separate terminal)
./bin/seam --server http://localhost:8080

# Web frontend (separate terminal)
cd web && npm install && npm run dev              # Vite dev server on :5173, proxies /api to :8080
```

## Configuration

`seam-server.yaml` with environment variable overrides:

```yaml
listen: ":8080"                      # SEAM_LISTEN
data_dir: "./data"                   # SEAM_DATA_DIR
jwt_secret: ""                       # SEAM_JWT_SECRET (required, min 32 chars)
ollama_base_url: "http://localhost:11434"  # SEAM_OLLAMA_URL
chromadb_url: "http://localhost:8000"      # SEAM_CHROMADB_URL

# AI model names (embeddings always use local Ollama)
models:
  embeddings: "qwen3-embedding:8b"
  background: "qwen3:32b"
  chat: "qwen3:32b"

# LLM provider for chat completions
# Embeddings stay local regardless of this setting
llm:
  provider: "ollama"               # SEAM_LLM_PROVIDER: "ollama", "openai", "anthropic"
  openai:
    api_key: ""                    # SEAM_OPENAI_API_KEY
    base_url: ""                   # SEAM_OPENAI_BASE_URL (for Azure, Together, Groq, etc.)
  anthropic:
    api_key: ""                    # SEAM_ANTHROPIC_API_KEY

# Whisper.cpp for voice transcription (optional)
whisper:
  model_path: ""                   # path to ggml model file
  binary_path: "whisper-cli"

auth:
  access_token_ttl: "15m"
  refresh_token_ttl: "168h"
  bcrypt_cost: 12

ai:
  queue_workers: 1
  embedding_timeout: "60s"
  chat_timeout: "5m"

userdb:
  eviction_timeout: "30m"          # close idle user DBs

watcher:
  debounce_interval: "200ms"
```

## Graceful Degradation

No Ollama URL? AI features are disabled, you get a solid markdown note system. No ChromaDB? No semantic search, FTS still works. No Whisper model? No voice capture. Seam does not crash because you did not install everything.

## TUI Keyboard Shortcuts

| Key | Action |
|---|---|
| `n` | New note (opens template picker) |
| `u` | Capture from URL |
| `v` | Capture from voice |
| `a` | Ask Seam (AI chat) |
| `t` | Timeline view |
| `/` | Search (prefix `?` for semantic) |
| `Ctrl+S` | Save note |
| `Ctrl+A` | AI writing assist (in editor) |
| `Esc` | Back / close |
