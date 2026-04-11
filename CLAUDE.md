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
make build              # Build server + TUI + reindex tool to ./bin/
make dev                # Run seamd + Vite + Chroma in parallel (Ctrl-C to stop)
make run                # Build and run seamd alone (no Vite, no Chroma)
make dev-web            # Vite dev server only (:5173, proxies /api to :8080)

# Testing
make test               # Go unit tests
make test-integration   # Integration tests (real filesystem, on-disk SQLite)
make test-race          # Go unit tests with race detector
make test-web           # Frontend tests (Vitest, single run)
make test-web-watch     # Frontend tests in watch mode

# Quality
make lint               # golangci-lint + eslint
make fmt                # gofmt + prettier
make vet                # go vet
make typecheck          # TypeScript typecheck (no emit)
make coverage           # Go tests with coverage report (coverage.html)

# Single test
go test ./internal/note/ -run TestService_Create -v
cd web && npx vitest run src/api/client

# ChromaDB container (optional, opt-in via `make init`)
make chroma-up          # Start ChromaDB container
make chroma-down        # Stop and remove the container
make chroma-logs        # Follow container logs
make chroma-status      # Container status

# Service management (after `make install-service`)
make service-status     # Show service status
make service-start      # Start seamd service
make service-stop       # Stop seamd service
make service-restart    # Restart seamd service
make logs               # Tail seamd + Chroma logs
make kill-stale         # Kill stale seamd listener on configured port

# Install
make install-service       # Install seamd as launchd/systemd user service (also drops the /seam-onboard skill)
make install-tui           # Install seam TUI to /usr/local/bin (PREFIX= to override)
make install-onboard-skill # (Re)install the /seam-onboard Claude Code skill for teaching new sessions about Seam MCP
make reindex               # Re-embed all notes after switching embedding model
make clean                 # Remove bin/, web/dist, coverage files
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

## Coding Rules

All Go and frontend coding conventions, testing patterns, common pitfalls, forbidden APIs, and accepted designs are in `AGENTS.md`. Read it before writing code.
