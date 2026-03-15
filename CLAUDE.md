# CLAUDE.md

Quick-reference guide for AI assistants working in this repository. For detailed coding conventions, see `AGENTS.md`.

## What is Seam?

Seam is a local-first, AI-powered knowledge system built on markdown. Go backend (REST + WebSocket), React web frontend, Bubble Tea TUI. Multi-user, single machine. Notes are plain `.md` files on disk with YAML frontmatter. AI is powered by Ollama (100% local, no cloud).

## Project Structure

```
cmd/seamd/          Server binary (main entry, dependency wiring)
cmd/seam/           TUI client (Bubble Tea)
cmd/seed/           Dev data generator
internal/           Core domain packages (strict layering, no circular imports)
  ai/               Ollama client, ChromaDB, task queue, embeddings
  agent/            MCP agent memory (sessions, knowledge, briefings)
  auth/             JWT + bcrypt authentication
  capture/          URL fetch (SSRF-safe), voice transcription
  chat/             Conversational RAG with streaming
  config/           YAML + env var config loading
  graph/            Knowledge graph visualization
  integration/      E2E + performance tests (build-tagged)
  mcp/              MCP server (/api/mcp)
  note/             Note CRUD, frontmatter, wikilinks, tags
  project/          Project CRUD, slug generation
  reqctx/           Request context keys
  review/           Knowledge gardening queue
  search/           FTS5 + semantic search
  server/           HTTP server, middleware, router
  settings/         Per-user settings
  template/         Note templates
  testutil/         Shared test helpers
  userdb/           Per-user SQLite DB manager (WAL, idle eviction)
  validate/         Path traversal & input sanitization
  watcher/          fsnotify file watcher
  ws/               WebSocket hub (per-user)
migrations/
  server/           server.db migrations (users, refresh tokens)
  user/             per-user seam.db migrations (notes, projects, links, FTS, agent)
web/                React SPA (TypeScript, Vite, Zustand)
  src/api/          REST + WebSocket client with JWT auto-refresh
  src/components/   UI components (CSS Modules)
  src/pages/        Route pages
  src/stores/       Zustand stores
  src/styles/       CSS variables, global styles
data/templates/     Built-in note templates
```

## Common Commands

```bash
# Build & Run
make build              # Build server + TUI to ./bin/
make run                # Build and run server (reads seam-server.yaml, default :8080)
make dev-web            # Vite dev server on :5173, proxies /api to :8080

# Testing
make test               # Go unit tests (no filesystem, no external services)
make test-integration   # Integration tests (real filesystem, on-disk SQLite)
make test-web           # Frontend tests (Vitest)

# Single Go test
go test ./internal/note/ -run TestService_Create -v

# Frontend test (single file)
cd web && npx vitest run src/api/client

# Linting & Formatting
make lint               # golangci-lint + eslint
make fmt                # gofmt + prettier
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend language | Go 1.25+ (no CGO) |
| HTTP router | chi v5 |
| Database | SQLite (modernc.org/sqlite, WAL mode, FTS5) |
| Vector store | ChromaDB (HTTP API) |
| LLM | Ollama (local) |
| Auth | JWT + bcrypt |
| IDs | ULID everywhere (never UUID) |
| Frontend | React 19 + TypeScript 5.9 + Vite 7 |
| State | Zustand |
| Editor | CodeMirror 6 |
| Graph | Cytoscape.js + fcose |
| Icons | Lucide (only) |
| CSS | CSS Modules + CSS custom properties (dark theme only) |
| Tests (Go) | testify/require, table-driven, in-memory SQLite |
| Tests (web) | Vitest + React Testing Library |

## Architecture Essentials

- **Per-user isolation**: each user gets their own SQLite DB (`seam.db`), notes directory, and ChromaDB collection. User ID comes from JWT middleware, never from request body.
- **Notes are files**: `.md` files on disk are source of truth. `notes.body` in SQLite is a denormalized copy for FTS. fsnotify detects external edits.
- **Strict layering**: `cmd/` -> `internal/server` -> `internal/{domain}` -> `internal/userdb`, `ws`, `ai`. No package imports `internal/server`.
- **Domain package layout**: `handler.go`, `service.go`, `store.go`, `{feature}.go`, `*_test.go`.
- **Error pattern**: Domain errors as `var Err{Condition}` sentinels, wrapped with `fmt.Errorf("pkg.Service.Method: %w", err)`, mapped to HTTP status in handlers.
- **Config**: `seam-server.yaml` + env var overrides (`SEAM_JWT_SECRET`, `SEAM_DATA_DIR`, etc.).
- **MCP agent memory**: sessions, knowledge storage, briefings at `/api/mcp`.

## Testing Conventions

- Use `testify/require` (fail fast), not `assert`.
- Table-driven tests for multiple input variations.
- SQLite test isolation: `file:{t.Name()}?mode=memory&cache=shared` (named in-memory DBs).
- Mock external services (Ollama, ChromaDB) with `httptest.NewServer()`.
- Test helpers: `testutil.TestServerDB(t)`, `testutil.TestUserDB(t)`, `testutil.TestDataDir(t)`.
- Build tags: default (unit), `integration`, `external` (needs Ollama/ChromaDB), `performance`.
- No `time.Sleep` for sync -- use channels or `sync.WaitGroup`.

## Key Rules

1. **No CGO** -- pure Go SQLite driver only.
2. **No emojis** in code or comments.
3. **No circular imports** -- respect the layering.
4. **Context everywhere** -- all service/store methods take `context.Context` first.
5. **ULID for IDs** -- never UUID.
6. **CSS variables only** -- never hardcode colors, spacing, or font sizes in the frontend.
7. **No `!important`** in CSS. Max 2 levels of nesting in CSS Modules.
8. **Security first** -- reject `..` in paths, validate all input at handler level, reject private IPs in URL capture.
9. **Sanitize responses** -- never expose internal error details in HTTP responses.
10. **Migrations must be idempotent** -- embedded via `go:embed`, run on DB open.

## Storage Layout

```
{data_dir}/
  server.db                 # Shared: users, refresh tokens
  users/{user_id}/
    notes/
      inbox/                # Unsorted captures
      {project-slug}/       # One dir per project
    seam.db                 # Per-user: metadata, FTS, links, AI tasks
```

## Note Format

```markdown
---
id: 01HX...
title: "Note Title"
project: project-slug
tags: [tag1, tag2]
created: 2026-03-08T10:00:00Z
modified: 2026-03-08T12:30:00Z
source_url: https://example.com
---

Content with [[wikilinks]] and #tags.
```

## Frontend Design System

- **Dark theme only** with warm aesthetics.
- Backgrounds: deep blue-black (`--bg-primary: #1d2130`).
- Text: warm off-white (`--text-primary: #e8e2d9`).
- Accent: amber/copper (`--accent-primary: #c4915c`) -- the "golden thread."
- Fonts: Fraunces (display), Outfit (UI), Lora (content), IBM Plex Mono (code).
- All tokens in `web/src/styles/variables.css`.
- Components use CSS Modules (`.module.css`, camelCase class names).

## Key Files to Know

| File | Why |
|------|-----|
| `cmd/seamd/main.go` | Server entry point, dependency wiring |
| `internal/server/server.go` | Router setup, middleware chain |
| `internal/testutil/testutil.go` | Shared test helpers |
| `web/src/styles/variables.css` | All design tokens |
| `web/vite.config.ts` | Vite config, API proxy |
| `seam-server.yaml.example` | Configuration reference |
| `AGENTS.md` | Detailed coding conventions |
| `SECURITY.md` | Security policies |
