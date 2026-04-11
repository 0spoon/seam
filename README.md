<p align="center">
  <br/>
  <img alt="Seam" width="120" src="./resources/logo.svg">
  <br/>
  <br/>
</p>

<h1 align="center">Seam</h1>

<p align="center">
  <strong>Where ideas connect.</strong><br/>
  <em>Shared memory for you and your AI agents. Local-first, built on plain markdown.</em>
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

You talk to a lot of agents. Claude Code for one task, Cursor for another, a research session here, a debugging session there. Each one starts from zero. What you figured out in one conversation doesn't carry over to the next. What one agent discovered, the other will never know. The knowledge that didn't make it into code or your own memory is gone.

Seam is the fix. It's a local knowledge base -- plain `.md` files on disk -- with an [MCP server](docs/mcp.md) that gives every agent you work with shared, persistent memory. Start a coding session and your agent gets a briefing: what happened in past sessions, what other agents found, what's on your plate. End the session and findings are preserved for next time. Your agents stop being isolated conversations and start being a team that builds on each other's work.

Everything runs on your machine by default. Your notes, your AI, your vectors -- nothing leaves unless you want it to.

> **Seam** -- _the join between two pieces._ Knowledge gains meaning at the intersections.

## Why Seam

**Your agents forget everything.** Every conversation is a blank slate. You re-explain context, re-share decisions, watch agents rediscover things you already know. Seam gives every MCP-compatible agent -- Claude Code, Cursor, Windsurf, anything -- access to persistent memory, session history, and your full knowledge base. What one agent learns, the next one knows.

**Your notes are files.** Not rows in someone else's database. Plain markdown with YAML frontmatter, organized in folders. Edit them with Seam, vim, VS Code, or anything else. Back them up with git, sync them with Syncthing, grep them from the terminal. No export step because there's nothing to export from.

**AI runs on your machine.** [Ollama](https://ollama.com) powers everything by default -- embeddings, chat, search, writing assist. Zero cloud dependencies, zero API costs, zero privacy trade-offs. Need more horsepower? Switch to OpenAI or Anthropic with one config line. Your notes stay local either way.

## What You Can Do

### Connect all your agents

Seam's [MCP server](docs/mcp.md) turns isolated agent conversations into a connected workflow. Every agent reads from and writes to the same knowledge base -- session plans, findings, research notes, decisions. Multiple agents can [collaborate](docs/mcp.md#research-lab) on the same investigation through session hierarchies and shared research labs. Your afternoon debugging session in Cursor knows what your morning architecture session in Claude Code decided.

### Ask your notes anything

"What did I write about caching strategies?" works even if you never used the word "caching." Semantic search finds meaning, not just keywords. **Ask Seam** combines retrieval with generation to answer questions grounded in things you actually wrote, with citations back to specific notes. Or synthesize across up to 50 notes at once -- "Summarize everything I know about project X."

### Work with an assistant that acts

The agentic assistant doesn't just answer questions about your knowledge base -- it works inside it. Ask it to capture a meeting summary, plan a project, find connections between ideas, or rewrite a section. It has [19 tools](docs/ai.md#tools) spanning notes, projects, tasks, search, the knowledge graph, and its own long-term memory. Six tools that write data pause and ask for your explicit approval. Every action is recorded in a full audit trail.

### Capture fast, find later

Quick capture for passing thoughts. URL-to-note for articles. Voice transcription via local Whisper -- no audio leaves your machine. Daily notes. Templates. Everything lands in your inbox until you or the [Librarian](docs/ai.md#librarian) sorts it.

### See how ideas connect

The knowledge graph shows connections between notes through `[[wikilinks]]`, shared `#tags`, and projects. Related notes surface semantically similar content you forgot you wrote. Two-hop backlinks reveal indirect connections. Orphan detection finds notes that need linking.

### Let AI organize for you

The [Librarian](docs/ai.md#librarian) is an autonomous background service that reviews orphaned and untagged notes, assigns them to projects, and adds tags from your existing taxonomy. It never touches your content, never invents new categories, and only processes notes that have been quiet for 15+ minutes. A library that shelves its own books.

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
