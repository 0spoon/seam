# Seam

**Your second brain. A local-first, AI-powered knowledge system built on markdown.**

Seam helps you capture, organize, and retrieve everything you know. All your notes live as plain `.md` files on your filesystem. AI runs entirely locally via [Ollama](https://ollama.com) -- no cloud, no API costs, no privacy trade-offs.

---

## What it does

- **Capture** -- quick-capture modal, URL-to-note, voice transcription via Whisper, inbox for unsorted thoughts
- **Organize** -- projects, `[[wikilinks]]`, `#tags`, templates, AI-suggested links between related notes
- **Search** -- full-text search (SQLite FTS5) and semantic search ("what did I write about caching strategies?")
- **Ask Seam** -- conversational chat grounded in your notes, with citations
- **Synthesize** -- "summarize everything I know about project X" across all your notes
- **Visualize** -- interactive knowledge graph, timeline view, backlinks panel

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
                  +--+-----+-----+--+
                     |     |     |
              +------+  +--+--+  +-------+
              |         |     |          |
          SQLite     ChromaDB  Ollama   Filesystem
        (per-user)  (vectors)  (LLM)   (.md files)
```

**Multi-user from day one.** Each user gets isolated storage -- their own SQLite database, their own notes directory, their own embedding collection. Users can browse and edit their `.md` files directly with any text editor; the server detects changes and re-indexes automatically.

## Tech stack

| Layer | Choice |
|---|---|
| Backend | Go, `net/http`, chi router |
| Metadata + search | SQLite (per-user) with FTS5 |
| Vector store | ChromaDB (client-server mode) |
| AI | Ollama (local) -- `qwen3:32b` for chat/synthesis, `qwen3-embedding:8b` for embeddings |
| TUI | Bubble Tea |
| Web | React, CodeMirror 6, Cytoscape.js |
| File watching | fsnotify |

## Prerequisites

- Go 1.24+
- Node.js 22+ (for the web frontend)
- [Ollama](https://ollama.com) running locally (for AI features)
- [ChromaDB](https://www.trychroma.com) in server mode (for semantic search)

## Getting started

```bash
# Build
make build

# Configure
cp seam-server.yaml.example seam-server.yaml
# Edit seam-server.yaml -- set data_dir and ollama_base_url

# Run the server
make run

# TUI client (separate terminal)
./bin/seam --server http://localhost:8080

# Web frontend (separate terminal)
cd web && npm install && npm run dev
```

## Development

```bash
make test              # unit tests
make test-integration  # integration tests (requires filesystem)
make lint              # golangci-lint + eslint
make fmt               # gofmt + prettier
```

## Project structure

```
cmd/
  seamd/          server binary
  seam/           TUI binary
internal/
  config/         config loading
  server/         HTTP server, middleware, router
  auth/           users, JWT, bcrypt
  userdb/         per-user SQLite database manager
  note/           note CRUD, frontmatter, wikilinks, tags
  project/        project CRUD
  search/         full-text + semantic search
  ai/             Ollama client, ChromaDB client, task queue, embedder, chat, synthesis
  capture/        URL fetch, voice transcription
  template/       note templates
  watcher/        filesystem watcher + reconciliation
  ws/             WebSocket hub
  graph/          knowledge graph data
web/              React frontend
migrations/       SQL migration files
```

## Data format

Notes are plain markdown with YAML frontmatter:

```markdown
---
id: 01HX...
title: "API Design Patterns"
project: seam-backend
tags: [architecture, api, rest]
created: 2026-03-08T10:00:00Z
modified: 2026-03-08T12:30:00Z
---

Your notes here, with [[wikilinks]] and #tags inline.
```

Files live on disk at `{data_dir}/users/{user_id}/notes/`. Edit them with any tool you like.

## Documentation

- [PLAN.md](./PLAN.md) -- architecture decisions and feature scope
- [IMP_PLAN.md](./IMP_PLAN.md) -- detailed implementation plan with task breakdown
- [TEST_PLAN.md](./TEST_PLAN.md) -- comprehensive test specifications (TDD)

## License

TBD
