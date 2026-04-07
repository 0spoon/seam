#!/usr/bin/env bash
#
# Interactive setup for seam-server.yaml.
# Generates a JWT secret, prompts for data directory and LLM provider,
# and writes a ready-to-use config file.

set -euo pipefail

CONFIG="seam-server.yaml"
EXAMPLE="seam-server.yaml.example"

# -- helpers ------------------------------------------------------------------

info()  { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
ok()    { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
warn()  { printf "\033[1;33m==>\033[0m %s\n" "$1"; }
ask()   { printf "\033[1;37m  > \033[0m%s " "$1"; }

# -- preflight ----------------------------------------------------------------

if [ ! -f "$EXAMPLE" ]; then
    warn "Cannot find $EXAMPLE. Run this from the repository root."
    exit 1
fi

if [ -f "$CONFIG" ]; then
    ask "$CONFIG already exists. Overwrite? [y/N]"
    read -r overwrite
    if [[ ! "$overwrite" =~ ^[Yy]$ ]]; then
        info "Keeping existing config. Nothing to do."
        exit 0
    fi
    echo
fi

info "Setting up Seam"
echo

# -- JWT secret ---------------------------------------------------------------

info "Generating JWT secret..."
JWT_SECRET=$(openssl rand -hex 32)
ok "JWT secret generated (64 hex chars)"
echo

# -- data directory -----------------------------------------------------------

DEFAULT_DATA_DIR="./data"
ask "Data directory (notes, databases, templates) [$DEFAULT_DATA_DIR]:"
read -r DATA_DIR
DATA_DIR="${DATA_DIR:-$DEFAULT_DATA_DIR}"

# Expand ~ if the user typed it
DATA_DIR="${DATA_DIR/#\~/$HOME}"

echo

# -- LLM provider -------------------------------------------------------------

info "LLM provider for chat completions (embeddings always run on local Ollama)"
echo "  1) ollama    -- fully local, no API costs"
echo "  2) openai    -- GPT-4o, or any OpenAI-compatible API"
echo "  3) anthropic -- Claude"
echo
ask "Provider [1]:"
read -r PROVIDER_CHOICE
echo

case "${PROVIDER_CHOICE:-1}" in
    1|ollama)    LLM_PROVIDER="ollama" ;;
    2|openai)    LLM_PROVIDER="openai" ;;
    3|anthropic) LLM_PROVIDER="anthropic" ;;
    *)
        warn "Unknown choice '$PROVIDER_CHOICE', defaulting to ollama"
        LLM_PROVIDER="ollama"
        ;;
esac

OPENAI_API_KEY=""
OPENAI_BASE_URL=""
ANTHROPIC_API_KEY=""

if [ "$LLM_PROVIDER" = "openai" ]; then
    ask "OpenAI API key (or set SEAM_OPENAI_API_KEY env var later):"
    read -r OPENAI_API_KEY
    echo

    ask "Custom base URL? (leave blank for api.openai.com, or enter for Azure/Together/Groq):"
    read -r OPENAI_BASE_URL
    echo
elif [ "$LLM_PROVIDER" = "anthropic" ]; then
    ask "Anthropic API key (or set SEAM_ANTHROPIC_API_KEY env var later):"
    read -r ANTHROPIC_API_KEY
    echo
fi

# -- Ollama URL ---------------------------------------------------------------

DEFAULT_OLLAMA_URL="http://localhost:11434"
ask "Ollama URL [$DEFAULT_OLLAMA_URL]:"
read -r OLLAMA_URL
OLLAMA_URL="${OLLAMA_URL:-$DEFAULT_OLLAMA_URL}"
echo

# -- ChromaDB -----------------------------------------------------------------

info "ChromaDB (vector store for semantic search)"
echo "  1) docker    -- Seam manages a ChromaDB container via docker/chroma-compose.yml (recommended)"
echo "  2) external  -- I already have ChromaDB running somewhere"
echo "  3) disable   -- no semantic search"
echo
ask "Choice [1]:"
read -r CHROMA_CHOICE
echo

CHROMA_URL=""
CHROMA_DOCKER="no"
case "${CHROMA_CHOICE:-1}" in
    1|docker)
        CHROMA_URL="http://localhost:8000"
        CHROMA_DOCKER="yes"
        ;;
    2|external)
        DEFAULT_CHROMA_URL="http://localhost:8000"
        ask "ChromaDB URL [$DEFAULT_CHROMA_URL]:"
        read -r CHROMA_URL
        CHROMA_URL="${CHROMA_URL:-$DEFAULT_CHROMA_URL}"
        echo
        ;;
    3|disable)
        warn "Semantic search disabled. You can re-enable by editing chromadb_url in $CONFIG."
        echo
        ;;
    *)
        warn "Unknown choice '$CHROMA_CHOICE', defaulting to docker"
        CHROMA_URL="http://localhost:8000"
        CHROMA_DOCKER="yes"
        echo
        ;;
esac

# -- models -------------------------------------------------------------------
#
# Chat and background models go through the LLM provider chosen above.
# Embeddings always run on local Ollama -- Seam does not currently wire any
# cloud embedding backend (OpenAI text-embedding-3-* / Voyage), so the
# embedding model name must be a model that Ollama can pull.

info "Model selection"

case "$LLM_PROVIDER" in
    openai)
        DEFAULT_CHAT_MODEL="gpt-5.4"
        DEFAULT_BG_MODEL="gpt-5.4-mini"
        ;;
    anthropic)
        DEFAULT_CHAT_MODEL="claude-sonnet-4-6"
        DEFAULT_BG_MODEL="claude-haiku-4-5-20251001"
        ;;
    *)  # ollama
        DEFAULT_CHAT_MODEL="qwen3:32b"
        DEFAULT_BG_MODEL="qwen3:32b"
        ;;
esac

echo "  Chat model is used for the assistant, writing assist, chat, and suggestions."
ask "Chat model [$DEFAULT_CHAT_MODEL]:"
read -r CHAT_MODEL
CHAT_MODEL="${CHAT_MODEL:-$DEFAULT_CHAT_MODEL}"
echo

echo "  Background model is used for auto-linking and other lighter background tasks."
ask "Background model [$DEFAULT_BG_MODEL]:"
read -r BG_MODEL
BG_MODEL="${BG_MODEL:-$DEFAULT_BG_MODEL}"
echo

# Embeddings live in their own provider knob, independent of the chat
# provider. Only prompt when Chroma is enabled, since without Chroma the
# embedding model is never invoked. Defaults to Ollama -- cloud embeddings
# are strictly opt-in.
EMBED_PROVIDER="ollama"
DEFAULT_EMBED_MODEL="qwen3-embedding:8b"
EMBED_MODEL="$DEFAULT_EMBED_MODEL"
EMBED_OPENAI_KEY=""
EMBED_OPENAI_BASE_URL=""
EMBED_OPENAI_DIMS="0"

if [ -n "$CHROMA_URL" ]; then
    info "Embedding provider (independent of chat provider above)"
    echo "  1) ollama -- fully local, no API costs (recommended)"
    echo "  2) openai -- text-embedding-3-large / -small via the OpenAI API"
    echo
    ask "Provider [1]:"
    read -r EMBED_PROVIDER_CHOICE
    echo

    case "${EMBED_PROVIDER_CHOICE:-1}" in
        1|ollama)
            EMBED_PROVIDER="ollama"
            DEFAULT_EMBED_MODEL="qwen3-embedding:8b"
            ;;
        2|openai)
            EMBED_PROVIDER="openai"
            DEFAULT_EMBED_MODEL="text-embedding-3-large"
            ;;
        *)
            warn "Unknown choice '$EMBED_PROVIDER_CHOICE', defaulting to ollama"
            EMBED_PROVIDER="ollama"
            ;;
    esac

    ask "Embedding model [$DEFAULT_EMBED_MODEL]:"
    read -r EMBED_MODEL
    EMBED_MODEL="${EMBED_MODEL:-$DEFAULT_EMBED_MODEL}"
    echo

    if [ "$EMBED_PROVIDER" = "openai" ]; then
        # Reuse the chat-side OpenAI key when the user already entered one,
        # otherwise prompt separately. The seamd config block also has a
        # fallback for this case, but asking once is friendlier than
        # surprising the user later.
        if [ -n "$OPENAI_API_KEY" ]; then
            EMBED_OPENAI_KEY="$OPENAI_API_KEY"
            ok "Reusing the OpenAI API key from the chat-provider step"
        else
            ask "OpenAI API key for embeddings (or set SEAM_EMBEDDINGS_OPENAI_API_KEY env var later):"
            read -r EMBED_OPENAI_KEY
            echo
        fi
        if [ -n "$OPENAI_BASE_URL" ]; then
            EMBED_OPENAI_BASE_URL="$OPENAI_BASE_URL"
        fi

        echo "  Embedding dimensions (only text-embedding-3-* models support truncation)."
        echo "  text-embedding-3-large is 3072 by default; 1024 trades a small amount of quality"
        echo "  for 3x lower vector storage. Press enter for native size."
        ask "Dimensions [native]:"
        read -r EMBED_OPENAI_DIMS_INPUT
        EMBED_OPENAI_DIMS="${EMBED_OPENAI_DIMS_INPUT:-0}"
        echo
    fi
fi

# -- listen address -----------------------------------------------------------

DEFAULT_LISTEN=":8080"
ask "Listen address [$DEFAULT_LISTEN]:"
read -r LISTEN
LISTEN="${LISTEN:-$DEFAULT_LISTEN}"
echo

# -- write config -------------------------------------------------------------

cat > "$CONFIG" << EOF
# Seam server configuration
# Generated by: make init

listen: "${LISTEN}"
data_dir: "${DATA_DIR}"
jwt_secret: "${JWT_SECRET}"
ollama_base_url: "${OLLAMA_URL}"
chromadb_url: "${CHROMA_URL}"

models:
  embeddings: "${EMBED_MODEL}"
  background: "${BG_MODEL}"
  chat: "${CHAT_MODEL}"

llm:
  provider: "${LLM_PROVIDER}"
  openai:
    api_key: "${OPENAI_API_KEY}"
    base_url: "${OPENAI_BASE_URL}"
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"

embeddings:
  provider: "${EMBED_PROVIDER}"
  openai:
    api_key: "${EMBED_OPENAI_KEY}"
    base_url: "${EMBED_OPENAI_BASE_URL}"
    dimensions: ${EMBED_OPENAI_DIMS}

whisper:
  model_path: ""
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
  eviction_timeout: "30m"

watcher:
  debounce_interval: "200ms"
EOF

ok "Wrote $CONFIG"
echo

# -- docker compose env for chroma -------------------------------------------
#
# If the user picked the Docker-managed Chroma, drop a docker/.env next to
# the compose file so that `make chroma-up` bind-mounts the same data
# directory that seamd is configured to use. We check for docker but do not
# fail if it's missing -- user can install it later and still run
# `make chroma-up`.

CHROMA_STARTED="no"
if [ "$CHROMA_DOCKER" = "yes" ]; then
    DOCKER_ENV="docker/.env"
    info "Writing $DOCKER_ENV for docker compose"
    mkdir -p docker
    cat > "$DOCKER_ENV" << EOF
# Generated by: make init
# Read by docker/chroma-compose.yml. Not checked in.
SEAM_DATA_DIR=${DATA_DIR}
EOF
    ok "Wrote $DOCKER_ENV (SEAM_DATA_DIR=${DATA_DIR})"
    echo

    if command -v docker >/dev/null 2>&1; then
        if docker info >/dev/null 2>&1; then
            ask "Start ChromaDB now via docker compose? [Y/n]"
            read -r START_NOW
            if [[ -z "$START_NOW" || "$START_NOW" =~ ^[Yy]$ ]]; then
                info "Starting ChromaDB container..."
                if (cd docker && docker compose -f chroma-compose.yml up -d); then
                    ok "ChromaDB container running on ${CHROMA_URL}"
                    CHROMA_STARTED="yes"
                else
                    warn "docker compose up failed. You can retry with 'make chroma-up'."
                fi
                echo
            fi
        else
            warn "Docker is installed but the daemon is not reachable."
            warn "Start Docker Desktop (or dockerd) and then run 'make chroma-up'."
            echo
        fi
    else
        warn "Docker not found on PATH."
        warn "Install Docker Desktop (or docker engine) and then run 'make chroma-up'."
        echo
    fi
fi

# -- summary ------------------------------------------------------------------

if [ "$CHROMA_DOCKER" = "yes" ]; then
    CHROMA_SUMMARY="${CHROMA_URL} (docker-managed)"
elif [ -n "$CHROMA_URL" ]; then
    CHROMA_SUMMARY="${CHROMA_URL}"
else
    CHROMA_SUMMARY="disabled"
fi

info "Configuration summary:"
echo "  Listen:        ${LISTEN}"
echo "  Data dir:      ${DATA_DIR}"
echo "  LLM provider:  ${LLM_PROVIDER}"
echo "  Ollama:        ${OLLAMA_URL}"
echo "  ChromaDB:      ${CHROMA_SUMMARY}"
echo "  Chat model:    ${CHAT_MODEL}"
echo "  Bg model:      ${BG_MODEL}"
if [ -n "$CHROMA_URL" ]; then
    echo "  Embed provider: ${EMBED_PROVIDER}"
    echo "  Embed model:   ${EMBED_MODEL}"
    if [ "$EMBED_PROVIDER" = "openai" ] && [ "$EMBED_OPENAI_DIMS" != "0" ]; then
        echo "  Embed dims:    ${EMBED_OPENAI_DIMS}"
    fi
fi
echo

# -- next steps ---------------------------------------------------------------

info "Next steps:"
echo "  make build              # build server + TUI"
echo "  make run                # start the server"
if [ "$CHROMA_DOCKER" = "yes" ] && [ "$CHROMA_STARTED" != "yes" ]; then
    echo "  make chroma-up          # start the ChromaDB container"
fi
echo

# Tell the user which Ollama models they need to pull. Cloud chat users
# only need the embedding model locally if their embedding provider is
# also Ollama.
NEEDS_OLLAMA_PULL="no"
if [ "$LLM_PROVIDER" = "ollama" ]; then NEEDS_OLLAMA_PULL="yes"; fi
if [ -n "$CHROMA_URL" ] && [ "$EMBED_PROVIDER" = "ollama" ]; then NEEDS_OLLAMA_PULL="yes"; fi

if [ "$NEEDS_OLLAMA_PULL" = "yes" ]; then
    info "Pull the local Ollama models you'll need:"
    if [ "$LLM_PROVIDER" = "ollama" ]; then
        echo "  ollama pull ${CHAT_MODEL}"
        if [ "$BG_MODEL" != "$CHAT_MODEL" ]; then
            echo "  ollama pull ${BG_MODEL}"
        fi
    fi
    if [ -n "$CHROMA_URL" ] && [ "$EMBED_PROVIDER" = "ollama" ]; then
        echo "  ollama pull ${EMBED_MODEL}"
    fi
    echo
fi

# Reindex hint: only relevant when Chroma is enabled. After switching
# embedding model or provider the operator must run this to repopulate
# the new collection.
if [ -n "$CHROMA_URL" ]; then
    info "If you ever switch embedding models or providers, run:"
    echo "  make reindex            # re-embed every note into the new collection"
    echo
fi

if [ -n "$OPENAI_API_KEY" ] || [ -n "$ANTHROPIC_API_KEY" ] || [ -n "$EMBED_OPENAI_KEY" ]; then
    warn "API key written to $CONFIG. Make sure this file is in .gitignore."
fi
