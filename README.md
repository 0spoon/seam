<p align="center">
  <br/>
  <img alt="Seam" width="120" src="./resources/logo.svg">
  <br/>
  <br/>
</p>

<h1 align="center">Seam</h1>

<p align="center">
  <strong>Where ideas connect.</strong><br/>
  <em>A local-first, AI-powered knowledge system built on plain markdown.</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25">
  <img src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black" alt="React 19">
  <img src="https://img.shields.io/badge/TypeScript-5.9-3178C6?logo=typescript&logoColor=white" alt="TypeScript 5.9">
  <img src="https://img.shields.io/badge/SQLite-FTS5-003B57?logo=sqlite&logoColor=white" alt="SQLite FTS5">
  <img src="https://img.shields.io/badge/AI-Local_First-000000?logo=ollama&logoColor=white" alt="Local AI">
</p>

---

<p align="center">
  <img src="./resources/feature-image.webp" alt="Seam -- where things connect" width="100%">
</p>

Seam is a knowledge system where your notes are plain `.md` files on disk and AI helps you find things again later. It runs locally by default via [Ollama](https://ollama.com) -- no cloud, no API costs, no one reading your notes. Switch to OpenAI or Anthropic with one config line. Your notes stay local either way.

> **Seam** -- *where things connect.* The seam is the join between two pieces; knowledge gains meaning at the intersections.

## What it Does

**Capture** -- Quick capture, URL-to-note, voice transcription, daily notes, templates, inbox.

**Organize** -- Projects, `[[wikilinks]]`, `#tags`, bulk actions, version history, task extraction.

**Retrieve** -- Full-text search (FTS5), semantic search, Ask Seam (RAG chat with citations), AI synthesis, backlinks, related notes, review queue.

**Visualize** -- Knowledge graph, timeline view, orphan detection.

**AI** -- Three LLM providers (Ollama, OpenAI, Anthropic). Embeddings always local. Auto-link suggestions, writing assist, tag/project suggestions, voice transcription with Whisper.

**Assistant** -- Agentic chat that actually does things. Tool-use loop calls into your notes, projects, tasks, search, and graph (22+ tools), with explicit approval gates for writes and a full audit trail. Per-user profile and long-term memory (facts, preferences, decisions, commitments) with FTS5 search, recency decay, and automatic conversation summarization. Streaming responses over SSE.

**Daily Briefing** -- In-process cron scheduler assembles a daily summary note from recent activity. Auto-provisioned on first run; manage schedules via `/api/schedules` or trigger on demand.

**Agent Memory** -- MCP server gives AI coding agents persistent long-term memory, session tracking, and access to your knowledge base.

## Quick Start

```bash
git clone https://github.com/katata/seam.git
cd seam
make build          # builds bin/seamd (server) + bin/seam (TUI)
make init           # interactive config (JWT secret, data dir, LLM provider, Chroma)
make chroma-up      # optional: start the Seam-managed ChromaDB container (if you picked Docker in init)
make run            # starts server on :8080

# In separate terminals:
./bin/seam --server http://localhost:8080          # TUI client
cd web && npm install && npm run dev               # Web frontend on :5173
```

`make init` asks how you want to handle ChromaDB -- a Seam-managed Docker container (recommended), an external instance you already run, or disable semantic search entirely. See [Getting Started](docs/getting-started.md) for prerequisites, the optional supervisor service, and full configuration.

## Documentation

| Document | Description |
|---|---|
| [Getting Started](docs/getting-started.md) | Prerequisites, installation, configuration |
| [AI](docs/ai.md) | LLM providers, features, task queue, models |
| [Architecture](docs/architecture.md) | System diagram, tech stack, data format, project structure |
| [API Reference](docs/api.md) | REST endpoints, WebSocket events |
| [MCP Agent Memory](docs/mcp.md) | MCP tools for AI coding agents |
| [Development](docs/development.md) | Build, test, lint, project structure |
| [Security](docs/security.md) | Security model and invariants |
| [Brand Guidelines](BRAND.md) | Visual identity, colors, fonts, logo usage |

## License

[MIT](LICENSE)
