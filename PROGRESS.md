# Seam -- Implementation Progress

Task-level tracking for each phase defined in [IMP_PLAN.md](./IMP_PLAN.md).

Legend: Done / Partial / Not started

---

## Phase 1 -- Core (Weeks 1-3): **Done** (all 26 known gaps fixed)

### Week 1: Foundation

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 1.1 | Project scaffolding | Done | Makefile, go.mod, directory structure, two binaries |
| 1.2 | Config loading (`internal/config`) | Done | YAML + env overrides + defaults, required field validation, optional field warnings, tests pass. |
| 1.3 | Server.db setup (`internal/auth/store.go`) | Done | Migrations, user + refresh token CRUD, tests pass |
| 1.4 | Per-user DB manager (`internal/userdb`) | Done | Open/cache/close, migrations, eviction timer, ListUsers, tests pass |
| 1.5 | HTTP server + auth middleware (`internal/server`, `internal/auth`) | Done | chi router, JWT, register/login/refresh/logout, CORS, request ID, email format validation, tests pass. |

### Week 2: Note CRUD + Projects

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 2.1 | Frontmatter parser (`internal/note/frontmatter.go`) | Done | YAML parse/serialize, Extra field preserves unknown keys on round-trip, tests pass. |
| 2.2 | Wikilink parser + resolution (`internal/note/wikilink.go`) | Done | Regex extraction, aliases, code block exclusion, resolution by title/filename/slug, dangling link resolution with slug matching, tests pass. |
| 2.3 | Tag parser (`internal/note/tag.go`) | Done | Inline #tag + frontmatter merge, code/heading/URL exclusion, tests pass |
| 2.4 | Project CRUD (`internal/project`) | Done | Store + service + handler, slug generation, filesystem dirs, cascade delete (inbox/delete), tests pass. |
| 2.5 | Note CRUD (`internal/note`) | Done | Store + service + handler, file I/O, FTS triggers, wikilink/tag indexing, project moves, pagination (default 100, max 500), sort_dir param, Template field, frontmatter timestamps in Reindex, project re-resolution, tests pass. |
| 2.6 | Full-text search (`internal/search/fts.go`) | Done | FTS5 MATCH + bm25 + highlight/snippet, input sanitization, prefix queries, tests pass |
| 2.7 | Tags endpoint (`GET /api/tags`) | Done | Tag list with note counts, tests pass |

### Week 3: File Watching, WebSocket, Client Scaffolding

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 3.1 | File watcher (`internal/watcher`) | Done | fsnotify, debounce (configurable), self-write suppression with 2s TTL, subdirectory watching, tests pass |
| 3.2 | Startup reconciliation (`internal/watcher/reconcile.go`) | Done | Two-pass mtime-first reconciliation, deleted file detection, ULID write-back for missing IDs, tests pass. |
| 3.3 | WebSocket hub (`internal/ws`) | Done | Connection registry, user-scoped send, broadcast, auth on connect, 30s ping/pong keepalive, CloseAll for graceful shutdown, tests pass. |
| 3.4 | Wire file watcher to WebSocket | Done | Events pushed on file change with note ID resolution and created/modified/deleted change types. |
| 3.5 | TUI scaffold (`cmd/seam`) | Done | Bubble Tea, login screen, project list, note list, navigation |
| 3.6 | TUI note editor | Done | Full-screen textarea, load/save via API, Ctrl+S, Esc |
| 3.7 | TUI search | Done | `/` opens search, debounced search-as-you-type, results with snippets |
| 3.8 | React app scaffold (`web/`) | Done | Vite 7 + React 19 + TypeScript 5, CSS architecture, Zustand stores, React Router, API client with JWT auto-refresh (including searchFTS), WebSocket client with reconnect, Login/Register page. |
| 3.9 | React sidebar + project view | Done | Sidebar (wordmark, search with inline dropdown, inbox, projects, tags, user, capture button with pulse), command palette (Cmd+K), inbox page, project page, note cards, empty states. |
| 3.10 | React note editor | Done | CodeMirror 6 with custom seam dark theme, split view, functional toolbar (bold/italic/heading/link/wikilink/code/list/checklist), wikilink autocomplete on `[[`, wikilink decoration (amber/dotted underline), markdown-it preview with wikilink plugin, auto-save, right panel. |
| 3.11 | React quick capture | Done | Modal with backdrop blur, title + body inputs, project selector, tag input, Cmd+Enter save, toast notifications, Escape confirmation when content entered. |

---

## Phase 1 -- Known Gaps (all 26 resolved)

All 26 gaps identified during Phase 1 implementation have been fixed.

| Gap ID | Description | Severity | Resolution |
|--------|-------------|----------|------------|
| 1.2-A | `listen` not validated as required | Minor polish | Added to `validate()` |
| 1.2-B | `ollama_base_url` not validated as required | Minor polish | Added to `validate()` |
| 1.2-C | No warnings for optional missing fields | Minor polish | Added `slog.Warn` for `chromadb_url` and `models.transcription` |
| 1.5-B | No email format validation on register | Minor polish | Added `isValidEmail()` with `@` and domain checks |
| 2.1-A | No Extra field for unknown frontmatter | Functional | Custom YAML marshal/unmarshal preserving unknown keys |
| 2.2-A | Wikilink resolution missing slug matching | Functional | Added step 3 (slugified title comparison) to `ResolveLink()` |
| 2.2-B | `ResolveDanglingLinks` missing slug matching | Functional | Added slug matching to `ResolveDanglingLinks()` |
| 2.4-A | Project delete cascade incomplete | Functional | Full cascade: inbox moves files + nulls project_id; delete removes files + DB rows |
| 2.5-A | No max 500 limit enforcement | Functional | Capped at 500 in handler |
| 2.5-B | Sort direction param mismatch | Functional | Handler accepts both `dir` and `sort_dir` |
| 2.5-C | No default limit of 100 | Functional | Default limit 100 when param absent |
| 2.5-D | `CreateNoteReq` missing Template field | Minor polish | Added `Template string` field |
| 2.5-E | Reindex ignores frontmatter timestamps | Functional | Uses `fm.Created`/`fm.Modified` when present |
| 2.5-F | Reindex skips project re-resolution | Functional | Re-resolves project slug from frontmatter on content change |
| 3.2-A | No two-pass mtime-first reconciliation | Minor polish | Two-pass: mtime check first, then content hash for changed files |
| 3.2-B | Reconciliation misses deleted files | Functional | DB-to-disk comparison detects and removes orphaned rows |
| 3.2-C | Reindex does not write ULID back to file | Functional | Writes ULID to frontmatter when missing |
| 3.3-A | No WebSocket ping/pong keepalive | Functional | 30s interval ping loop goroutine |
| 3.4-A | note.changed event wrong ID and type | Functional | Resolves actual note ID, detects created/modified/deleted |
| X-5 | No WebSocket close frames on shutdown | Minor polish | `Hub.CloseAll()` sends close frames |
| 3.8-C | `searchFTS()` bypasses JWT interceptor | Functional | Added 401 handling with token refresh |
| 3.9-B | No sidebar search inline dropdown | Functional | Debounced search-as-you-type, 5 results, keyboard navigation |
| 3.10-A | No wikilink autocomplete | Missing feature | `@codemirror/autocomplete` extension triggers on `[[` |
| 3.10-B | No wikilink decoration | Missing feature | `ViewPlugin` with `MatchDecorator` for amber/dotted underline |
| 3.10-C | Toolbar buttons non-functional | Functional | All 8 buttons wired with formatting functions |
| 3.11-A | No Escape confirmation in capture modal | Functional | `window.confirm()` when content present |

---

### Test summary

- Go: 12 packages tested, all passing
  - `internal/ai` (41 tests): Ollama client, ChromaDB client, task store, queue (10 tests incl. fair scheduling, context cancellation), embedder, chat, synthesizer, linker, handler
  - `internal/search` (20 tests): FTS store (7), sanitize (9), handler HTTP tests (8), semantic search (2), snippet extraction (4)
  - `internal/auth`, `internal/config`, `internal/note`, `internal/project`, `internal/server`, `internal/userdb`, `internal/watcher`, `internal/ws`: all passing
- Frontend: 13 test files, 113 tests, all passing
  - `api/client.test.ts` (22 tests): token management, register, login, logout, CRUD, search, searchSemantic, askSeam, synthesize, 401 handling
  - `stores/authStore.test.ts` (6 tests): state management, login/register/logout flows
  - `stores/noteStore.test.ts` (7 tests): CRUD, backlinks, state updates
  - `stores/projectStore.test.ts` (5 tests): CRUD, state updates
  - `lib/markdown.test.ts` (7 tests): rendering, wikilinks, aliases
  - `lib/tagColor.test.ts` (6 tests): deterministic hashing, project colors
  - `lib/dates.test.ts` (4 tests): date formatting
  - `pages/Login/LoginPage.test.tsx` (8 tests): rendering, form interaction, mode toggle
  - `pages/Search/SearchPage.test.tsx` (10 tests): rendering, fulltext/semantic toggle, debounced search, result navigation, empty states
  - `pages/Ask/AskPage.test.tsx` (12 tests): rendering, message display, streaming state, input behavior, empty state
  - `components/SynthesisModal/SynthesisModal.test.tsx` (14 tests): rendering, generate flow, loading/error states, backdrop click, keyboard interaction
  - `components/NoteCard/NoteCard.test.tsx` (9 tests): rendering, navigation, markdown stripping
  - `components/EmptyState/EmptyState.test.tsx` (3 tests): rendering, action button

---

## Phase 2 -- Intelligence (Weeks 4-6): **Done** (all 15 tasks completed, all gaps resolved)

### Week 4: Embeddings + Semantic Search

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 4.1 | Ollama HTTP client (`internal/ai/ollama.go`) | Done | `GenerateEmbedding`, `ChatCompletion`, `ChatCompletionStream`, configurable timeouts, 5 tests |
| 4.2 | ChromaDB HTTP client (`internal/ai/chroma.go`) | Done | v2 API, `GetOrCreateCollection`, `AddDocuments`, `Query`, `UpsertDocuments`, `DeleteDocuments`, 6 tests |
| 4.3 | AI task queue (`internal/ai/queue.go`) | Done | Priority queue with `container/heap`, task persistence in `ai_tasks` table, WS status push, `LoadPending` for startup recovery, 3 tests |
| 4.4 | Embedding pipeline (`internal/ai/embedder.go`) | Done | Paragraph-aware text chunking with overlap, per-note embedding, task handlers for `embed` and `delete_embed`, 4 tests |
| 4.5 | Semantic search (`internal/search/semantic.go`) | Done | Query embedding, ChromaDB nearest-neighbor, per-note deduplication, snippet extraction, `GET /api/search/semantic`, 3 tests |
| 4.6 | Related notes panel | Done | `GET /api/ai/notes/{id}/related`, embeds current note, queries ChromaDB excluding self, returns top-N similar notes, 5 handler tests |

### Week 5: Ask Seam + Synthesis

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 4.7 | Ask Seam -- RAG chat (`internal/ai/chat.go`) | Done | RAG with prompt construction, conversation history truncation, sync + streaming (WS `chat.ask`/`chat.stream`/`chat.done`), 4 tests |
| 4.8 | AI synthesis (`internal/ai/synthesizer.go`) | Done | Project-scoped and tag-scoped synthesis via `POST /api/ai/synthesize`, 6 tests |
| 4.9 | Auto-link suggestions (`internal/ai/linker.go`) | Done | Semantic similarity + LLM-based link suggestion with JSON parsing, WS `note.link_suggestions`, 3 tests |

### Week 6: Client Integration

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 4.10 | React: semantic search UI | Done | Full-text/Semantic tab toggle in SearchPage, calls `GET /api/search/semantic`, shows similarity score badges |
| 4.11 | React: Ask Seam page | Done | `/ask` route, chat interface with message bubbles, WS streaming (chat.stream/chat.done), citation links, multi-turn conversation |
| 4.12 | React: synthesis UI | Done | "Summarize" button in ProjectPage header, SynthesisModal with prompt input, rendered markdown response |
| 4.13 | React: auto-link suggestion UI | Done | WS listener for `note.link_suggestions`, dismissible panel in editor right panel, "Link" button inserts `[[wikilink]]`, related notes section |
| 4.14 | TUI: semantic search | Done | `?` prefix in search triggers semantic mode, shows similarity % scores, mode indicator in header |
| 4.15 | TUI: Ask Seam | Done | `a` key from main screen, full chat model with textarea input, scrollable conversation, multi-turn history |

### Server wiring

- `cmd/seamd/main.go`: Creates all AI components, registers task handlers, hooks embedding into watcher callback, starts queue workers
- `internal/server/server.go`: Mounts `/api/ai/*` routes, `AIHandler` and `WSMessageHandler` config fields
- `internal/ws/client.go`: `MessageHandler` callback for processing incoming WS messages (e.g., `chat.ask`)

## Phase 2 -- Known Gaps (all resolved)

All gaps identified during Phase 2 have been fixed.

| Gap ID | Description | Severity | Resolution |
|--------|-------------|----------|------------|
| 4.3-A | Fair scheduling in queue | Functional | Added `lastUserByPr` map to `dequeue()` for round-robin across users within each priority level |
| 4.3-B | No `task.failed` WS message type | Minor polish | Added `MsgTypeTaskFailed` to `protocol.go`, updated `sendEvent` to emit it on task failure |
| 4.3-C | Queue tests use `time.Sleep` | Test quality | Rewrote `queue_test.go` with channels and `sync.WaitGroup` -- zero `time.Sleep` |
| 4.3-D | Queue tests lack coverage | Test gap | Added 7 new tests: FairScheduling, FailedTask, NoHandler, ContextCancellation, LoadPending, EnqueueAssignsDefaults, and more (10 total) |
| 4.4-A | No batch reindex endpoint | Functional | Added `ReindexAll` to `Embedder`, `POST /api/ai/reindex-embeddings` handler |
| 4.4-B | Hardcoded 100 chunk limit | Edge case | Changed to 500 with `maxChunksPerNote` constant |
| 4.8-A | Fake streaming in SynthesizeStream | Minor polish | Replaced with true Ollama streaming via `ChatCompletionStream` |
| 4.9-A | `note.link_suggestions` never sent | Functional | Added `ws.Hub` to `AutoLinker`, sends `MsgTypeLinkSuggestions` in `HandleAutolinkTask` |
| 4.11-A | Ask Seam plain text responses | Functional | Added `renderMarkdown` + `dangerouslySetInnerHTML` for assistant messages, CSS for markdown |
| 4.12-A | No tag-scoped synthesis trigger | Minor polish | Added Summarize button + SynthesisModal to InboxPage when tag filter is active |
| 4.15-A | TUI Ask Seam no WS streaming | Functional | Rewrote `ask.go` with `askViaWebSocket` using `coder/websocket`, with HTTP fallback |
| 4.2-A | Dead code `ErrCollectionNotFound` | Dead code | Removed unused error variable |
| 4.5-A | No search handler HTTP tests | Test gap | Added `handler_test.go` with 8 tests covering FTS + semantic search endpoints |
| 4.5-C | Service in handler.go | Convention | Created `internal/search/service.go`, moved `Service` type out of handler.go |
| 4.X-A | No frontend tests for Phase 2 | Test gap | Added `SearchPage.test.tsx` (10 tests), `AskPage.test.tsx` (12 tests), `SynthesisModal.test.tsx` (14 tests), and 7 new API client tests |

### Remaining design choices (not bugs)

| ID | Description | Notes |
|----|-------------|-------|
| 4.5-B | No offset/pagination for semantic search | Design choice -- ChromaDB returns relevance-ranked results; offset is not meaningful |
| 4.X-B | No dedicated Zustand store for AI/chat state | Design choice -- chat history is local component state; acceptable for current feature scope |

---

## Phase 3 -- Rich Capture (Weeks 7-8): Not started

| Task | Description | Status |
|------|-------------|--------|
| 5.1 | URL capture (`internal/capture/url.go`) | Not started |
| 5.2 | Voice capture (`internal/capture/voice.go`) | Not started |
| 5.3 | React + TUI: capture integration | Not started |
| 5.4 | Templates system (`internal/template`) | Not started |
| 5.5 | AI writing assist (`internal/ai/writer.go`) | Not started |
| 5.6 | React + TUI: templates and AI assist | Not started |

## Phase 4 -- Visualization (Weeks 9-10): Not started

| Task | Description | Status |
|------|-------------|--------|
| 6.1 | Graph data endpoint (`internal/graph`) | Not started |
| 6.2 | React: knowledge graph view | Not started |
| 6.3 | React: timeline view | Not started |
| 6.4 | TUI: timeline view | Not started |
| 6.5 | Backlinks panel refinement | Not started |
| 6.6 | End-to-end testing and polish | Not started |

---

*Last updated: 2026-03-08 (Phase 2 complete -- all gaps resolved)*
