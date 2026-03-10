<p align="center">
  <br/>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTIwIiBoZWlnaHQ9IjEyMCIgdmlld0JveD0iMCAwIDEyMCAxMjAiIGZpbGw9Im5vbmUiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyI+CjxjaXJjbGUgY3g9IjYwIiBjeT0iNjAiIHI9IjU2IiBzdHJva2U9IiNjNDkxNWMiIHN0cm9rZS13aWR0aD0iMiIgZmlsbD0ibm9uZSIvPgo8cGF0aCBkPSJNMzAgNDVDNDAgMzUgNjAgMzAgNzUgNDBDOTAgNTAgODAgNzAgNjUgNzVDNTAgODAgMzUgNzAgNDUgNTVDNTUgNDAgNzUgNDUgNzAgNjAiIHN0cm9rZT0iI2M0OTE1YyIgc3Ryb2tlLXdpZHRoPSIyLjUiIGZpbGw9Im5vbmUiIHN0cm9rZS1saW5lY2FwPSJyb3VuZCIvPgo8Y2lyY2xlIGN4PSIzMCIgY3k9IjQ1IiByPSI0IiBmaWxsPSIjYzQ5MTVjIi8+CjxjaXJjbGUgY3g9IjcwIiBjeT0iNjAiIHI9IjQiIGZpbGw9IiNjNDkxNWMiLz4KPC9zdmc+">
    <img alt="Seam" width="120" src="data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTIwIiBoZWlnaHQ9IjEyMCIgdmlld0JveD0iMCAwIDEyMCAxMjAiIGZpbGw9Im5vbmUiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyI+CjxjaXJjbGUgY3g9IjYwIiBjeT0iNjAiIHI9IjU2IiBzdHJva2U9IiNjNDkxNWMiIHN0cm9rZS13aWR0aD0iMiIgZmlsbD0ibm9uZSIvPgo8cGF0aCBkPSJNMzAgNDVDNDAgMzUgNjAgMzAgNzUgNDBDOTAgNTAgODAgNzAgNjUgNzVDNTAgODAgMzUgNzAgNDUgNTVDNTUgNDAgNzUgNDUgNzAgNjAiIHN0cm9rZT0iI2M0OTE1YyIgc3Ryb2tlLXdpZHRoPSIyLjUiIGZpbGw9Im5vbmUiIHN0cm9rZS1saW5lY2FwPSJyb3VuZCIvPgo8Y2lyY2xlIGN4PSIzMCIgY3k9IjQ1IiByPSI0IiBmaWxsPSIjYzQ5MTVjIi8+CjxjaXJjbGUgY3g9IjcwIiBjeT0iNjAiIHI9IjQiIGZpbGw9IiNjNDkxNWMiLz4KPC9zdmc+">
  </picture>
  <br/>
  <br/>
</p>

<h1 align="center">Seam</h1>

<p align="center">
  <strong>Your second brain. A local-first, AI-powered knowledge system built on markdown.</strong>
</p>

<p align="center">
  <a href="#features">Features</a> &middot;
  <a href="#architecture">Architecture</a> &middot;
  <a href="#getting-started">Getting Started</a> &middot;
  <a href="#development">Development</a> &middot;
  <a href="#documentation">Documentation</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25">
  <img src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black" alt="React 19">
  <img src="https://img.shields.io/badge/TypeScript-5.9-3178C6?logo=typescript&logoColor=white" alt="TypeScript 5.9">
  <img src="https://img.shields.io/badge/SQLite-FTS5-003B57?logo=sqlite&logoColor=white" alt="SQLite FTS5">
  <img src="https://img.shields.io/badge/AI-Ollama_(local)-000000?logo=ollama&logoColor=white" alt="Ollama">
</p>

---

<p align="center">
  <img src="./resources/feature-image.webp" alt="Seam — where things connect" width="100%">
</p>

Seam helps you capture, organize, and retrieve everything you know. All your notes live as plain `.md` files on your filesystem. AI runs entirely locally via [Ollama](https://ollama.com) -- no cloud, no API costs, no privacy trade-offs.

> **Seam** -- *where things connect.* The seam is the join between two pieces; knowledge gains meaning at the intersections.

---

## Features

### Capture

- **Quick capture** -- keyboard shortcut, dump the thought, organize later
- **URL-to-note** -- paste a URL to auto-fetch page title, og:title, and content as a note with source link
- **Voice transcription** -- record audio, Whisper transcribes locally, AI auto-summarizes
- **Inbox** -- nothing needs to be organized at capture time
- **Templates** -- project kick-off, meeting notes, research summary, daily log (with `{{variable}}` substitution)

### Organize

- **Projects** as first-class entities -- every note belongs to a project or lives in Inbox
- **`[[Wikilinks]]`** with autocomplete, alias support, and amber-highlighted decoration in the editor
- **`#Tags`** inline or in YAML frontmatter
- **AI auto-link suggestions** -- on save, AI reads the note and suggests links to related existing content
- **AI writing assist** -- expand a bullet into a paragraph, summarize a long doc, extract action items

### Retrieve

- **Full-text search** across all notes (SQLite FTS5 with BM25 ranking)
- **Semantic search** -- embeddings-based ("what did I write about caching strategies?")
- **Ask Seam** -- conversational chat grounded in your notes, with citations and streaming responses
- **AI synthesis** -- "summarize everything I know about project X" across all your notes
- **Backlinks panel** -- direct backlinks and two-hop backlinks with intermediate path display
- **Related notes** -- semantically similar notes, always visible alongside the editor

### Visualize

- **Knowledge graph** -- interactive node graph (Cytoscape.js), filterable by project/tag/date, click to open note, minimap
- **Timeline view** -- date-grouped notes with created/modified toggle, date picker for jump-to-date
- **Orphan detection** -- identifies notes with no inbound or outbound links

---

## Architecture

```
                    +------------------+
                    |     Clients      |
                    |  TUI  |  React   |
                    +--------+---------+
                             |
                      REST + WebSocket
                             |
                    +--------+---------+
                    |   Go Backend     |
                    |  chi router      |
                    |  JWT auth        |
                    +--+-----+-----+---+
                       |     |     |
                +------+  +--+--+  +-------+
                |         |     |          |
            SQLite     ChromaDB  Ollama   Filesystem
          (per-user)  (vectors)  (LLM)   (.md files)
```

**Multi-user, single machine.** Each user gets fully isolated storage -- their own SQLite database, their own notes directory on disk, their own embedding collection in ChromaDB. Users can browse and edit their `.md` files directly with any text editor; the server detects changes via `fsnotify` and re-indexes automatically.

### Tech Stack

| Layer | Technology | Why |
|---|---|---|
| **Backend** | Go + `net/http` + chi router | Strong concurrency, single binary, low memory footprint |
| **Storage** | Plain `.md` files on disk | Portable, human-readable, user-accessible. Source of truth for content |
| **Metadata** | SQLite (per-user) via `modernc.org/sqlite` | ACID transactions, FTS5, zero infrastructure. Pure Go, no CGO |
| **Full-text search** | SQLite FTS5 | Comes free with SQLite. BM25 ranking, highlighting, snippets |
| **Vector store** | ChromaDB (client-server mode) | Per-user collections, HTTP API, lightweight |
| **AI / LLM** | Ollama (local) | 100% private, no API costs. Language-agnostic HTTP API |
| **TUI** | Bubble Tea (Charm) | Elm architecture, mature Go TUI framework |
| **Web frontend** | React 19 + TypeScript 5.9 + Vite 7 | Best markdown editing experience via CodeMirror 6 |
| **Graph** | Cytoscape.js + fcose layout | Built-in zoom/pan/filter/click |
| **State management** | Zustand | Minimal, hook-based stores |
| **Auth** | JWT + bcrypt | Stateless tokens, mobile-ready |
| **File watching** | fsnotify | Detects external edits, triggers re-indexing |

### AI Models (via Ollama)

| Role | Default Model | Notes |
|---|---|---|
| Embeddings | `qwen3-embedding:8b` | Top-ranked on MTEB multilingual leaderboard |
| Chat + Synthesis | `qwen3:32b` | Ask Seam, summarization, auto-linking |
| Transcription | Whisper | Voice-to-text, runs fully locally |

All model names are config values -- swap based on your hardware with no code changes.

---

## Getting Started

### Prerequisites

| Requirement | Version | Purpose |
|---|---|---|
| [Go](https://go.dev) | 1.25+ | Build the server and TUI |
| [Node.js](https://nodejs.org) | 22+ | Build the web frontend |
| [Ollama](https://ollama.com) | Latest | AI features (embeddings, chat, synthesis) |
| [ChromaDB](https://www.trychroma.com) | Latest | Semantic search and embeddings (optional) |

### Quick Start

```bash
# 1. Clone and build
git clone https://github.com/katata/seam.git
cd seam
make build                          # builds bin/seamd (server) + bin/seam (TUI)

# 2. Configure
cp seam-server.yaml.example seam-server.yaml
# Edit seam-server.yaml:
#   - Set jwt_secret (run: openssl rand -hex 32)
#   - Set data_dir to where you want notes stored
#   - Set ollama_base_url if not localhost

# 3. Start Ollama + pull models
ollama pull qwen3:32b
ollama pull qwen3-embedding:8b

# 4. Run the server
make run                            # builds and starts seamd on :8080

# 5. TUI client (separate terminal)
./bin/seam --server http://localhost:8080

# 6. Web frontend (separate terminal)
cd web && npm install && npm run dev # Vite dev server on :5173, proxies /api to :8080
```

### Configuration

Seam is configured via `seam-server.yaml` with environment variable overrides:

```yaml
listen: ":8080"                      # SEAM_LISTEN
data_dir: "./data"                   # SEAM_DATA_DIR
jwt_secret: ""                       # SEAM_JWT_SECRET (required, min 32 chars)
ollama_base_url: "http://localhost:11434"  # SEAM_OLLAMA_URL
chromadb_url: "http://localhost:8000"      # SEAM_CHROMADB_URL

models:
  embeddings: "qwen3-embedding:8b"
  background: "qwen3:32b"
  chat: "qwen3:32b"

auth:
  access_token_ttl: "15m"
  refresh_token_ttl: "168h"
  bcrypt_cost: 12

ai:
  queue_workers: 1
  embedding_timeout: "60s"
  chat_timeout: "5m"

watcher:
  debounce_interval: "200ms"
```

---

## Data Format

Notes are plain markdown with YAML frontmatter:

```markdown
---
id: 01HX...
title: "API Design Patterns"
project: seam-backend
tags: [architecture, api, rest]
created: 2026-03-08T10:00:00Z
modified: 2026-03-08T12:30:00Z
source_url: https://example.com/article
---

Your notes here, with [[wikilinks]] and #tags inline.
```

### Storage Layout

```
{data_dir}/
  seam-server.yaml
  server.db                        # shared SQLite: user accounts, refresh tokens
  users/
    {user_id}/
      notes/                       # markdown files -- browse and edit directly
        inbox/                     # unsorted captures
        {project-slug}/            # one directory per project
      seam.db                      # per-user SQLite: metadata, FTS, links, AI tasks
```

Files live on disk at `{data_dir}/users/{user_id}/notes/`. Edit them with any tool you like -- Seam watches for changes and re-indexes automatically.

---

## API

### REST Endpoints

```
Auth
  POST   /api/auth/register
  POST   /api/auth/login
  POST   /api/auth/refresh
  POST   /api/auth/logout

Notes
  GET    /api/notes                     # list (filter by project, tag, date; paginated)
  POST   /api/notes                     # create
  GET    /api/notes/{id}                # read
  PUT    /api/notes/{id}                # update
  DELETE /api/notes/{id}                # delete
  GET    /api/notes/{id}/backlinks      # notes linking to this note
  GET    /api/notes/{id}/related        # semantically similar notes

Projects
  GET    /api/projects
  POST   /api/projects
  GET    /api/projects/{id}
  PUT    /api/projects/{id}
  DELETE /api/projects/{id}

Search
  GET    /api/search?q=...              # full-text search (FTS5)
  GET    /api/search/semantic?q=...     # semantic search (embeddings)

AI
  POST   /api/ai/synthesize             # AI synthesis over a scope
  POST   /api/ai/notes/{id}/assist      # writing assist (expand, summarize, extract)
  POST   /api/ai/reindex-embeddings     # batch reindex all embeddings

Capture
  POST   /api/capture                   # quick capture (text, URL, or voice)

Templates
  GET    /api/templates                 # list available templates
  POST   /api/templates/{name}/apply    # apply a template

Graph
  GET    /api/graph                     # node/edge data for graph view
  GET    /api/graph/two-hop-backlinks/{id}
  GET    /api/graph/orphans             # notes with no links

Tags
  GET    /api/tags                      # all tags with note counts

Chat
  POST   /api/chat/conversations        # create a conversation
  GET    /api/chat/conversations        # list conversations (paginated)
  GET    /api/chat/conversations/{id}   # get conversation with messages
  DELETE /api/chat/conversations/{id}   # delete a conversation
  POST   /api/chat/conversations/{id}/messages  # add a message

Settings
  GET    /api/settings                  # get all user settings
  PUT    /api/settings                  # update user settings
  DELETE /api/settings/{key}            # delete a setting

Review
  GET    /api/review/queue              # knowledge gardening review queue
```

### WebSocket

```
/ws                                     # authenticated connection per user
```

Events pushed to clients: `task.progress`, `task.complete`, `task.failed`, `note.changed`, `note.link_suggestions`, `chat.stream`, `chat.done`

---

## Development

```bash
make build                # build seamd (server) + seam (TUI) to ./bin/
make run                  # build and run the server
make dev-web              # run React dev server (Vite on :5173, proxies /api to :8080)
make test                 # all Go unit tests
make test-integration     # integration tests (real filesystem, on-disk SQLite)
make test-web             # all frontend tests (Vitest)
make lint                 # golangci-lint + eslint
make fmt                  # gofmt
make clean                # remove build artifacts + web/dist
```

### Running Specific Tests

```bash
# Single Go test
go test ./internal/note/ -run TestService_Create_WritesFile -v

# All tests in a package
go test ./internal/note/ -v

# Tests matching a pattern
go test ./internal/note/ -run "TestStore_.*" -v

# With race detector
go test -race ./internal/...

# Frontend tests
cd web && npx vitest run                       # all
cd web && npx vitest run src/api/client        # single file
```

### Build Tags

| Tag | Purpose |
|---|---|
| *(default)* | Unit tests only. No filesystem, no external services |
| `integration` | Real filesystem, on-disk SQLite |
| `external` | Requires running Ollama and/or ChromaDB |
| `performance` | Benchmarks (1000-note stress tests, concurrent users) |

### Test Coverage

- **Go:** 16 packages, 200+ tests across `auth`, `config`, `note`, `project`, `search`, `ai`, `capture`, `template`, `graph`, `watcher`, `ws`, `userdb`, `server`, `integration`
- **Frontend:** 18 test files, 153+ tests covering API client, stores, pages, components, and utilities

---

## Project Structure

```
cmd/
  seamd/                    # server binary
  seam/                     # TUI binary
  seed/                     # seed data generator for development
internal/
  ai/                       # Ollama client, ChromaDB client, task queue,
                            #   embedder, synthesis, auto-linker, writer
  auth/                     # user registration, login, JWT, bcrypt
  capture/                  # URL fetch (SSRF-safe), voice transcription
  chat/                     # Ask Seam conversational chat (RAG, streaming)
  config/                   # YAML + env config loading
  graph/                    # knowledge graph data (nodes, edges, orphans, two-hop)
  note/                     # note CRUD, frontmatter parser, wikilink parser, tag parser
  project/                  # project CRUD, slug generation, cascade delete
  reqctx/                   # request-scoped context keys (user ID, request ID)
  review/                   # knowledge gardening review queue (orphans, untagged, similar)
  search/                   # full-text search (FTS5) + semantic search
  server/                   # HTTP server, middleware, router wiring
  settings/                 # per-user settings (editor mode, sidebar state, etc.)
  template/                 # note templates with variable substitution
  userdb/                   # per-user SQLite database manager
  validate/                 # path traversal, input sanitization
  watcher/                  # fsnotify file watcher + startup reconciliation
  ws/                       # WebSocket hub (per-user connections, broadcast)
  testutil/                 # shared test helpers
  integration/              # e2e + performance tests
web/
  src/
    api/                    # API client with JWT auto-refresh, WebSocket client
    components/             # Sidebar, CommandPalette, NoteCard, CaptureModal,
                            #   SynthesisModal, EmptyState, Toast, ...
    pages/                  # Login, Inbox, Project, NoteEditor, Search,
                            #   Ask, Graph, Timeline, Settings
    stores/                 # Zustand stores (auth, notes, projects)
    lib/                    # markdown rendering, date formatting, sanitization
    styles/                 # CSS variables, global styles, CSS Modules
migrations/
  server/                   # server.db SQL migrations
  user/                     # per-user seam.db SQL migrations
```

---

## Design

Seam's frontend follows the **"Dark Cartography"** aesthetic -- warm, precise, and layered. Inspired by vintage cartography and technical draftsmanship.

- **Dark theme only** -- deep blue-black backgrounds with warm off-white text
- **Amber accent** (`#c4915c`) -- the golden thread linking ideas. Appears on wikilinks, graph edges, active navigation, and hover states
- **Four font families** -- Fraunces (display), Outfit (UI), Lora (content), IBM Plex Mono (code)
- **CSS custom properties** for all design tokens -- colors, spacing, typography, radii, z-indexes
- **CSS Modules** per component -- no global class name conflicts

---

## Security

- **Path traversal protection** -- rejects `..`, absolute paths, null bytes in all file operations
- **User isolation** -- user ID resolved from JWT in middleware, never accepted from request body
- **SSRF protection** -- URL capture rejects private IPs, localhost, `file://`, with DNS rebinding mitigation
- **Input validation** -- note titles, project names, tags sanitized for filesystem safety
- **Request body limits** -- 1MB max on all JSON endpoints, 100MB max on audio uploads
- **XSS prevention** -- DOMPurify sanitization on all rendered HTML
- **Rate limiting** -- bcrypt cost 12, max 10 refresh tokens per user, expired token cleanup

---

## Documentation

| Document | Description |
|---|---|
| [PLAN.md](./PLAN.md) | Architecture decisions, feature scope, data model |
| [AGENTS.md](./AGENTS.md) | Instructions for AI coding agents |

---

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

---

## License

TBD
