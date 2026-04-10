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

Seam is a personal knowledge system where your notes are plain `.md` files on disk and AI helps you think, find, and connect ideas. It runs entirely on your machine by default -- your notes never leave, your AI never phones home, and you never pay per token. Switch to OpenAI or Anthropic with one config line when you want to.

> **Seam** -- _the join between two pieces._ Knowledge gains meaning at the intersections.

## Why Seam

**Your notes are files.** Not rows in someone else's database. Plain markdown with YAML frontmatter, organized in folders. Edit them with Seam, vim, VS Code, or anything else. Back them up with git, sync them with Syncthing, grep them from the terminal. No export step because there's nothing to export from.

**AI runs on your machine.** [Ollama](https://ollama.com) powers everything by default -- embeddings, chat, search, writing assist. Zero cloud dependencies, zero API costs, zero privacy trade-offs. Need more horsepower? Switch to OpenAI or Anthropic with one config line. Your notes stay local either way.

**AI agents get memory.** Seam exposes an [MCP server](docs/mcp.md) that gives tools like Claude Code and Cursor persistent long-term memory, session tracking, and direct access to your knowledge base. Your AI assistant remembers what happened last session, knows what you're working on, and can search your notes for context -- across conversations, across tools.

## What You Can Do

### Ask your notes anything

"What did I write about caching strategies?" works even if you never used the word "caching." Semantic search finds meaning, not just keywords. **Ask Seam** combines retrieval with generation to answer questions grounded in things you actually wrote, with citations back to specific notes. Or synthesize across up to 50 notes at once -- "Summarize everything I know about project X."

### Work with an assistant that acts

The agentic assistant doesn't just answer questions about your knowledge base -- it works inside it. Ask it to capture a meeting summary, plan a project, find connections between ideas, or rewrite a section. It has [19 tools](docs/ai.md#tools) spanning notes, projects, tasks, search, the knowledge graph, and its own long-term memory. Six tools that write data pause and ask for your explicit approval. Every action is recorded in a full audit trail.

### Capture fast, find later

Quick capture for passing thoughts. URL-to-note for articles. Voice transcription via local Whisper -- no audio leaves your machine. Daily notes. Templates. Everything lands in your inbox until you or the Librarian sorts it.

### See how ideas connect

The knowledge graph shows connections between notes through `[[wikilinks]]`, shared `#tags`, and projects. Related notes surface semantically similar content you forgot you wrote. Two-hop backlinks reveal indirect connections. Orphan detection finds notes that need linking.

### Let AI organize for you

The [Librarian](docs/ai.md#librarian) is an autonomous background service that reviews orphaned and untagged notes, assigns them to projects, and adds tags from your existing taxonomy. It never touches your content, never invents new categories, and only processes notes that have been quiet for 15+ minutes. A library that shelves its own books.

### Give your AI agents memory

Seam's [MCP server](docs/mcp.md) turns AI coding tools into persistent collaborators. Start a session and your agent gets a briefing -- recent activity, relevant memories, open tasks. End it and findings are preserved for next time. Multiple agents can collaborate on the same investigation through session hierarchies and shared [research labs](docs/mcp.md#research-lab). Your coding agent and your knowledge base, connected.

## Quick Start

```bash
git clone https://github.com/katata/seam.git
cd seam
make build          # builds bin/seamd (server), bin/seam (TUI), bin/seam-reindex (re-embed tool)
make init           # interactive setup: JWT secret, data dir, LLM provider, ChromaDB
make run            # starts server on :8080

# In separate terminals:
./bin/seam --server http://localhost:8080          # TUI client
cd web && npm install && npm run dev               # Web frontend on :5173
```

Seam works without AI -- you get a solid markdown note system with full-text search out of the box. Add Ollama for AI features. Add ChromaDB for semantic search (`make init` can manage a Docker container for you). See [Getting Started](docs/getting-started.md) for prerequisites and the full setup walkthrough.

## How It Works

```
You write .md files       Seam watches and indexes       AI connects the dots
       |                           |                            |
       v                           v                            v
    notes/                      seam.db                     ChromaDB
    inbox/                    (SQLite FTS5)                 (embeddings)
    project-a/               metadata, links               similarity
    project-b/              tasks, versions                  search
       |                           |                            |
       +------------+--------------+----------------------------+
                    |
              seamd (Go binary)
              |       |       |
            Web     TUI    MCP Server
          (React) (Bubble  (AI coding
                   Tea)    agents)
```

A single Go binary serves the REST API, WebSocket events, and the MCP endpoint. SQLite handles metadata and full-text search. Your `.md` files on disk are the source of truth -- Seam watches for external edits and re-indexes automatically. ChromaDB stores vector embeddings for semantic search (optional, degrades gracefully if absent).

Three interfaces to the same data: a web app, a terminal TUI, and a full REST API.

### Graceful Degradation

| Missing | What happens |
| --- | --- |
| Ollama | AI features disabled. Markdown notes + FTS5 search still work. |
| ChromaDB | No semantic search. Full-text search and AI chat still work. |
| Whisper | No voice capture. Everything else works. |

## Documentation

| Document | Description |
| --- | --- |
| [Getting Started](docs/getting-started.md) | Prerequisites, installation, configuration |
| [AI & Assistant](docs/ai.md) | LLM providers, Ask Seam, agentic assistant, Librarian |
| [Architecture](docs/architecture.md) | System diagram, tech stack, data format, project layout |
| [API Reference](docs/api.md) | REST endpoints, WebSocket events |
| [MCP Server](docs/mcp.md) | Persistent memory and knowledge base access for AI coding agents |
| [Development](docs/development.md) | Build, test, lint, project structure |
| [Security](docs/security.md) | Threat model, auth, input validation, assistant safety |
| [Brand Guidelines](BRAND.md) | Visual identity, colors, fonts, logo usage |

## License

[MIT](LICENSE)
