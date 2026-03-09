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

- Go: 15 packages tested, all passing
  - `internal/ai` (49 tests): Ollama client, ChromaDB client, task store, queue (10 tests incl. fair scheduling, context cancellation), embedder, chat, synthesizer, linker, handler (incl. 5 assist handler tests), writer
  - `internal/capture` (35 tests): URL fetcher (17 incl. og:title, encoding, redirect, timeout), SSRF (4 functions / 20 subcases), voice (7), handler (7)
  - `internal/graph` (20 tests): service (13), handler (7)
  - `internal/search` (20 tests): FTS store (7), sanitize (9), handler HTTP tests (8), semantic search (2), snippet extraction (4)
  - `internal/template` (17 tests): service (10), handler (7)
  - `internal/auth`, `internal/config`, `internal/note`, `internal/project`, `internal/server`, `internal/userdb`, `internal/watcher`, `internal/ws`: all passing
- Frontend: 18 test files, 153 tests, all passing
  - `api/client.test.ts` (36 tests): token management, register, login, logout, CRUD, search, searchSemantic, askSeam, synthesize, captureURL, listTemplates, applyTemplate, aiAssist, getGraph, getTwoHopBacklinks, getOrphanNotes, 401 handling
  - `api/captureVoice.test.ts` (4 tests): multipart form, auth header, 401 retry, error handling
  - `stores/authStore.test.ts` (6 tests): state management, login/register/logout flows
  - `stores/noteStore.test.ts` (7 tests): CRUD, backlinks, state updates
  - `stores/projectStore.test.ts` (5 tests): CRUD, state updates
  - `lib/markdown.test.ts` (7 tests): rendering, wikilinks, aliases
  - `lib/tagColor.test.ts` (6 tests): deterministic hashing, project colors
  - `lib/dates.test.ts` (4 tests): date formatting
  - `pages/Login/LoginPage.test.tsx` (8 tests): rendering, form interaction, mode toggle
  - `pages/Search/SearchPage.test.tsx` (10 tests): rendering, fulltext/semantic toggle, debounced search, result navigation, empty states
  - `pages/Ask/AskPage.test.tsx` (12 tests): rendering, message display, streaming state, input behavior, empty state
  - `pages/NoteEditor/NoteEditorPage.test.tsx` (3 tests): AI assist button, dropdown, no-note state
  - `pages/Graph/GraphPage.test.tsx` (5 tests): loading, empty, filter panel, reset button, API call
  - `pages/Timeline/TimelinePage.test.tsx` (8 tests): loading, empty, date groups, title, toggle, sort switch, tags, date picker
  - `components/SynthesisModal/SynthesisModal.test.tsx` (14 tests): rendering, generate flow, loading/error states, backdrop click, keyboard interaction
  - `components/Modal/CaptureModal.test.tsx` (6 tests): render states, URL mode, template picker, cancel, disabled save
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

## Phase 3 -- Rich Capture (Weeks 7-8): **Done** (all 6 tasks completed, all gaps resolved)

### Week 7: Voice + URL Capture

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 5.1 | URL capture (`internal/capture/url.go`) | Done | SSRF-safe URL fetcher, HTML parsing via `golang.org/x/net/html`, og:title fallback, charset detection via `x/net/html/charset`, title/article/body extraction, `POST /api/capture` with `{"type":"url"}`, 21 unit tests + 7 handler tests |
| 5.2 | Voice capture (`internal/capture/voice.go`) | Done | Whisper transcription via Ollama `/v1/audio/transcriptions`, multipart upload, creates note with `transcript_source: true` persisted through DB and file, background summarization via AI task queue, 7 tests |
| 5.3 | React + TUI: capture integration | Done | React: URL paste detection in CaptureModal (`isURL()` auto-triggers capture endpoint), voice recording via MediaRecorder API with mic button. TUI: `u` key opens URL capture modal, `v` key opens voice capture modal (records via sox/arecord/ffmpeg) |

### Week 8: Templates + AI Writing Assist

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 5.4 | Templates system (`internal/template`) | Done | Shared + per-user template dirs, 4 defaults (meeting-notes, project-kickoff, research-summary, daily-log), `{{var}}` substitution with builtins (date/time/year/month/day), `GET /api/templates`, `POST /api/templates/{name}/apply`, 10 service tests + 7 handler tests |
| 5.5 | AI writing assist (`internal/ai/writer.go`) | Done | expand/summarize/extract-actions via Ollama ChatCompletion, loads note body via NoteBodyLoader interface (respects layering), `POST /api/ai/notes/{id}/assist` with `errors.Is` for proper error mapping, 7 unit tests + 5 handler tests |
| 5.6 | React + TUI: templates and AI assist | Done | React: template picker in CaptureModal (loads templates on open, applies with vars, pre-fills body), AI assist toolbar dropdown in NoteEditorPage (Sparkles icon, expand/summarize/extract-actions, result preview with Insert/Dismiss). TUI: `n` key opens template picker (list/select/title phases), `Ctrl+A` in editor opens AI assist command palette (3 actions, result preview with insert/dismiss) |

### Server wiring

- `internal/server/server.go`: Added `CaptureHandler` + `TemplateHandler` to Config, mounted routes at `/api/capture` and `/api/templates`
- `cmd/seamd/main.go`: Creates capture service with SSRF-safe fetcher, voice transcriber conditional on `models.transcription`, template service with `EnsureDefaults()`, AI writer with NoteBodyLoader/Updater adapters, wires template applier to note handler, wires summarize callback to capture service
- `internal/ai/handler.go`: Added `writer` field, `POST /api/ai/notes/{id}/assist` route, uses `errors.Is` for proper error matching

### Test summary

- Go: All existing tests pass, plus new/expanded packages:
  - `internal/capture` (35 tests): URL fetcher (17 incl. og:title, encoding, redirect, timeout, large page), SSRF security (4 test functions / 20 subcases), voice transcriber (7 incl. empty audio, summarize callback), handler (7)
  - `internal/template` (17 tests): service (10), handler (7)
  - `internal/ai` (49 tests): writer (7), handler assist tests (5: success, missing action, invalid action, note not found, writer nil), plus all existing tests
- Frontend: 136 tests passing (123 existing + 13 new)
  - `api/client.test.ts`: captureURL (2), listTemplates (1), applyTemplate (3), aiAssist (4)
  - `api/captureVoice.test.ts` (4 tests): multipart form, auth header, 401 retry, error handling
  - `components/Modal/CaptureModal.test.tsx` (6 tests): render states, URL mode, template picker, cancel, disabled save
  - `pages/NoteEditor/NoteEditorPage.test.tsx` (3 tests): AI assist button, dropdown, no-note state

### TUI additions

- `cmd/seam/client.go`: Added `CaptureURL`, `ListTemplates`, `ApplyTemplate`, `Assist` API methods
- `cmd/seam/url_capture.go`: URL capture modal (`u` key), text input for URL, Enter to fetch
- `cmd/seam/voice_capture.go`: Voice capture modal (`v` key), records via sox/arecord/ffmpeg, uploads multipart
- `cmd/seam/template_picker.go`: Template picker modal (`n` key), three phases (loading/list/title), applies template and creates note
- `cmd/seam/ai_assist.go`: AI assist command palette (`Ctrl+A` in editor), three actions, result preview with insert/dismiss

## Phase 3 -- Known Gaps (all resolved)

All gaps identified during Phase 3 have been fixed.

| Gap ID | Description | Severity | Resolution |
|--------|-------------|----------|------------|
| 5.2-A | `TranscriptSource` never persisted | High | Added `TranscriptSource` field to `CreateNoteReq`, `note.Service.Create` now honors `req.TranscriptSource` in frontmatter and DB row. `capture.Service.CaptureVoice` passes `TranscriptSource: true` through the request instead of setting it post-create. |
| 5.5-A | `errors.Is` vs `==` in assist handler | High | Changed bare `==` comparisons to `errors.Is()` for `ErrInvalidAction` and `ErrEmptyInput` in `internal/ai/handler.go`. Added `errors` import. Wrapped errors now correctly match, returning 400 instead of falling through to 500. |
| 5.4-A | `CreateNoteReq.Template` field is dead code | Medium | Added `TemplateApplier` interface to note handler, wired `template.Service` via `noteHandler.SetTemplateApplier(templateSvc)` in `main.go`. Handler's `create` method now applies the template to pre-fill body when `req.Template` is set and body is empty. |
| 5.1-A | No `og:title` fallback | Medium | Added `extractOGTitle()` function that parses `<meta property="og:title" content="...">` tags. `FetchURL` now checks og:title when `<title>` is empty, falling back to hostname only if both are absent. |
| 5.2-B | No background summarization for voice captures | Low | Added `SummarizeFunc` callback to capture `Service`, `TaskTypeSummarizeTranscript` constant, `SummarizeTranscriptPayload` type, and `Writer.HandleSummarizeTranscriptTask` handler. Wired in `main.go`: capture service enqueues a background task that reads the note body, generates a 2-4 sentence summary via LLM, and prepends it. |
| 5.1-B | No charset detection / encoding conversion | Low | Added `golang.org/x/net/html/charset` import. `FetchURL` now calls `charset.NewReader()` with the Content-Type header before parsing, automatically converting Latin-1, Windows-1252, and other encodings to UTF-8. Falls back to raw reader if detection fails. |
| 5.X-A3 | AI Writer bypasses note.Service, queries DB directly | Low | Added `NoteBodyLoader` and `NoteBodyUpdater` interfaces to the writer. Created `noteBodyAdapter` in `main.go` that uses `note.SQLStore.Get` via the proper layering. Writer uses the interfaces when set, with fallback to direct DB query for backward compatibility. |
| 5.1-T | Missing URL capture tests (10) | Test gap | Added 10 tests: `ExtractTitleFromOG`, `ExtractOGTitle`, `StripHTML`, `Timeout`, `LargePage`, `EncodingUTF8`, `EncodingLatin1`, `RedirectFollowed`, `PrivateIP_Handler`, `OGTitlePreference`. |
| 5.2-T | Missing voice capture tests (3) | Test gap | Added 3 tests: `Transcribe_EmptyAudio`, `Service_SetSummarizeFunc`, `Handler_VoiceCapture_TranscriberNotConfigured`. |
| 5.5-T | Missing AI assist handler tests (5) | Test gap | Added 5 handler tests: `Assist_Success`, `Assist_MissingAction`, `Assist_InvalidAction`, `Assist_NoteNotFound`, `Assist_WriterNil`. Created `setupTestHandlerWithWriter` helper. |
| 5.X-T1 | Missing SSRF security tests (4) | Test gap | Added 4 test functions: `SSRF_MetadataEndpoint`, `SSRF_IPv6Loopback`, `SSRF_UnsafeSchemes` (ftp/javascript/data/gopher), `SSRF_PrivateRanges_Comprehensive` (16 cases covering all private ranges and boundary IPs). |
| 5.X-T2 | Missing frontend component tests (3 files) | Test gap | Added `captureVoice.test.ts` (4 tests), `CaptureModal.test.tsx` (6 tests), `NoteEditorPage.test.tsx` (3 tests). |

### Remaining design choices (not bugs)

| ID | Description | Notes |
|----|-------------|-------|
| 5.X-A1 | Capture service uses concrete types, not interfaces | Design choice -- could extract interface later for better testability |
| 5.X-A2 | Template service uses concrete types, not interfaces | Design choice -- could extract interface later for better testability |

---

## Phase 4 -- Visualization (Weeks 9-10): **Done** (all 6 tasks completed)

### Week 9: Graph + Timeline

| Task | Description | Status | Notes |
|------|-------------|--------|-------|
| 6.1 | Graph data endpoint (`internal/graph`) | Done | `graph.go` (types: Node, Edge, Graph, GraphFilter), `service.go` (GetGraph, GetTwoHopBacklinks, GetOrphanNotes), `handler.go` (GET /api/graph, GET /api/graph/two-hop-backlinks/{id}, GET /api/graph/orphans). Wired in `server.go` + `main.go`. 13 service tests + 7 handler tests, all passing. |
| 6.2 | React: knowledge graph view | Done | `/graph` route, Cytoscape.js with fcose layout, project-colored nodes sized by link count, edge highlighting on hover, double-click navigation to note, filter panel (project checkboxes, tag pills, reset button), dot-grid background. `GraphPage.test.tsx` (5 tests). |
| 6.3 | React: timeline view | Done | `/timeline` route, date-grouped note list with created/modified toggle, date picker for jump-to-date, sticky date markers, today indicator dot, tag display on notes. `TimelinePage.test.tsx` (8 tests). |
| 6.4 | TUI: timeline view | Done | `t` key from main screen, date navigation with `[`/`]`, note selection with `j`/`k`, `s` to toggle created/modified sort, `enter` to open note, `q`/`esc` to return. |
| 6.5 | Backlinks panel refinement | Done | Two-hop backlinks section in NoteEditorPage right panel via `GET /api/graph/two-hop-backlinks/{id}`, orphan detection via `GET /api/graph/orphans`. |
| 6.6 | End-to-end testing and polish | Done | All Go tests passing (15 packages), all frontend tests passing (18 test files, 153 tests). Fixed test mocks for `getTwoHopBacklinks`, `window.matchMedia`, and date picker rendering. |

### Frontend integration

- `web/src/api/types.ts`: Added `GraphNode`, `GraphEdge`, `GraphData`, `GraphFilter`, `TwoHopBacklink` types
- `web/src/api/client.ts`: Added `getGraph()`, `getTwoHopBacklinks()`, `getOrphanNotes()` API methods
- `web/src/App.tsx`: Added `/graph` and `/timeline` routes with lazy loading
- `web/src/components/Sidebar/Sidebar.tsx`: Added Network (graph) and Calendar (timeline) nav items
- `web/src/components/CommandPalette/CommandPalette.tsx`: Added Timeline command entry
- `web/package.json`: Added `cytoscape`, `cytoscape-fcose`, `@types/cytoscape` dependencies

### TUI additions

- `cmd/seam/timeline.go`: Timeline view model with date grouping, sort toggle, date navigation
- `cmd/seam/app.go`: Added `screenTimeline`, `updateTimeline`, `openTimelineMsg`
- `cmd/seam/main_screen.go`: Added `t` key binding for timeline

### Test summary

- Go: 15 packages tested, all passing
  - `internal/graph` (20 tests): service (13: GetGraph with various filter combos, GetTwoHopBacklinks, GetOrphanNotes, empty DB), handler (7: GET /graph, /graph/two-hop-backlinks/{id}, /graph/orphans, project/tag filters, error cases)
  - All existing packages continue to pass
- Frontend: 18 test files, 153 tests, all passing
  - `pages/Graph/GraphPage.test.tsx` (5 tests): loading, empty, filter panel, reset button, API call
  - `pages/Timeline/TimelinePage.test.tsx` (8 tests): loading, empty, date groups, title, toggle, sort switch, tags, date picker
  - `api/client.test.ts` (36 tests): +4 graph API tests (getGraph, getTwoHopBacklinks, getOrphanNotes, getGraph with filter)
  - All existing test files continue to pass

## Phase 4 -- Known Gaps (all 20 resolved)

All gaps identified during Phase 4 have been fixed.

| Gap ID | Description | Severity | Resolution |
|--------|-------------|----------|------------|
| 6.1-A | N+1 query for node tags | Performance | Replaced per-node `loadNodeTags` with `loadAllNodeTags` batch query using `WHERE nt.note_id IN (...)`. Single query for all nodes. |
| 6.1-B | `queryEdges` scans all links, filters in Go | Performance | Rewrote `queryEdges` to use `WHERE source_note_id IN (...) AND target_note_id IN (...)` for SQL-level filtering. No more full-table scan. |
| 6.1-C | Node type missing `project` (name) field | Spec mismatch | Added `ProjectName` field to `Node` (`json:"project"`). `queryNodes` now uses `LEFT JOIN projects p ON p.id = n.project_id` to include human-readable name. Added `project` field to frontend `GraphNode` type. |
| 6.1-D | No max limit cap on `?limit=` parameter | Functional | Added `if filter.Limit > 500 { filter.Limit = 500 }` in handler. Added `TestHandler_GetGraph_LimitCap` test. |
| 6.1-E | Two-hop backlinks missing `IS NOT NULL` filters | Edge case | Added explicit `AND l1.target_note_id IS NOT NULL` and `AND l2.target_note_id IS NOT NULL` to JOIN conditions, and `AND target_note_id IS NOT NULL` to the exclusion subquery. |
| 6.1-F | `getGraph` URL trailing slash | Minor | Fixed `client.ts` from `/graph/${qs}` to `/graph${qs}` -- no trailing slash when no query params. |
| 6.2-A | No minimap | Missing feature | Added minimap as a secondary Cytoscape instance in a 120x80px container at bottom-right. Uses preset layout with simplified node/edge styles. Added `.minimap` CSS class. |
| 6.2-B | No date range filter in filter panel | Missing feature | Added since/until date inputs to the filter panel JSX. Date changes trigger API re-fetch with `?since=` and `?until=` query params. Added `TestGraphPage_renders_date_range_inputs` test. |
| 6.2-C | Font size hardcoded `10px` | Minor polish | Changed node label font-size from `10px` to `12px` (matching `--font-size-xs: 0.75rem`). Added explicit `text-halign: 'center'`. |
| 6.2-D | Selected node background color | Minor polish | Changed selected node style to use `COLORS.accentMuted` constant (`rgba(196, 145, 92, 0.10)`) matching the `--accent-muted` CSS variable. |
| 6.2-E | Hardcoded color values | Convention | Extracted all Cytoscape colors to a `COLORS` constant object at module scope, mirroring CSS variable values from `variables.css`. All style references now use `COLORS.*`. |
| 6.2-F | Tag/project filter asymmetry | Inconsistency | Unified both tag and project filtering to be client-side via Cytoscape show/hide. Tags now support multi-select (Set-based). Only date range triggers API re-fetch. Both filter types can be combined. |
| 6.2-G | No click-to-select behavior | Minor polish | Added `cy.on('tap')` handler: clicking background calls `cy.elements().unselect()`, clicking a node selects it (Cytoscape default). `node:selected` style already applies. |
| 6.3-A | Sticky date markers lack background | Minor polish | Added `background: var(--bg-base)` and `padding: var(--space-1) 0` to `.dateMarker` CSS so sticky headers are opaque and readable. |
| 6.4-A | TUI timeline missing sort/limit params | Functional | Added `ListNotesAll(sort, limit)` method to TUI API client. Timeline now calls `client.ListNotesAll(sortMode, 500)` instead of `client.ListNotes("")`. Server-side sorting and 500-note limit. Removed redundant client-side sort. |
| 6.4-B | TUI timeline no viewport scrolling | Minor polish | Added viewport-aware rendering with scroll offset calculation based on terminal height. Shows `... N more` indicator when notes overflow. |
| 6.5-A | Orphan detection not surfaced in UI | Missing feature | NoteEditorPage now calls `getOrphanNotes()` on load and displays an orphan badge ("Orphan note -- no links in or out") in the right panel when the current note is detected as an orphan. Added `.orphanBadge` CSS. |
| 6.5-B | Two-hop backlinks missing intermediate path | Minor polish | Added `TwoHopNode` type to backend with `ViaID` and `ViaTitle` fields. `GetTwoHopBacklinks` SQL now JOINs the intermediate `notes via` table. Frontend shows "via [note title]" link below each two-hop backlink. Added `.twoHopItem`, `.twoHopVia`, `.twoHopViaLink` CSS. Updated `TwoHopBacklink` frontend type. |
| 6.6-A | No end-to-end user journey test | Missing feature | Created `internal/integration/e2e_test.go` (build tag: `integration`). `TestE2E_FullUserJourney` exercises: register, login, create project, create 4 notes with wikilinks, search, get graph (verify project names and edges), get graph with project filter, get orphans (verify standalone note), get two-hop backlinks, verify tags endpoint, health check. |
| 6.6-B | No performance testing | Missing feature | Created `internal/integration/performance_test.go` (build tag: `performance`). `TestPerformance_1000Notes` creates 1000 notes with wikilinks, measures graph/orphan/search endpoint response times (must be under 5s). `TestPerformance_ConcurrentUsers` tests 3 concurrent users each creating 50 notes and fetching graphs. |
| 6.6-C | Graph command already in palette | Not a gap | Graph command already existed in `CommandPalette.tsx` (Network icon, `/graph` route). No fix needed. |

### Remaining design choices (not bugs)

| ID | Description | Notes |
|----|-------------|-------|
| 6.3-B | Controls not shown in empty state | Design choice -- no notes means nothing to toggle. Acceptable UX. |
| 6.X-B | Graph uses `fcose` without compound grouping | Design choice -- spec says "cluster by project" but fcose compound grouping requires parent nodes. Current layout clusters naturally by connectivity, not explicitly by project. |

### Test summary (updated)

- Go: 15 packages, all unit tests passing. `internal/graph`: 21 tests (+1 limit cap test)
  - Integration: `internal/integration` (build tag `integration`): 1 test (full user journey)
  - Performance: `internal/integration` (build tag `performance`): 2 tests (1000 notes, 3 concurrent users)
- Frontend: 18 test files, 154 tests, all passing (+1 date range inputs test in GraphPage)

---

## Post-Implementation Audit (2026-03-09) -- **All Critical + Medium issues resolved**

Comprehensive code review across all packages, frontend, and migrations. All 15 Critical/High and 30 Medium severity issues have been fixed. All Go tests (16 packages) and frontend tests (18 files, 154 tests) pass.

---

### Critical / High Severity (15 issues -- all resolved)

| ID | Category | Description | Resolution |
|----|----------|-------------|------------|
| A-1 | Security | **Path traversal in `Reindex`.** `filePath` joined without validation. | Created `internal/validate` package with `Path()` and `PathWithinDir()` helpers. Added validation at entry of `note.Service.Reindex` to reject `..`, absolute paths, null bytes, and verify resolved path stays within notes dir. |
| A-2 | Security | **Path traversal via `userID`.** `userID` used in filesystem paths without validation. | Added `validate.UserID()` checks in `userdb.Manager.Open` and `EnsureUserDirs`. Rejects anything except alphanumeric, hyphens, underscores (max 128 chars). |
| A-3 | Security | **SSRF DNS rebinding (TOCTOU).** DNS resolved for validation, then re-resolved by dialer. | `ssrfSafeDialer` now connects to the validated IP directly (`net.JoinHostPort(ips[0].IP.String(), port)`) instead of re-resolving the hostname. |
| A-4 | Security | **SSRF: `0.0.0.0` not blocked.** `isPrivateIP` missing `IsUnspecified()`. | Added `ip.IsUnspecified()` to `isPrivateIP` check. |
| A-5 | Security | **No input validation for note titles.** Only checked for empty. | Added `validate.Name()` in `note.Handler.create` and `update`. Rejects `/`, `\`, `..`, null bytes, length > 255. |
| A-6 | Security | **No input validation for project names.** Same as A-5. | Added `validate.Name()` in `project.Handler.create` and `update`. Same rules as A-5. |
| A-7 | Data integrity | **No DB transactions for multi-step operations.** | Defined `DBTX` interface (`ExecContext`, `QueryContext`, `QueryRowContext`, `PrepareContext`) in both `note` and `project` packages. Updated `Create`, `Update`, `Delete`, `UpdateTags`, `UpdateLinks`, `ResolveLink`, `ResolveDanglingLinks` store methods to accept `DBTX`. Wrapped all multi-step service operations in `db.BeginTx()`/`tx.Commit()`. |
| A-8 | Data integrity | **Stale tags written to frontmatter.** Old tags used when building frontmatter. | Reordered `note.Service.Update` to compute merged tags BEFORE building frontmatter. Tags are now correct in both the `.md` file and the DB. |
| A-9 | Data integrity | **Project rename leaves stale `file_path`.** | After renaming the project directory, `project.Service.Update` now executes `UPDATE notes SET file_path = newPrefix || SUBSTR(file_path, ...) WHERE project_id = ?` to update all affected paths. |
| A-10 | Concurrency | **DB handle use-after-close from eviction.** | Removed the eviction timer entirely. `Manager.Run` now simply blocks until shutdown. DB handles are only closed via `CloseAll()` during graceful shutdown. |
| A-11 | Concurrency | **Race condition in debounce timer.** | Added `debounceGen map[string]uint64` generation counter. Each `debounceEvent` increments the generation; the timer callback checks if it's still current and skips if stale. |
| A-12 | Concurrency | **One-shot self-write suppression.** Only first fsnotify event suppressed. | Changed `checkAndClearSuppression` to time-based expiry: returns true for all events within the 2-second window, only deletes the entry after expiry passes. |
| A-13 | Concurrency | **AI queue single-worker bottleneck.** `processAll()` drains queue serially. | Replaced buffered channel with `sync.Cond`. Each worker calls `waitForTask()` which uses `cond.Wait()` to block, then dequeues and processes exactly one task. Multiple workers now process tasks in parallel. |
| A-14 | Correctness | **Fair scheduling assumes heap is sorted.** | Fixed `dequeueLocked` to scan all elements at the top priority level using `continue` instead of `break` when priority differs, since heap siblings are not sorted. |
| A-15 | Correctness | **ChromaDB nil pointer panic.** | Added nil/empty checks on `result.Distances` and `result.Metadatas` outer slices before accessing index `[0]`. |

---

### Medium Severity (30 issues -- all resolved)

| ID | Category | Description | Resolution |
|----|----------|-------------|------------|
| B-1 | Security | **No request body size limit.** | Added `r.Body = http.MaxBytesReader(w, r.Body, 1<<20)` (1MB) before JSON decoding in all handlers: note, project, auth, AI. |
| B-2 | Security | **`WriteTimeout` kills WebSocket/streaming.** | Removed `WriteTimeout: 30s` from `http.Server` config. |
| B-3 | Security | **No max password length.** | Added `len(req.Password) > 1024` check in `auth.Service.Register`. |
| B-4 | Security | **No username format validation.** | Added `usernameRe` regexp (`^[a-zA-Z0-9_-]{3,64}$`) validation in Register. |
| B-5 | Security | **JWT secret too short.** | Added `len(cfg.JWTSecret) < 32` validation in `config.validate()`. |
| B-6 | Security | **Bcrypt cost out of range.** | Added `BcryptCost < 4 || > 14` validation in `config.validate()`. |
| B-7 | Security | **Error messages leak internals.** | Auth handler now uses `safeRegistrationMessage()` to map domain errors to fixed user-safe messages. Capture handler returns generic "invalid or unsafe URL". |
| B-8 | Security | **Redirect scheme not validated.** | Added scheme validation in `CheckRedirect`: only `http` and `https` allowed. Rejects `file://`, `ftp://`, etc. |
| B-9 | Security | **WebSocket origin check disabled.** | Replaced `InsecureSkipVerify: true` with `OriginPatterns: []string{"localhost:*", "127.0.0.1:*"}`. |
| B-10 | Security | **No WebSocket auth timeout.** | Added `context.WithTimeout(r.Context(), 10*time.Second)` for the auth message read. |
| B-11 | Correctness | **Content-Type mismatch in middleware.** | Replaced `http.Error()` with `writeJSONError()` that sets `Content-Type: application/json`. |
| B-12 | Correctness | **Since/Until filters vs sort column mismatch.** | `List` method now uses a `timeCol` variable matching the sort column for Since/Until filters. |
| B-13 | Correctness | **Silently discarded `time.Parse` errors.** | All 6 silent discards now log `slog.Warn` with the raw value. Zero-time fallback preserved. |
| B-14 | Correctness | **No `rows.Err()` check in synthesizer.** | Added `dbRows.Err()` checks after all four `for dbRows.Next()` loops. |
| B-15 | Architecture | **noteBodyAdapter bypasses service layer.** | `UpdateNoteBody` now calls `noteSvc.Update()` which writes both the `.md` file and DB atomically. |
| B-16 | Correctness | **Incorrect shutdown order.** | Reversed order: HTTP server stops first (`srv.Shutdown`), then WebSocket connections are closed (`hub.CloseAll()`). |
| B-17 | Resource leak | **Refresh token accumulation.** | Added `DeleteOldestTokensForUser()` to auth store. Called in `generateTokenPair` with cap of 10 tokens per user. |
| B-18 | Resource leak | **No expired token cleanup.** | Added `DeleteExpiredTokens()` to auth store. Wired on a 1-hour ticker in `main.go`. |
| B-19 | Correctness | **Unbounded `uniqueFilename` loop.** | Added 10000 iteration upper bound. Non-IsNotExist errors handled. Falls back to ULID-based filename. |
| B-20 | Correctness | **LIKE wildcards not escaped.** | Added `escapeLIKE()` helper. `ResolveLink` step 2 now uses escaped pattern with `ESCAPE '\'` clause. |
| B-21 | Performance | **Full table scan for slug matching.** | Added `slug` column (indexed) to `notes` table via migration. `Create`/`Update` populate it. `ResolveLink` step 3 now uses `WHERE slug = ?` instead of loading all notes. |
| B-22 | Concurrency | **Race on `Service.semantic` field.** | Added `sync.RWMutex` to search `Service`. `SetSemanticSearcher` uses write lock, `SearchSemantic` uses read lock. |
| B-23 | Correctness | **`extractSnippet` slices bytes not runes.** | Converted to `[]rune` for position calculations. Byte-based match positions converted to rune offsets before slicing. |
| B-24 | Security | **Unbounded `io.Copy` for audio.** | Replaced with `io.Copy(tmpFile, io.LimitReader(audio, 100*1024*1024))` (100MB max). Returns error if limit exceeded. |
| B-25 | Security | **No max length on AI inputs.** | Added `maxInputLen` (100KB) constant. Handler rejects query/selection/prompt exceeding limit with 400. |
| B-26 | Security | **XSS via `dangerouslySetInnerHTML`.** | Installed `dompurify`. Created `lib/sanitize.ts` with `sanitizeHtml()`. Applied to all 7 `dangerouslySetInnerHTML` usages across SearchPage, NoteEditorPage, AskPage, SynthesisModal, Sidebar. Strips `javascript:` URLs. |
| B-27 | Concurrency | **Stale closure in AskPage.** | Added `streamingRef = useRef('')` to accumulate tokens. `chat.done` handler reads from ref (always current) instead of stale closure state. |
| B-28 | Correctness | **"Load More" replaces notes.** | InboxPage and ProjectPage now use local `loadedNotes` state that appends on "Load more" with deduplication by ID. |
| B-29 | Performance | **renderMarkdown on every render.** | Wrapped in `useMemo(() => renderMarkdown(content), [content, viewMode])`. Returns empty string when `viewMode === 'editor'`. |
| B-30 | Performance | **`getOrphanNotes()` fetches all orphans.** | Removed the API call. Orphan status now computed locally from backlinks and wikilink regex on the content. |

---

### Low Severity (35+ issues -- not yet addressed)

These issues are tracked for a future improvement pass.

| ID | Package | File:Line(s) | Category | Description |
|----|---------|-------------|----------|-------------|
| C-1 | auth, userdb | `auth/store.go:174`, `userdb/manager.go:212` | Reliability | No SQLite `MaxOpenConns` set. Default unlimited connections can cause `SQLITE_BUSY` errors under write contention. |
| C-2 | config | `config.go:175` | Correctness | `DataDir` not resolved to absolute path. Relative paths (e.g., `./data`) resolve differently based on working directory. |
| C-3 | userdb | `manager.go:160-167` | Spec gap | `EnsureUserDirs` does not create `notes/inbox/` subdirectory. TEST_PLAN.md expects it. |
| C-4 | server | `middleware.go:74-92` | Observability | Recovery middleware missing stack trace. Uses `debug.Stack()` would help debug panics in production. |
| C-5 | main | `main.go:83-86` | Config | Logging level hardcoded to DEBUG. No config option for log level or format. |
| C-6 | server | `server.go` | Deployment | No static file serving for production frontend. `web/dist/` is not served by the Go server. |
| C-7 | server | `server.go:63-64` | Config | CORS origins not configurable. Hardcoded to `localhost`/`127.0.0.1` only, blocking LAN access. |
| C-8 | all handlers | various | Code quality | Duplicated `writeJSON`/`writeError` across 6+ handler files. Should extract to shared package. |
| C-9 | all handlers | various | Correctness | `writeJSON` ignores `json.Encoder.Encode` errors. |
| C-10 | various | 5+ locations | Correctness | `ulid.MustNew` can panic on entropy failure. Should use non-panicking `ulid.New()` in server code. |
| C-11 | note | `store.go:201-208` | Performance | N+1 query for tags in `note.Store.List`. Each note triggers a separate `loadTags` query. |
| C-12 | ai | `queue.go:61` | Concurrency | `RegisterHandler` writes to `q.handlers` map without lock. Safe only if all handlers are registered before `Run()` starts. |
| C-13 | ai | `chroma.go:126-133,229-236,266-273` | Performance | ChromaDB response bodies not drained on success. Prevents HTTP connection reuse. |
| C-14 | ai | `ollama.go:219` | Correctness | Silently skips malformed JSON lines in Ollama streaming. No logging or error signal. |
| C-15 | ai | `task.go:19` | Dead code | `TaskTypeTranscribe = "transcribe"` defined but no handler registered anywhere. |
| C-16 | main | `main.go:187,378` | Dead code | Duplicate `ChatService` instantiation. Two separate instances created with identical arguments. |
| C-17 | project | `service.go:281-286` | Performance | Regex recompiled on every `Slugify` call. Should be package-level `var`. |
| C-18 | note | `handler.go:257-331` | Architecture | `backlinks` and `listTags` handlers bypass service layer, directly accessing `service.store` and `service.userDBManager`. |
| C-19 | project | `service.go:214-237` | Correctness | Project cascade-to-inbox does not update YAML frontmatter on disk. File still contains `project: old-slug`. |
| C-20 | ws | `client.go:168-179` | Dead code | `writeError` function defined but never called. |
| C-21 | watcher | `reconcile.go:82-87` | Correctness | Mtime comparison has 1-second blind spot due to RFC3339 second precision vs nanosecond mtime. |
| C-22 | watcher | `reconcile.go:103-115` | Correctness | No context cancellation check in delete-detection loop. Causes slow shutdown with many orphaned entries. |
| C-23 | watcher | `watcher.go:75-103` | Concurrency | `Watch`/`Unwatch` race on concurrent calls for same user. `WalkDir` and `fsWatcher.Add/Remove` execute outside the mutex. |
| C-24 | search | `semantic.go:99-120` | Performance | N+1 DB query per note in semantic search snippet retrieval. |
| C-25 | search | `semantic.go:134-147` | Correctness | `getNoteSnippet` silently swallows DB errors. Returns empty string with no logging. |
| C-26 | graph | `service.go:274-307` | Correctness | `GetOrphanNotes` does not load tags or project names. Inconsistent with `GetGraph` which loads both. |
| C-27 | graph | `handler.go:53-61` | Correctness | Invalid date parameters silently ignored. No HTTP 400 returned. |
| C-28 | graph | `service.go:281-289` | Performance | No upper limit on orphan notes query result size. |
| C-29 | ai | `embedder.go:107-127` | Performance | `DeleteNoteEmbeddings` sends 500 chunk IDs per delete regardless of actual chunk count. |
| C-30 | ai | `chroma.go:38-39` | Config | ChromaDB client has 30s hardcoded timeout, not configurable. |
| C-31 | note | `store.go:293` | Correctness | `INSERT OR IGNORE` in `UpdateLinks` silently drops links with duplicate `link_text` but different display text. |
| C-32 | main | `main.go:304-309` | Correctness | Swallowed `Enqueue` errors for embed/delete tasks. If persistence fails, the task is silently lost. |
| C-33 | auth | `service.go:85-98` | Semantics | `ErrInvalidCredentials` used for validation errors in Register. Conflates authentication failures with input validation errors. |
| C-34 | config | `config.go:140-171` | Spec gap | Default Whisper binary path (`whisper-cli`) not applied in `applyDefaults`. |
| C-35 | note | `store.go:122-128` | Correctness | `ProjectID` and `InboxOnly` not mutually exclusive at store level (handler prevents this, but store does not). |

---

### Frontend-Specific Low Severity (not yet addressed)

| ID | Area | Description |
|----|------|-------------|
| C-F1 | `api/client.ts:44` | Refresh token stored in localStorage (vulnerable to XSS). HttpOnly cookies would be more secure. |
| C-F2 | `api/client.ts:203-242,431-482` | `listNotes`, `searchFTS`, `captureVoice` bypass the `request()` helper, duplicating auth/retry logic. |
| C-F3 | `api/client.ts:379,387` | Template name not URL-encoded in path interpolation. |
| C-F4 | Stores | `noteStore` and `projectStore` methods (`createNote`, `updateNote`, `deleteNote`, `createProject`) do not catch errors internally. Callers must handle. |
| C-F5 | `NoteEditorPage.tsx:149-156` | `handleDelete` missing error handling. If deletion fails, `navigate('/')` never executes. |
| C-F6 | `NoteEditorPage.tsx` | Dropdown menus lack ARIA roles (`role="menu"`, `aria-expanded`), keyboard navigation, and outside-click handlers. |
| C-F7 | `CaptureModal.tsx`, `SynthesisModal.tsx` | Missing focus trap in modals. Tab key can escape to elements behind the backdrop. |
| C-F8 | `CaptureModal.tsx:178-182` | Backdrop click discards content without confirmation (unlike Escape key which shows confirm dialog). |
| C-F9 | `Layout.tsx:16-32`, `Sidebar.tsx:53-55` | `useKeyboard` hook creates new bindings array on every render, causing listener re-registration. Should use `useMemo`. |
| C-F10 | `AskPage.tsx:23` | `useStreaming` state never toggled. Non-streaming code path is unreachable dead code. |
| C-F11 | `NoteEditorPage/editorTheme.ts:22` | Uses `!important` (violates FE_DESIGN.md / AGENTS.md rule: "Never use `!important`"). |
| C-F12 | `Sidebar.tsx:374-381` | Settings button actually logs out. Misleading `title="Settings"` and `aria-label="Settings"` with Settings icon but `onClick` calls `handleLogout`. |
| C-F13 | `wikilinkExtension.ts:63` | `lastFetch` variable written but never read (dead code). |
| C-F14 | `GraphPage.tsx:12` | `cytoscape.use(fcose)` called at module level. Double-registration in hot-reload can throw. |
| C-F15 | Search pages | Search debounce does not cancel in-flight requests. Stale responses can overwrite newer results. Should use `AbortController`. |
| C-F16 | `api/client.ts` | Concurrent token refresh requests not deduplicated. Multiple 401s trigger parallel refresh calls. |

---

### Spec Mismatches (not yet addressed)

| ID | Description |
|----|-------------|
| S-1 | AI routes at `/api/ai/notes/{id}/assist` and `/api/ai/notes/{id}/related` instead of `/api/notes/{id}/ai-assist` and `/api/notes/{id}/related` per IMP_PLAN.md. |
| S-2 | Missing `internal/userdb/migrate.go` -- no migration version tracking. Uses `IF NOT EXISTS` only, which cannot handle future `ALTER TABLE` migrations. |
| S-3 | Many TEST_PLAN.md test cases not implemented (auth handler edge cases, JWT edge cases, middleware tests, store validation tests). |
| S-4 | `models.transcription` default value for Whisper binary path not applied in `applyDefaults`. |

---

### New packages and files added

| File | Purpose |
|------|---------|
| `internal/validate/validate.go` | Shared input validation: `Path()`, `PathWithinDir()`, `Name()`, `UserID()` |
| `internal/validate/validate_test.go` | 10 tests covering path traversal, name safety, userID format |
| `web/src/lib/sanitize.ts` | DOMPurify wrapper with `javascript:` URL stripping |
| `migrations/user/002_add_slug.sql` | Adds `slug` column and index to `notes` table |

### Test summary (updated)

- Go: 16 packages tested, all passing
  - `internal/validate` (10 tests): path traversal, name safety, userID format
  - All existing packages continue to pass with updated tests
- Frontend: 18 test files, 154 tests, all passing
  - All existing test files continue to pass

---

*Last updated: 2026-03-09 (All Critical + Medium audit issues resolved)*
