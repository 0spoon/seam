# Getting Started

You'll have Seam running in under five minutes. The only hard requirement is Go -- everything else is optional and adds capabilities incrementally.

## Prerequisites

| Requirement | Version | Required? | What it unlocks |
| --- | --- | --- | --- |
| [Go](https://go.dev) | 1.25+ | Yes | Build the server and TUI |
| [Node.js](https://nodejs.org) | 22+ | For web frontend | React web app |
| [Ollama](https://ollama.com) | Latest | For AI features | Chat, search, writing assist, the assistant |
| [Docker](https://www.docker.com) | Latest | For ChromaDB (recommended) | Seam manages the container for you |
| [ChromaDB](https://www.trychroma.com) | Latest | Only if self-hosted | Semantic search, Ask Seam, auto-linking |

Seam degrades gracefully. Without Ollama, you get a solid markdown note system with full-text search. Add Ollama for AI. Add ChromaDB for semantic search. Switch to OpenAI or Anthropic when you want cloud-grade models -- one config line.

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

## ChromaDB

ChromaDB is the vector store that powers semantic search, Ask Seam, auto-link suggestions, and synthesis. It is optional -- skip it and you still get full-text search and writing assist. When you run `make init`, it asks how you want to handle ChromaDB:

| Choice | When to pick it |
| --- | --- |
| `docker` (default) | You have Docker installed and want Seam to manage the container for you |
| `external` | You already run ChromaDB somewhere -- locally via brew, on another host, etc. |
| `disable` | You do not want semantic search |

If you pick `docker`, `make init` writes `docker/.env` (which records your `SEAM_DATA_DIR` so the bind mount lines up with seamd's data directory) and offers to start the container immediately. The container is defined in `docker/chroma-compose.yml` and runs `chromadb/chroma` with persistence under `${SEAM_DATA_DIR}/chromadb`.

### Managing the container

The Makefile has thin wrappers around `docker compose` for the chroma container:

```bash
make chroma-up           # start (or recreate) the ChromaDB container
make chroma-down         # stop and remove the container
make chroma-logs         # follow container logs
make chroma-status       # show container status
```

These all act on `docker/chroma-compose.yml` and read `docker/.env` automatically.

### Optional supervisor service

If you install seamd as a system service (`make install-service`), the installer asks whether to also install a sibling supervisor for the ChromaDB container. The supervisor:

- Runs `scripts/chroma-supervisor.sh` under launchd (macOS) or systemd --user (Linux)
- On startup, probes `docker info`. If Docker is not running, it launches Docker Desktop on macOS (`open -ga Docker`) or starts the daemon on Linux (`systemctl --user start docker.service`, falling back to `sudo systemctl start docker` if available)
- Polls for up to 60 seconds until the daemon is ready, then runs `docker compose up` in the foreground
- Restarts on failure -- if you `docker compose down` manually, the service brings the container back after a short throttle interval
- Survives reboots and login cycles, so semantic search "just works" after a restart

Skip the supervisor if you manage Chroma yourself or run it on a different host. You can always run `make chroma-up` manually instead.

### What seamd does if Chroma is unreachable

seamd does not require ChromaDB to start. On startup, it does a 2-second heartbeat probe against `chromadb_url`. If Chroma is unreachable, it logs a single loud warning telling you to run `make chroma-up` (or install the supervisor). The AI task queue continues running normally and embeddings will succeed once Chroma comes up.

## Running

```bash
# Everything at once (recommended for development)
make dev                                          # runs seamd + Vite + Chroma in parallel (Ctrl-C to stop)

# Or run components separately
make run                                          # seamd only, on :8080
make dev-web                                      # Vite dev server only, on :5173
./bin/seam --server http://localhost:8080          # TUI client (separate terminal)
```

## Configuration

`seam-server.yaml` with environment variable overrides. The full reference with comments is in [`seam-server.yaml.example`](../seam-server.yaml.example); this is the abridged version.

```yaml
listen: ":8080" # SEAM_LISTEN
data_dir: "./data" # SEAM_DATA_DIR
jwt_secret: "" # SEAM_JWT_SECRET (required, min 32 chars)
ollama_base_url: "http://localhost:11434" # SEAM_OLLAMA_URL
chromadb_url: "http://localhost:8000" # SEAM_CHROMADB_URL (blank = disable semantic search)

# AI model names. models.embeddings is interpreted by the embeddings provider
# below; models.chat / models.background go through the llm provider.
models:
  embeddings: "qwen3-embedding:8b"
  background: "qwen3:32b"
  chat: "qwen3:32b"

# LLM provider for chat completions, writing assist, synthesis, the agentic
# assistant, and tag/project suggestions. Independent of the embeddings
# provider below.
llm:
  provider: "ollama" # SEAM_LLM_PROVIDER: "ollama", "openai", "anthropic"
  openai:
    api_key: "" # SEAM_OPENAI_API_KEY
    base_url: "" # SEAM_OPENAI_BASE_URL (Azure, Together, Groq, ...)
  anthropic:
    api_key: "" # SEAM_ANTHROPIC_API_KEY
    max_tokens: 4096

# Embedding provider. Independent of llm.provider: a setup with Anthropic
# chat may legitimately want OpenAI or Ollama embeddings (Anthropic ships no
# embedding model). Switching the provider or embedding model invalidates
# the existing Chroma collection -- run `make reindex` after a swap.
embeddings:
  provider: "ollama" # SEAM_EMBEDDINGS_PROVIDER: "ollama", "openai"
  openai:
    api_key: "" # SEAM_EMBEDDINGS_OPENAI_API_KEY (falls back to llm.openai.api_key)
    base_url: "" # SEAM_EMBEDDINGS_OPENAI_BASE_URL
    dimensions: 0 # 0 = native; only for text-embedding-3-* models

# Whisper.cpp for voice transcription (optional)
whisper:
  model_path: "" # path to ggml model file
  binary_path: "whisper-cli"

auth:
  access_token_ttl: "15m"
  refresh_token_ttl: "168h" # 7 days
  bcrypt_cost: 12

ai:
  queue_workers: 1
  embedding_timeout: "60s"
  chat_timeout: "5m"

watcher:
  debounce_interval: "200ms"

# Cron-based proactive jobs. The default daily briefing is auto-provisioned
# on first start; manage it through /api/schedules afterwards.
scheduler:
  enabled: true
  tick_interval: "1m"
  daily_briefing:
    enabled: true
    cron_expr: "0 8 * * *" # 08:00 daily, server-local time
    project_slug: "briefings"
    lookback_hours: 24
```

## Graceful Degradation

No Ollama URL? AI features are disabled, you get a solid markdown note system. No ChromaDB? No semantic search, FTS still works. No Whisper model? No voice capture. Seam does not crash because you did not install everything.

## TUI Keyboard Shortcuts

| Key            | Action                                                              |
| -------------- | ------------------------------------------------------------------- |
| `n`            | New note (opens template picker)                                    |
| `u`            | Capture from URL                                                    |
| `v`            | Capture from voice                                                  |
| `a`            | Ask Seam (AI chat)                                                  |
| `t`            | Timeline view                                                       |
| `/`            | Search (prefix `?` for semantic)                                    |
| `Alt+S` / `F2` | Save note (Ctrl+S is intercepted by tmux and terminal flow control) |
| `Ctrl+A`       | AI writing assist (in editor)                                       |
| `Esc`          | Back / close                                                        |
