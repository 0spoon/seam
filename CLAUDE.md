# CLAUDE.md

Quick-reference guide for AI assistants working in this repository. For detailed coding conventions, see `AGENTS.md`.

## What is Seam?

Seam is a local-first, AI-powered knowledge system built on markdown. Go backend (REST + WebSocket), React web frontend, Bubble Tea TUI. Single-user, single machine. Notes are plain `.md` files on disk with YAML frontmatter. AI is powered by Ollama by default (100% local, no cloud), with optional OpenAI and Anthropic providers.

## Project Structure

```
cmd/seamd/          Server binary (main entry, dependency wiring)
cmd/seam/           TUI client (Bubble Tea)
cmd/seam-reindex/   One-shot tool to re-embed notes after switching embedding model
internal/           Core domain packages (strict layering, no circular imports)
  agent/            MCP agent memory (sessions, knowledge, briefings)
  ai/               Ollama/OpenAI/Anthropic clients, ChromaDB, task queue, embeddings
  assistant/        Agentic assistant (tool-use loop, profile, long-term memory, audit)
  auth/             JWT + bcrypt authentication
  briefing/         Daily briefing assembler (used by the scheduler)
  capture/          URL fetch (SSRF-safe), voice transcription
  chat/             Conversation history persistence
  config/           YAML + env var config loading
  graph/            Knowledge graph (nodes, edges, orphans, two-hop)
  integration/      E2E + performance tests (build-tagged)
  librarian/        Autonomous note organizer (scheduler-driven, LLM classification)
  mcp/              MCP server (/api/mcp)
  note/             Note CRUD, frontmatter, wikilinks, tags, versions, daily
  project/          Project CRUD, slug generation
  reqctx/           Request-scoped context (user ID, request ID)
  review/           Knowledge gardening queue
  scheduler/        Cron-based scheduler for proactive jobs (daily briefing)
  search/           FTS5 + semantic search
  server/           HTTP server, middleware, router
  settings/         Owner settings
  task/             Checkbox task extraction and tracking
  template/         Note templates
  testutil/         Shared test helpers
  usage/            Token usage tracking, budget gating, dashboard API
  userdb/           SQLite database manager for the single seam.db
  validate/         Path traversal & input sanitization
  watcher/          fsnotify file watcher + startup reconciliation
  webhook/          Webhook CRUD, HMAC delivery, SSRF protection
  ws/               WebSocket hub (connection registry, broadcast)
migrations/         Embedded SQL schema (001_initial.sql, single flattened migration)
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

# ChromaDB container (optional, opt-in via `make init`)
make chroma-up          # Start ChromaDB container (docker/chroma-compose.yml)
make chroma-down        # Stop and remove the container
make chroma-logs        # Follow container logs
make chroma-status      # Container status

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

| Layer            | Technology                                            |
| ---------------- | ----------------------------------------------------- |
| Backend language | Go 1.25+ (no CGO)                                     |
| HTTP router      | chi v5                                                |
| Database         | SQLite (modernc.org/sqlite, WAL mode, FTS5)           |
| Vector store     | ChromaDB (HTTP API)                                   |
| LLM              | Ollama (local)                                        |
| Auth             | JWT + bcrypt                                          |
| IDs              | ULID everywhere (never UUID)                          |
| Frontend         | React 19 + TypeScript 5.9 + Vite 7                    |
| State            | Zustand                                               |
| Editor           | CodeMirror 6                                          |
| Graph            | Cytoscape.js + fcose                                  |
| Icons            | Lucide (only)                                         |
| CSS              | CSS Modules + CSS custom properties (dark theme only) |
| Tests (Go)       | testify/require, table-driven, in-memory SQLite       |
| Tests (web)      | Vitest + React Testing Library                        |

## Architecture Essentials

- **Single-user system** (since 2026-03-15, see `docs/security.md`): one owner per instance, identified via JWT middleware. All data lives at the top level of `data_dir` (no per-user subdirs). User ID is the constant `userdb.DefaultUserID`. Service and store APIs still take a `userID` parameter as a forward path back to multi-tenant; in production it is always `DefaultUserID`. Registration is closed after the first owner.
- **Notes are files**: `.md` files on disk are source of truth. `notes.body` in SQLite is a denormalized copy for FTS. fsnotify detects external edits.
- **Strict layering**: `cmd/` -> `internal/server` -> `internal/{domain}` -> `internal/userdb`, `ws`, `ai`. No package imports `internal/server`.
- **Domain package layout**: `handler.go`, `service.go`, `store.go`, `{feature}.go`, `*_test.go`.
- **Error pattern**: Domain errors as `var Err{Condition}` sentinels, wrapped with `fmt.Errorf("pkg.Service.Method: %w", err)`, mapped to HTTP status in handlers.
- **Config**: `seam-server.yaml` + env var overrides (`SEAM_JWT_SECRET`, `SEAM_DATA_DIR`, etc.).
- **MCP agent memory**: sessions, knowledge storage, briefings at `/api/mcp`.
- **ChromaDB is optional and external**: seamd treats it as a remote HTTP service. It can be unreachable at startup -- main.go probes once with a 2s heartbeat and logs a Warn, never blocks. The recommended way to run it locally is the Seam-managed Docker container in `docker/chroma-compose.yml` (managed via `make chroma-up`/`down`/`logs`/`status` or the optional `scripts/chroma-supervisor.sh` service installed by `make install-service`).

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
11. **Read `AGENTS.md` > "Common pitfalls"** before writing Go code -- recurring footguns from prior audit passes (`ulid.MustNew`, byte-slicing UTF-8, non-atomic `.md` writes, missing `RowsAffected`/`rows.Err()`, etc.). `make lint` enforces a subset; the rest is on you.

## Storage Layout

```
{data_dir}/
  seam.db                   # Owner account, notes metadata, FTS, links, AI tasks, agent memory, settings
  notes/                    # Markdown files on disk (source of truth)
    inbox/                  # Unsorted captures
    {project-slug}/         # One dir per project
  templates/                # Built-in note templates
  chromadb/                 # ChromaDB persistent volume (only when run via docker/chroma-compose.yml)
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

## MCP Server (Agent Tools)

Seam exposes an MCP server at `/api/mcp` that gives you persistent memory and access to the user's knowledge base. If configured, prefer these tools over asking the user to look things up manually.

**When to use it:**

- **Starting a work session** -- call `session_start` at the beginning of a non-trivial task. This returns a briefing with recent activity, relevant memories, and open tasks, giving you context you would otherwise lack. Call `session_end` when done to persist findings for next time.
- **Remembering things across conversations** -- use `memory_write` / `memory_read` to store and retrieve knowledge that should survive beyond this conversation (architectural decisions, debugging insights, user preferences specific to their notes). This is the agent's own scratchpad, separate from Claude Code's `~/.claude/` memory system.
- **Searching the user's notes** -- call `notes_search` or `context_gather` when the user references a topic, project, or idea that likely exists in their knowledge base. This is faster and more accurate than asking them to find it.
- **Creating notes** -- use `notes_create` to capture work output (meeting summaries, research findings, decision records) as notes in the user's system. These are auto-tagged `created-by:agent`.
- **Checking tasks** -- call `tasks_list` or `tasks_summary` to see what the user has on their plate before suggesting priorities or next steps.
- **Research and debugging** -- use the research lab tools (`lab_open`, `trial_record`, `decision_record`, `trial_query`) for systematic engineering investigations. Open a lab, record trials with expected vs actual outcomes, and track decisions. Multiple agents can collaborate on the same lab via the session hierarchy. Activate with `/seam-research <lab-name> <problem>`.

**When NOT to use it:**

- For information already in the codebase -- use `grep`/`read` instead.
- For ephemeral scratch work -- just think in-context.
- For things the user just told you -- don't parrot it back into memory.

**Available tool groups:** sessions (`session_*`), agent memory (`memory_*`), user notes (`notes_*`), tasks (`tasks_*`), webhooks (`webhook_*`), research lab (`lab_open`, `trial_*`, `decision_*`), context search (`context_gather`). Full reference: `docs/mcp.md`.

## Key Files to Know

| File | Why |
| --- | --- |
| `cmd/seamd/main.go` | Server entry point, dependency wiring |
| `internal/server/server.go` | Router setup, middleware chain |
| `internal/testutil/testutil.go` | Shared test helpers |
| `web/src/styles/variables.css` | All design tokens |
| `web/vite.config.ts` | Vite config, API proxy |
| `seam-server.yaml.example` | Configuration reference |
| `docker/chroma-compose.yml` | Seam-managed ChromaDB container (pinned image, bind-mounted volume) |
| `scripts/chroma-supervisor.sh` | Wakes Docker, waits for daemon, runs `compose up`. Run by the optional supervisor service. |
| `scripts/install-service.sh` | Installs seamd as launchd/systemd; optionally installs the chroma supervisor too |
| `AGENTS.md` | Detailed coding conventions and accepted designs |
| `docs/security.md` | Security invariants, assistant safety, known gaps |
