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

## Post-Implementation Audit (2026-03-09)

Comprehensive code review across all packages, frontend, and migrations. All Go tests (15 packages) and frontend tests (18 files, 154 tests) pass. Issues below are organized by severity.

---

### Critical / High Severity (15 issues)

| ID | Package | File:Line(s) | Category | Description |
|----|---------|-------------|----------|-------------|
| A-1 | note | `service.go:408-415` | Security | **No path traversal validation in `Reindex`.** `filePath` is joined directly via `filepath.Join(notesDir, filePath)`. A path like `../../other-user/notes/secret.md` escapes the user directory. AGENTS.md mandates: "reject any file path containing `..`, absolute paths, or null bytes." |
| A-2 | userdb | `manager.go:131-166` | Security | **No path traversal validation on `userID`.** `UserNotesDir`, `UserDataDir`, `EnsureUserDirs`, and `Open` use `userID` directly in filesystem paths without validating for `..`, `/`, `\`, or null bytes. A crafted `userID` accesses arbitrary directories. |
| A-3 | capture | `url.go:54-72` | Security | **SSRF bypass via DNS rebinding (TOCTOU).** DNS is resolved to check for private IPs, then `dialer.DialContext` re-resolves DNS internally. Between the two resolutions, a DNS rebinding attack can flip the A record from public to `127.0.0.1`. Fix: connect to the already-validated IP directly. |
| A-4 | capture | `url.go:76-78` | Security | **SSRF bypass: `0.0.0.0` not blocked.** `isPrivateIP` does not call `ip.IsUnspecified()`. The address `0.0.0.0` routes to localhost on many OSes. |
| A-5 | note | `handler.go:73-76`, `service.go:84` | Security | **No input validation for note titles.** Only checks for empty. Does not reject `\x00`, `/`, `\`, or `..` per AGENTS.md security invariants. |
| A-6 | project | `handler.go:64-66` | Security | **No input validation for project names.** Same issue as A-5. |
| A-7 | note, project | `note/service.go:170-191,347-370,472-486`, `project/service.go:189-258` | Data integrity | **No DB transactions for multi-step operations.** Create/Update/Reindex perform separate DB writes for notes, tags, and links without a transaction. A crash mid-operation leaves the database inconsistent (note without tags, half-resolved links, etc.). |
| A-8 | note | `service.go:321-330` | Data integrity | **Stale tags written to frontmatter on Update.** When building frontmatter for Update, `existing.Tags` (the OLD tags from the initial `store.Get`) is used. New tags from `req.Tags` are only applied to the DB after the file is rewritten. The frontmatter on disk has old tags; next reindex restores old tags, overwriting the new ones. |
| A-9 | project | `service.go:139-154` | Data integrity | **Project slug rename does not update note `file_path` in DB.** When a project is renamed and the slug changes, the directory is renamed on disk but the `notes.file_path` column is not updated. All notes with the old slug have stale paths, causing `Get` to fail with file-not-found. |
| A-10 | userdb | `manager.go:188-203` | Concurrency | **DB handle use-after-close from eviction.** The eviction timer can close a `*sql.DB` handle while other goroutines still hold references from previous `Open()` calls. No reference counting or lease mechanism exists. Subsequent queries on evicted handles fail with "database is closed." |
| A-11 | watcher | `watcher.go:273-288` | Concurrency | **Race condition in debounce timer.** `time.Timer.Stop()` does not prevent an already-fired callback from executing, so duplicate handler invocations are possible when rapid events reset the same file's timer. |
| A-12 | watcher | `watcher.go:139-148,237-251` | Concurrency | **Self-write suppression is one-shot but writes generate multiple fsnotify events.** A single `os.WriteFile` produces 2+ events (CREATE+WRITE or WRITE+CHMOD). Only the first is suppressed; subsequent events trigger the handler, causing unnecessary reindex cycles. |
| A-13 | ai | `queue.go:94-171` | Concurrency | **AI queue workers: only 1 processes tasks despite config.** The `notify` channel has buffer size 1. Only one worker wakes up per notification and calls `processAll()` which drains the entire queue serially. Multiple workers provide no parallelism. |
| A-14 | ai | `queue.go:207-227` | Correctness | **Fair scheduling incorrect: assumes heap elements are sorted.** The `dequeue` method linearly scans and breaks early when priority differs, but a heap only guarantees `pq[0]` is minimum -- other elements are NOT in sorted order. Tasks from the same priority may not be interleaved across users correctly. |
| A-15 | ai | `chroma.go:186-199` | Correctness | **ChromaDB nil pointer panic.** Accesses `result.Distances[0]` and `result.Metadatas[0]` without checking if these slices are non-nil/non-empty. If ChromaDB returns null for these fields, the code panics with index-out-of-range. |

---

### Medium Severity (30 issues)

| ID | Package | File:Line(s) | Category | Description |
|----|---------|-------------|----------|-------------|
| B-1 | all handlers | various | Security | **No request body size limit (`MaxBytesReader`) on any handler.** All `json.NewDecoder(r.Body).Decode` calls read the entire request body without size limits. An attacker can send multi-GB payloads to exhaust server memory. |
| B-2 | server | `server.go:141-142` | Security | **`WriteTimeout: 30s` kills WebSocket and streaming connections.** The `http.Server.WriteTimeout` is a hard deadline for the entire response, including long-lived WebSocket connections and streaming AI responses. |
| B-3 | auth | `service.go:96-98` | Security | **No maximum password length check.** `bcrypt.GenerateFromPassword` silently truncates at 72 bytes. More importantly, an attacker can send a multi-MB password to cause excessive CPU usage. |
| B-4 | auth | `service.go:84` | Security | **No username format validation.** Accepts spaces, special characters, and extreme lengths. Should enforce alphanumeric/underscore/hyphen with a max length. |
| B-5 | auth | `jwt.go:31`, `config/config.go:191` | Security | **No JWT secret minimum length enforcement.** Accepts a single-character secret. Should enforce at least 32 characters. |
| B-6 | config | `config.go:153` | Security | **No bcrypt cost range validation.** A user can set `bcrypt_cost: 1` (below `bcrypt.MinCost` of 4) or `bcrypt_cost: 31` (extreme DoS). |
| B-7 | auth, capture | `auth/handler.go:51`, `capture/handler.go:81` | Security | **Error messages leak internal details to clients.** Appends full Go error chains (including function names and package paths) to HTTP responses. AGENTS.md: "Never expose internal error details in HTTP responses." |
| B-8 | capture | `url.go:42-48` | Security | **SSRF: redirect target scheme not validated.** `CheckRedirect` limits redirect count but does not validate the redirect target URL's scheme. A redirect to `file://` is possible before the dialer is invoked. |
| B-9 | ws | `client.go:35-38` | Security | **WebSocket origin check disabled.** `InsecureSkipVerify: true` allows any website to connect via WebSocket (cross-site WebSocket hijacking). |
| B-10 | ws | `client.go:45-46` | Security | **No WebSocket auth timeout.** After accepting the upgrade, `conn.Read` blocks indefinitely waiting for the auth message. Malicious clients can hold connections open without authenticating. |
| B-11 | server | `middleware.go:51,57,63,86` | Correctness | **`http.Error` sets Content-Type to `text/plain` but body is JSON.** Auth middleware and recovery middleware write JSON-formatted error bodies but `http.Error()` sets `Content-Type: text/plain`. Clients parsing Content-Type will not attempt JSON decode. |
| B-12 | note | `store.go:133-158` | Correctness | **`Since`/`Until` filters use `created_at` but default sort uses `updated_at`.** Users filtering by time range likely expect the filter and sort to apply to the same column. A note modified within the range but created outside it is excluded. |
| B-13 | auth, note, project | `auth/store.go:91-92,145`, `note/store.go:531-532`, `project/store.go:68-69` | Correctness | **Silently discarded `time.Parse` errors.** If stored timestamps are malformed, fields silently become zero-value (`time.Time{}`), causing downstream logic errors. |
| B-14 | ai | `synthesizer.go:63-93,146-179` | Correctness | **No `rows.Err()` check after `Next()` loops.** If `dbRows.Next()` terminates due to a scan error, the error is swallowed and synthesis proceeds with incomplete data. |
| B-15 | main | `main.go:57-70` | Architecture | **`noteBodyAdapter.UpdateNoteBody` bypasses service layer.** Directly executes raw SQL `UPDATE` against the notes table. The `.md` file on disk is not updated, no wikilink re-parsing occurs, and no `content_hash` update happens -- causing DB/disk divergence. |
| B-16 | main | `main.go:468-478` | Correctness | **Incorrect shutdown order.** WebSocket connections are closed before the HTTP server stops accepting new connections. New WS connections can be accepted between `hub.CloseAll()` and `srv.Shutdown()`. |
| B-17 | auth | `service.go:220-243` | Resource leak | **Refresh token accumulation with no cap per user.** Every login creates a new refresh token. No limit on active tokens per user and no cleanup of old ones. |
| B-18 | auth | store/service | Resource leak | **No expired refresh token cleanup job.** Expired tokens are only deleted when used. Unused expired tokens remain in the database indefinitely. |
| B-19 | note | `service.go:594-601` | Correctness | **Unbounded loop in `uniqueFilename`.** `for i := 2; ; i++` runs indefinitely if the filesystem has a pathological state. Should have a reasonable upper bound (e.g., 10000). |
| B-20 | note | `store.go:328-334` | Correctness | **SQL LIKE wildcards not escaped in `ResolveLink`.** `linkText` from wikilinks is used directly in a `LIKE` pattern. A link text containing `%` or `_` matches unintended rows. |
| B-21 | note | `store.go:344-361` | Performance | **Full table scan for slug-match link resolution.** Step 3 of `ResolveLink` loads ALL notes into memory for slug comparison. Same issue in `ResolveDanglingLinks` (lines 416-438). |
| B-22 | search | `service.go:32-34` | Concurrency | **Race condition on `Service.semantic` field.** `SetSemanticSearcher()` writes without synchronization while `SearchSemantic()` reads concurrently. |
| B-23 | search | `semantic.go:151-197` | Correctness | **`extractSnippet` slices bytes not runes.** `body[start:end]` can split a multi-byte UTF-8 character in half, producing invalid UTF-8. |
| B-24 | capture | `voice.go:81` | Security | **Unbounded `io.Copy` for audio data.** Copies the entire audio stream to disk without size limit. |
| B-25 | ai | `handler.go:77,105,275`, `chat.go:54`, `writer.go:83` | Security | **No max length on query/selection input strings.** A 100MB query string would be forwarded to Ollama, potentially causing OOM. |
| B-26 | web | multiple pages | Security | **XSS risk via `dangerouslySetInnerHTML`.** FTS snippets from the backend and markdown-rendered content are injected as raw HTML without sanitization (SearchPage, Sidebar, NoteEditorPage, AskPage, SynthesisModal). `markdown-it` with `linkify: true` does not block `javascript:` URLs. |
| B-27 | web | `AskPage.tsx:38-58` | Concurrency | **Stale closure in AskPage streaming handler.** `chat.done` handler reads `streamingContent` from a stale closure. The completed message may be missing tokens. Should use a ref. |
| B-28 | web | `InboxPage.tsx:88-96`, `ProjectPage.tsx:111-117` | Correctness | **"Load More" replaces notes instead of appending.** `noteStore.fetchNotes` does `set({ notes })` which replaces the entire array. Clicking "Load more" loses the previously loaded notes. |
| B-29 | web | `NoteEditorPage.tsx:304` | Performance | **`renderMarkdown(content)` called on every render without `useMemo`.** CPU-intensive markdown rendering runs even when the preview pane is hidden (`viewMode === 'editor'`). |
| B-30 | web | `NoteEditorPage.tsx:80-82` | Performance | **`getOrphanNotes()` fetches ALL orphans to check one note.** Expensive for large note collections. Should use a per-note endpoint or cache. |

---

### Low Severity (35+ issues)

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

### Frontend-Specific Low Severity

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

### Spec Mismatches

| ID | Description |
|----|-------------|
| S-1 | AI routes at `/api/ai/notes/{id}/assist` and `/api/ai/notes/{id}/related` instead of `/api/notes/{id}/ai-assist` and `/api/notes/{id}/related` per IMP_PLAN.md. |
| S-2 | Missing `internal/userdb/migrate.go` -- no migration version tracking. Uses `IF NOT EXISTS` only, which cannot handle future `ALTER TABLE` migrations. |
| S-3 | Many TEST_PLAN.md test cases not implemented (auth handler edge cases, JWT edge cases, middleware tests, store validation tests). |
| S-4 | `models.transcription` default value for Whisper binary path not applied in `applyDefaults`. |

---

### Recommended Fix Priority

1. **A-1, A-2**: Add path traversal validation (reject `..`, absolute paths, null bytes) to `userdb.Manager` and `note.Service.Reindex`
2. **A-3, A-4**: Fix SSRF -- add `IsUnspecified()` check; connect to validated IP directly instead of re-resolving DNS
3. **A-5, A-6**: Add input validation for note titles, project names, tags per AGENTS.md security invariants
4. **A-7**: Wrap multi-step DB operations in transactions (note Create/Update/Reindex, project Delete)
5. **A-8**: Fix stale tags -- apply new tags to frontmatter before writing file to disk
6. **A-9**: Fix project rename to update `notes.file_path` for all affected notes
7. **B-1**: Add `http.MaxBytesReader` on all handler JSON decode paths (1-10MB limit)
8. **B-2**: Remove `WriteTimeout` from HTTP server (breaks WebSocket/streaming) or use per-handler timeouts
9. **B-26**: Add DOMPurify for all `dangerouslySetInnerHTML` usage; configure markdown-it to reject `javascript:` URLs
10. **B-27**: Fix AskPage stale closure -- use `useRef` for streaming content accumulation

---

*Last updated: 2026-03-09 (Post-implementation audit)*
