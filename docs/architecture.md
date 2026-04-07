# Architecture

## System Diagram

```mermaid
graph TD
    subgraph Clients
        TUI["TUI (Bubble Tea)"]
        Web["Web (React 19)"]
        MCP["MCP Agents"]
    end

    TUI & Web & MCP <-->|"REST + WebSocket + MCP"| Backend

    subgraph Backend["Go Backend"]
        Router["chi router"]
        Auth["JWT auth"]
        Queue["AI task queue"]
    end

    Backend --> SQLite["SQLite<br/>Metadata + FTS5"]
    Backend --> Chroma["ChromaDB<br/>Vector embeddings"]
    Backend --> LLM["LLM Provider<br/>Ollama / OpenAI / Anthropic"]
    Backend --> FS["Filesystem<br/>Plain .md files"]

    FS -.->|"fsnotify"| Backend

    style Backend fill:#1d2130,stroke:#c4915c,color:#e8e2d9
    style Clients fill:#1d2130,stroke:#3a4260,color:#e8e2d9
    style TUI fill:#161922,stroke:#3a4260,color:#e8e2d9
    style Web fill:#161922,stroke:#3a4260,color:#e8e2d9
    style MCP fill:#161922,stroke:#3a4260,color:#e8e2d9
    style Router fill:#161922,stroke:#3a4260,color:#e8e2d9
    style Auth fill:#161922,stroke:#3a4260,color:#e8e2d9
    style Queue fill:#161922,stroke:#3a4260,color:#e8e2d9
    style SQLite fill:#161922,stroke:#6b9b7a,color:#e8e2d9
    style Chroma fill:#161922,stroke:#7b8ec4,color:#e8e2d9
    style LLM fill:#161922,stroke:#b87ba4,color:#e8e2d9
    style FS fill:#161922,stroke:#c4915c,color:#e8e2d9
```

**Single-user, single machine.** Seam consolidated to a single-owner model on 2026-03-15 (see [SECURITY.md](../SECURITY.md) for the rationale): one shared `seam.db`, one notes directory, one ChromaDB collection, registration closed after the first owner. Service and store APIs still take a `userID` parameter so the architecture can return to multi-tenant later, but in production the constant `userdb.DefaultUserID` is wired in throughout. Edit your `.md` files with vim, VS Code, or whatever you want -- Seam watches for changes via `fsnotify` and re-indexes automatically.

**ChromaDB is the only external runtime dependency for AI features.** seamd treats it as a remote HTTP service (always, even when it lives on `localhost`) and tolerates it being unreachable -- a 2-second startup probe logs a Warn but never blocks boot, and the AI task queue retries naturally once Chroma comes up. The recommended deployment is the Seam-managed Docker container in `docker/chroma-compose.yml`, started via `make chroma-up` or kept alive by the optional supervisor service installed by `make install-service`. See [Getting Started](getting-started.md#chromadb) for the full setup story.

## Tech Stack

| Layer | Technology | Why |
|---|---|---|
| **Backend** | Go + chi router | Single binary, low memory, strong concurrency. No CGO |
| **Storage** | Plain `.md` files on disk | Portable, human-readable, yours forever. Source of truth |
| **Metadata** | SQLite (`modernc.org/sqlite`) | ACID, FTS5, zero infrastructure. Pure Go |
| **Vector store** | ChromaDB (optionally containerized) | Per-user collections, HTTP API. Seam can manage it via `docker/chroma-compose.yml` |
| **AI** | Ollama / OpenAI / Anthropic | Local by default, cloud when you want it |
| **TUI** | Bubble Tea | Elm architecture for your terminal |
| **Web** | React 19 + TypeScript 5.9 + Vite 7 | CodeMirror 6 markdown editor |
| **Graph** | Cytoscape.js + fcose | Interactive knowledge graph |
| **State** | Zustand | Minimal, hook-based |
| **Auth** | JWT + bcrypt | Stateless tokens |
| **File watching** | fsnotify | Detects external edits |

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

## Storage Layout

```
{data_dir}/
  server.db                        # owner account, refresh tokens
  seam.db                          # notes metadata, FTS, links, AI tasks, agent memory
  notes/                           # your markdown files -- edit with anything
    inbox/                         # unsorted captures
    {project-slug}/                # one directory per project
  templates/                       # built-in note templates
  chromadb/                        # ChromaDB persistent volume (only when run via docker/chroma-compose.yml)
```

Files live on disk. Edit them with whatever you want. Seam watches and re-indexes.

## Project Structure

```
cmd/
  seamd/                    # server binary
  seam/                     # TUI binary
  seed/                     # seed data generator
internal/
  agent/                    # MCP agent sessions, memory, briefings, tool audit
  ai/                       # providers (Ollama, OpenAI, Anthropic), embedder,
                            #   synthesizer, auto-linker, writer, suggester, queue
  auth/                     # registration, login, JWT, bcrypt
  capture/                  # URL fetch (SSRF-safe), voice transcription
  chat/                     # conversation history persistence
  config/                   # YAML + env config loading
  graph/                    # knowledge graph (nodes, edges, orphans, two-hop)
  mcp/                      # MCP server, Streamable HTTP, tool handlers
  note/                     # CRUD, frontmatter, wikilinks, tags, versions, daily
  project/                  # CRUD, slugs, cascade delete
  reqctx/                   # request-scoped context (user ID, request ID)
  review/                   # knowledge gardening queue
  search/                   # FTS5 + semantic search
  server/                   # HTTP server, middleware, router wiring
  settings/                 # owner settings
  task/                     # checkbox task extraction and tracking
  template/                 # note templates with variable substitution
  userdb/                   # SQLite database manager (owner-scoped seam.db)
  validate/                 # path traversal, input sanitization
  watcher/                  # fsnotify file watcher + startup reconciliation
  webhook/                  # webhook CRUD, HMAC delivery, SSRF protection
  ws/                       # WebSocket hub (connection registry, broadcast)
  testutil/                 # shared test helpers
  integration/              # e2e + performance tests
web/
  src/
    api/                    # HTTP client with JWT auto-refresh, WebSocket client
    components/             # Sidebar, CommandPalette, NoteCard, CaptureModal,
                            #   SynthesisModal, BulkActionBar, VersionHistory, ...
    pages/                  # Login, Inbox, Project, NoteEditor, Search,
                            #   Ask, Graph, Timeline, Review, Settings
    stores/                 # Zustand (auth, notes, projects, settings, ui, review)
    lib/                    # markdown rendering, date formatting, sanitization
    styles/                 # CSS variables, global styles, CSS Modules
migrations/
  server/                   # server.db migrations
  user/                     # seam.db migrations (owner-scoped data)
docs/                       # documentation
```
