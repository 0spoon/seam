# Seam — Project Plan

> *Your second brain. A local-first, AI-powered knowledge system built on markdown.*

---

## Vision

Seam is a personal knowledge OS that helps you capture, organize, and retrieve everything you know. Inspired by Obsidian's linking model and graph navigation, Seam adds a full AI layer — running entirely locally via Ollama — for semantic search, synthesis, and intelligent connection of ideas. All data lives as plain `.md` files on your filesystem. No cloud lock-in. No privacy trade-offs.

---

## Name

**Seam** — where things connect. The seam is the join between two pieces; knowledge gains meaning at the intersections.

---

## Target Platforms

- **Phase 1:** Go backend (REST + WebSocket API), TUI client (Bubble Tea), React web app
- **Later:** Android / iOS (API is client-agnostic from day one)
- **Deferred:** Web clipper extension, screenshot OCR, sync/cloud backup, email-to-note

---

## Multi-User Design

Seam is multi-user from day one. Multiple users share a single server instance running on one machine. Each user has fully isolated data — notes, metadata, embeddings, and configuration.

### User isolation guarantees

- Each user's markdown notes live in their own directory, accessible directly on the filesystem
- Each user has their own SQLite database (metadata, full-text search index, link graph)
- Embedding collections in ChromaDB are namespaced per user
- No user can access another user's notes or search results through the API
- AI task queue enforces fair scheduling across users

---

## MVP Feature Set

### Capture

- Full markdown editor with live preview (web) / modal editor (TUI)
- Quick-capture — keyboard shortcut, dump the thought, organize later
- Paste URL to auto-fetch page title + save as note with source URL embedded
- Voice memo to Whisper transcription to auto-summarized note
- Capture queue / Inbox — nothing needs to be organized at capture time

### Organization

- **Projects as first-class entities** — every note belongs to a project, or lives in Inbox
- `[[wikilinks]]` with autocomplete
- Tags via `#tag` inline or YAML frontmatter
- Templates — project kick-off, meeting notes, research summary, daily log
- AI auto-link suggestions — on save, AI reads the note and suggests links to related existing content

### Retrieval

- Full-text search across all notes (SQLite FTS5)
- **Semantic search** — embeddings-based ("what did I write about caching strategies?")
- **AI synthesis** — "summarize everything I know about project X" across all notes
- **Ask Seam** — conversational chat interface grounded in your notes only
- Project view — all notes/docs for a project aggregated in one place
- Backlinks panel — what else links to this note?
- Related notes panel — semantically similar notes, always visible alongside the editor
- Timeline view — calendar-style view of notes by creation/modification date

### Editing

- Split view — edit markdown on the left, rendered preview on the right (web)
- AI writing assist — expand a bullet into a paragraph, summarize a long doc, extract action items
- Templates (applied on new note creation)

### Visualization

- **Knowledge graph view** — interactive node graph (Cytoscape.js), filterable by project / tag / date, click to open note

### UI

- Dark theme only (use CSS variables from day one so a light theme can be added later without refactoring)

---

## Data Model

### Notes on disk

Each note is a single `.md` file with YAML frontmatter. The frontmatter contains only metadata that is not derivable from the note body:

```markdown
---
id: ulid_01HX...
title: "API Design Patterns"
project: seam-backend
tags: [architecture, api, rest]
created: 2026-03-08T10:00:00Z
modified: 2026-03-08T12:30:00Z
source_url: https://example.com/article   # if captured from web
transcript_source: true                   # if captured from voice
---

Note content here, with [[wikilinks]] and #tags inline.
```

Things **not** stored in frontmatter:
- `links` — parsed from `[[wikilinks]]` in the body at index time, stored in SQLite. Keeping a duplicate list in frontmatter would go stale when users edit files directly.

### Note IDs

ULID (Universally Unique Lexicographically Sortable Identifier). Time-ordered, globally unique, shorter than UUID. Every note gets a ULID assigned at creation.

### SQLite database (per user)

Each user has their own `seam.db` containing:

| Table | Purpose |
|---|---|
| `notes` | Note metadata: id, title, project, tags, created, modified, file path |
| `notes_fts` | FTS5 virtual table for full-text search over note content |
| `links` | Directed edges: source_note_id -> target_note_id (parsed from `[[wikilinks]]`) |
| `projects` | Project metadata: id, name, description, created |
| `tags` | Tag index for fast filtering |
| `ai_tasks` | AI task queue: pending embedding jobs, synthesis requests, etc. |

The markdown files on disk are the source of truth for note **content**. SQLite is the source of truth for **metadata and indexes**. On startup and on file change events, the two are reconciled.

### ChromaDB (shared instance, per-user collections)

Vector embeddings for semantic search. Runs in client-server mode to handle concurrent access from multiple user sessions.

- Collection naming: `user_{user_id}_notes`
- Each document keyed by note ULID
- Embeddings regenerated on note change (queued, not blocking)

---

## Storage Layout

```
/var/seam/                          # server root (configurable)
  seam-server.yaml                  # server-level config (port, ollama URL, etc.)
  server.db                         # shared SQLite: user accounts, sessions
  chroma/                           # ChromaDB data directory (server mode)
  users/
    {user_id}/
      notes/                        # markdown files, user-accessible on filesystem
        inbox/                      # unsorted captures
        {project-slug}/             # one directory per project
      seam.db                       # per-user SQLite (metadata, FTS, links)
      config.yaml                   # per-user config overrides (model prefs, etc.)
```

Users can browse and edit their notes directly at `/var/seam/users/{user_id}/notes/` with any text editor. The file watcher detects external changes and re-indexes automatically.

---

## Screens

| Screen | Web | TUI |
|---|---|---|
| **Sidebar / Navigation** | Projects list, Inbox, global search bar, quick-capture button | Project list, Inbox, search |
| **Project view** | All notes in a project, sorted by date or relevance | Note list with preview |
| **Note editor** | Split markdown/preview, backlinks panel, related notes panel | Full-screen editor, tab to switch panels |
| **Graph view** | Interactive knowledge graph (Cytoscape.js), filterable, click to open | Deferred (not in TUI MVP) |
| **Timeline view** | Calendar-style, notes by date | Date-grouped list |
| **Ask Seam** | Conversational chat interface over your personal notes | Chat-style input/output |
| **Quick-capture** | Modal overlay — type and dismiss | Inline capture prompt |

---

## Tech Stack

| Layer | Choice | Reason |
|---|---|---|
| **Backend** | Go + `net/http` + chi router | Strong concurrency, low resource usage, single binary deployment. AI calls are HTTP to Ollama — no need for Python AI ecosystem. |
| **Storage** | Markdown files on disk | Portable, human-readable, user-accessible. Source of truth for content. |
| **Metadata index** | SQLite (per-user `.db`) via `modernc.org/sqlite` | ACID transactions, concurrent reads, proper indexing, zero infrastructure. Pure Go driver, no CGO. |
| **Full-text search** | SQLite FTS5 | Comes free with SQLite. Fast ranking, no additional dependencies. |
| **Vector store** | ChromaDB (client-server mode) | Lightweight, per-user collections, HTTP API callable from Go. |
| **LLM / AI** | Ollama (local) | 100% private, no API costs. HTTP API, language-agnostic. |
| **TUI** | Bubble Tea (Charm) | Elm architecture, mature Go TUI framework. |
| **Frontend** | React + CodeMirror | Best markdown editing experience for web. |
| **Graph view** | Cytoscape.js | Built-in zoom/pan/filter/click. Faster to ship than D3. |
| **Auth** | JWT + bcrypt | Stateless tokens, mobile-ready. User credentials in server-level SQLite. |
| **File watching** | `fsnotify` | Detects external edits to `.md` files, triggers re-indexing. |

### Why Go over Python

The original plan proposed Python/FastAPI. Go is a better fit here because:
- **Concurrency:** Goroutines handle multi-user request serving and background task processing natively. No async/await pitfalls.
- **Single binary:** Deploy one binary. No virtualenvs, no dependency management on the server.
- **Resource efficiency:** Lower memory footprint per connection matters when multiple users share one machine.
- **AI is over HTTP:** All Ollama and ChromaDB interactions are HTTP calls. The Python AI ecosystem advantage disappears when the AI runs as a separate service.

---

## AI Models (via Ollama)

| Role | Model | Notes |
|---|---|---|
| **Embeddings** | `qwen3-embedding:8b` | Top-ranked on MTEB multilingual leaderboard |
| **LLM — background** | `qwen3:32b` | Synthesis, auto-linking, summarization. Queued, not interactive. |
| **LLM — chat** | `qwen3:32b` | Interactive "Ask Seam" responses. Priority over background tasks. |
| **Transcription** | `whisper` | Voice to text, runs fully locally via Ollama. |

### AI task queue

All AI operations go through a central task queue to prevent Ollama contention across users:

- **Priority levels:** `interactive` (chat, real-time search) > `user-triggered` (explicit summarize/synthesize) > `background` (auto-link suggestions, embedding generation)
- **Fair scheduling:** Round-robin across users within each priority level. One user's bulk re-embedding does not starve another user's chat.
- **Implementation:** In-process Go channel-based queue (sufficient for 2-5 users). No Redis or external queue needed.
- **Status updates:** WebSocket push to clients for task progress (embedding progress, synthesis completion).

### Config

```yaml
# seam-server.yaml (server-level)
listen: :8080
data_dir: /var/seam
ollama_base_url: http://localhost:11434

models:
  embeddings: qwen3-embedding:8b
  background: qwen3:32b
  chat: qwen3:32b
  transcription: whisper

# Per-user overrides in /var/seam/users/{id}/config.yaml
# Users can override model choices if desired
```

Model names are config values, never hardcoded — swap based on hardware with no code changes.

---

## API Design

### REST API

Standard REST for CRUD operations:

```
POST   /api/auth/register
POST   /api/auth/login
POST   /api/auth/refresh
POST   /api/auth/logout

GET    /api/notes                    # list (filterable by project, tag, date)
POST   /api/notes                    # create
GET    /api/notes/{id}               # read
PUT    /api/notes/{id}               # update
DELETE /api/notes/{id}               # delete
GET    /api/notes/{id}/backlinks     # notes that link to this note
GET    /api/notes/{id}/related       # semantically similar notes
POST   /api/notes/{id}/ai-assist    # AI writing assist (expand, summarize, extract actions)

GET    /api/projects
POST   /api/projects
GET    /api/projects/{id}
PUT    /api/projects/{id}
DELETE /api/projects/{id}

GET    /api/search?q=...             # full-text search
GET    /api/search/semantic?q=...    # semantic search

POST   /api/capture                  # quick capture (text, URL, or voice)
POST   /api/synthesize               # AI synthesis over a scope

GET    /api/graph                    # node/edge data for graph view
GET    /api/tags                     # all tags with counts
GET    /api/templates                # list available note templates
```

### WebSocket

```
/ws                                  # authenticated connection per user
```

Events pushed to client:
- `task.progress` — AI task status updates (embedding, synthesis)
- `task.complete` — AI task finished, includes result
- `note.changed` — external file change detected by watcher
- `chat.stream` — streaming tokens for Ask Seam responses

### Auth flow

1. User registers or logs in via REST, receives JWT access token + refresh token
2. Access token included in `Authorization: Bearer` header for REST calls
3. Access token sent as first message on WebSocket connection
4. TUI stores token in OS keychain or config file
5. User credentials stored in `server.db` (shared SQLite), passwords hashed with bcrypt

---

## File Watching and Reconciliation

The backend runs an `fsnotify` watcher on each active user's notes directory. When a `.md` file is created, modified, or deleted externally:

1. **Parse** the file's YAML frontmatter and body
2. **Update** the SQLite metadata (title, project, tags, modified timestamp)
3. **Re-parse** `[[wikilinks]]` from the body, update the `links` table
4. **Update** the FTS5 index with new content
5. **Queue** embedding regeneration (background priority)
6. **Notify** connected clients via WebSocket (`note.changed` event)

On server startup, a full reconciliation scan runs: compare file mtimes against SQLite `modified` timestamps, re-index anything that changed while the server was down.

---

## Build Phases

### Phase 1 — Core (Weeks 1-3)

Backend:
- Go project scaffolding (chi router, middleware, config loading)
- SQLite setup: schema, migrations, per-user database creation
- User registration and login (JWT + bcrypt, server.db)
- Note CRUD API (create, read, update, delete)
- Projects and Inbox
- `[[wikilinks]]` parsing and link graph storage
- Tags (inline `#tag` + YAML frontmatter)
- Full-text search (SQLite FTS5)
- File watching (`fsnotify`) and reconciliation
- WebSocket connection management

TUI (parallel):
- Bubble Tea app scaffold
- Login / auth flow
- Note list and project navigation
- Note editor (markdown, full-screen)
- Quick capture
- Search

Web (parallel):
- React app scaffold
- Login / auth flow
- Sidebar navigation (projects, inbox, search)
- Note editor (CodeMirror, split view markdown/preview)
- Quick capture modal

### Phase 2 — Intelligence (Weeks 4-6)

- ChromaDB setup (client-server mode, per-user collections)
- Embeddings pipeline (generate on note save/change, queue-based)
- Semantic search API
- Related notes panel (web + TUI)
- AI synthesis ("summarize project X")
- Ask Seam — streaming chat over notes (WebSocket)
- AI auto-link suggestions on save
- AI task queue with priority and fair scheduling

### Phase 3 — Rich Capture (Weeks 7-8)

- Voice capture: Whisper transcription via Ollama, auto-summarized note
- URL paste: fetch page title + content, save as note with source link
- Templates system (project kick-off, meeting notes, research summary, daily log)
- AI writing assist (expand, summarize, extract actions)

### Phase 4 — Visualization (Weeks 9-10)

- Knowledge graph view (Cytoscape.js, web only)
- Timeline view (web + TUI)
- Backlinks panel refinement
- Graph filtering by project / tag / date range

---

## Out of Scope (Explicitly Deferred)

- Mobile apps (Android / iOS) — API is ready, clients are not
- Web clipper browser extension
- Screenshot OCR
- File sync / cloud backup
- Email-to-note
- Real-time collaboration (multi-user editing of the same note)
- Version history / git integration
- Plugin system
- Light theme (CSS variables are in place, just no design yet)

---

*Last updated: 2026-03-08*
