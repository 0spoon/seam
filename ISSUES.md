# Seam -- Open Issues

Comprehensive audit of the codebase against IMP_PLAN.md, FE_DESIGN.md, and AGENTS.md.
Issues are organized by severity, then by area. Each issue has a unique ID for tracking.

Legend: **Critical** = crash/data loss/security exploit, **High** = significant bug or missing spec feature, **Medium** = correctness/UX/security concern, **Low** = minor quality/polish

---

## Critical (3 issues) -- RESOLVED

All 3 critical issues have been fixed.

<details>
<summary>Click to expand resolved critical issues</summary>

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-C1 | Frontend: NoteEditor | Wikilink clicks in preview pane do nothing. | **Fixed** |
| I-C2 | Frontend: AskPage | Streaming has no error recovery -- UI gets permanently stuck. | **Fixed** |
| I-C3 | Backend: AI handler | Nil pointer dereference when AI services not configured. | **Fixed** |

</details>

---

## High (14 issues) -- RESOLVED

All 14 high-priority issues have been fixed.

<details>
<summary>Click to expand resolved high issues</summary>

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-H1 | Frontend: NoteEditor | Unsaved content lost on navigation. | **Fixed** |
| I-H2 | Frontend: NoteEditor | Race condition on related notes/two-hop backlinks. | **Fixed** |
| I-H3 | Frontend: NoteEditor | AI assist errors silently swallowed. | **Fixed** |
| I-H4 | Frontend: SearchPage | AbortController does not actually cancel HTTP requests. | **Fixed** |
| I-H5 | TUI: login.go | Register mode is cosmetic -- always calls Login. | **Fixed** |
| I-H6 | TUI: client.go | Race condition on shared HTTPClient.Timeout. | **Fixed** |
| I-H7 | TUI: ask.go | Streaming infrastructure defined but never produces streaming messages. | **Fixed** |
| I-H8 | TUI: voice_capture.go | Shells out to cat/rm instead of os.ReadFile/os.Remove. | **Fixed** |
| I-H9 | TUI: voice_capture.go | cmd.Stdout = nil inherits parent stdout, can corrupt TUI. | **Fixed** |
| I-H10 | TUI: template_picker.go | Potential panic: indexing empty templates slice. | **Fixed** |
| I-H11 | Backend: note service | File rewritten before DB transaction in Update. | **Fixed** |
| I-H12 | Backend: project service | Delete cascade moves files before transaction commit. | **Fixed** |
| I-H13 | Backend: synthesizer | SynthesizeStream never exposed via any endpoint. | **Fixed** |
| I-H14 | Backend: ai chat | No validation on history message roles (prompt injection). | **Fixed** |

</details>

---

## Medium (45 issues) -- RESOLVED

All 45 medium-priority issues have been fixed.

<details>
<summary>Click to expand resolved medium issues</summary>

### Backend

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-M1 | main.go | Shutdown order fixed: watcher closes before DBs, AI queue waited on. | **Fixed** |
| I-M2 | auth handler | MaxBytesReader added to refresh/logout endpoints. | **Fixed** |
| I-M3 | note store | Get/GetByFilePath changed to accept DBTX interface. | **Fixed** |
| I-M4 | note service | SetSuppressor protected with sync.RWMutex. | **Fixed** |
| I-M5 | project service | Update wrapped in database transaction. | **Fixed** |
| I-M6 | ws client | WebSocket origin patterns now configurable. | **Fixed** |
| I-M7 | capture handler | MaxBytesReader added to URL capture JSON body. | **Fixed** |
| I-M8 | watcher | Context checked before debounce handler call. | **Fixed** |
| I-M9 | ai queue | Failed tasks re-enqueued on DB open failure. | **Fixed** |
| I-M10 | ai queue | UpdateStatus errors now logged. | **Fixed** |
| I-M11 | ai queue | Max queue size (10000) with backpressure. | **Fixed** |
| I-M12 | ai queue | Per-task timeout (5 min) via context.WithTimeout. | **Fixed** |
| I-M13 | ai embedder | Context cancellation checked between chunks. | **Fixed** |
| I-M14 | ai embedder | ReindexAll limited to 10000 IDs with warning. | **Fixed** |
| I-M15 | ai writer | Setters protected with sync.RWMutex. | **Fixed** |
| I-M16 | ai writer | DB fallback removed; uses note service layer. | **Fixed** |
| I-M17 | ai synthesizer | Shared retrieveNotes method extracted. | **Fixed** |
| I-M18 | ai synthesizer | SQL-level body truncation with SUBSTR. | **Fixed** |
| I-M19 | ai handler | Per-user rate limiting (10/min) on AI endpoints. | **Fixed** |
| I-M20 | ws main.go | Streaming goroutine breaks on write failures. | **Fixed** |
| I-M21 | config | Model names only required when AI enabled. | **Fixed** |
| I-M22 | chroma | Array length validation on Upsert/Add. | **Fixed** |
| I-M23 | search handler | FTS search limit capped at 500. | **Fixed** |

### Frontend

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-M24 | api/client.ts | 30s request timeout via AbortController. | **Fixed** |
| I-M25 | api/types.ts | project_id type clarified (undefined=no change, ""=inbox). | **Fixed** |
| I-M26 | authStore | Email persisted in localStorage. | **Fixed** |
| I-M27 | authStore | restoreSession uses client's tryRefresh(). | **Fixed** |
| I-M28 | noteStore | WS note.changed events trigger list updates. | **Fixed** |
| I-M29 | Sidebar | Collapsed search navigates to /search. | **Fixed** |
| I-M30 | Sidebar | ARIA attributes added to search dropdown. | **Fixed** |
| I-M31 | GraphPage | Minimap updates on viewport changes. | **Fixed** |
| I-M32 | GraphPage | Error state with retry button. | **Fixed** |
| I-M33 | GraphPage | Keyboard navigation and ARIA labels added. | **Fixed** |
| I-M34 | TimelinePage | Pagination with "Load more" button. | **Fixed** |
| I-M35 | TimelinePage | Error state with retry button. | **Fixed** |
| I-M36 | AskPage | History trimmed to last 10 messages for server. | **Fixed** |
| I-M37 | AskPage | Citations show note titles. | **Fixed** |
| I-M38 | CommandPalette | "Ask Seam" command added. | **Fixed** |
| I-M39 | CommandPalette | Focus trap and ARIA dialog role added. | **Fixed** |
| I-M40 | CaptureModal | Microphone errors shown via toast. | **Fixed** |
| I-M41 | CaptureModal | Template overwrites require confirmation. | **Fixed** |
| I-M42 | Various | Toast notifications integrated across app. | **Fixed** |
| I-M43 | ws.ts | Token refresh before WS reconnection. | **Fixed** |
| I-M44 | NoteEditor | Loading indicator on note fetch. | **Fixed** |

### TUI

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-M45 | editor.go | Unsaved changes warning on Esc (double-press). | **Fixed** |
| I-M46 | editor.go | Modified flag only set on actual content change. | **Fixed** |
| I-M47 | editor.go | Markdown syntax hints displayed. | **Fixed** |
| I-M48 | main_screen.go | Delete confirmation (double-press d). | **Fixed** |
| I-M49 | client.go | Automatic token refresh on 401. | **Fixed** |
| I-M50 | client.go | Context support for all HTTP requests. | **Fixed** |
| I-M51 | voice_capture.go | Secure temp file via os.CreateTemp. | **Fixed** |
| I-M52 | voice_capture.go | Temp file cleaned up on Esc. | **Fixed** |
| I-M53 | timeline.go | Colors use styles.go palette. | **Fixed** |
| I-M54 | timeline.go | Loading indicator on init. | **Fixed** |
| I-M55 | main.go (TUI) | 5s timeout on startup refresh. | **Fixed** |
| I-M56 | login.go | Server URL field added. | **Fixed** |

</details>

---

## Low (48 issues) -- RESOLVED

All 48 low-priority issues have been fixed.

<details>
<summary>Click to expand resolved low issues</summary>

### Backend

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-L1 | main.go | WebDistDir configurable via config. | **Fixed** |
| I-L2 | middleware | writeJSONError uses json.NewEncoder. | **Fixed** |
| I-L3 | note store | ResolveLink uses exact match + path-separated LIKE. | **Fixed** |
| I-L4 | note handler | Malformed since/until returns 400. | **Fixed** |
| I-L5 | project service | os.RemoveAll for non-empty dirs. | **Fixed** |
| I-L6 | userdb manager | ListUsers validates directory names. | **Fixed** |
| I-L7 | watcher | Expired suppression entries cleaned up. | **Fixed** |
| I-L8 | watcher reconcile | Scan errors logged. | **Fixed** |
| I-L9 | ws client | SetReadLimit(64KB) applied. | **Fixed** |
| I-L10 | ai queue | O(n) complexity documented as acceptable. | **Fixed** |
| I-L11 | ai queue | waitForTask goroutine pattern documented. | **Fixed** |
| I-L12 | ai chroma | Tenant/database configurable via ChromaConfig. | **Fixed** |
| I-L13 | ai chroma | Query returns empty slice instead of nil. | **Fixed** |
| I-L14 | ai embedder | Chunk size/overlap configurable. | **Fixed** |
| I-L15 | ai chat | Retrieval limit and truncation configurable. | **Fixed** |
| I-L16 | ai synthesizer | Note limit configurable. | **Fixed** |
| I-L17 | ai linker | Three-strategy JSON extraction (direct, bracket, regex). | **Fixed** |
| I-L18 | ai handler | FindRelated method on Embedder. | **Fixed** |
| I-L19 | ai writer | marshalResult returns and handles errors. | **Fixed** |
| I-L20 | ai task store | Time parse errors logged. | **Fixed** |
| I-L21 | config | Env overrides for SEAM_LOG_LEVEL, SEAM_CORS_ORIGINS. | **Fixed** |
| I-L22 | config | Falls back to defaults when YAML missing. | **Fixed** |
| I-L23 | migration | 002_add_slug.sql handles missing column. | **Fixed** |
| I-L24 | migration | Migration + version recording in single transaction. | **Fixed** |
| I-L25 | project store | Create/Update accept DBTX interface. | **Fixed** |

### Frontend

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-L26 | api/client.ts | offset=0 no longer skipped. | **Fixed** |
| I-L27 | api/types.ts | Unused SearchResponse removed. | **Fixed** |
| I-L28 | noteStore | createNote refetches list for correct ordering. | **Fixed** |
| I-L29 | noteStore | fetchBacklinks logs errors. | **Fixed** |
| I-L30 | SearchPage | Query synced to URL search params. | **Fixed** |
| I-L31 | AskPage | aria-live="polite" on streaming area. | **Fixed** |
| I-L32 | GraphPage | Dot-grid background (already existed in CSS). | **Fixed** |
| I-L33 | Various | motion/framer-motion for page transitions and staggered lists. | **Fixed** |
| I-L34 | Various | Responsive breakpoints in Layout CSS. | **Fixed** |
| I-L35 | CaptureModal | Ref-based focus trap instead of querySelector. | **Fixed** |
| I-L36 | CaptureModal | MediaRecorder compatibility check. | **Fixed** |
| I-L37 | markdown.ts | Wikilink href uses javascript:void(0). | **Fixed** |

### TUI

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-L38 | app.go | Dead code removed. | **Fixed** |
| I-L39 | main_screen.go | Pagination with Ctrl+F/B. | **Fixed** |
| I-L40 | editor.go | Selection limitation documented. | **Fixed** |
| I-L41 | editor.go | Title editing via Ctrl+T. | **Fixed** |
| I-L42 | search.go | Minimum 2-character query. | **Fixed** |
| I-L43 | ask.go | Ctrl+S submits, Enter inserts newline. | **Fixed** |
| I-L44 | voice_capture.go | OS-aware recording commands. | **Fixed** |
| I-L45 | voice_capture.go | 5-minute max recording duration. | **Fixed** |
| I-L46 | ai_assist.go | Scrollable results with j/k. | **Fixed** |
| I-L47 | app.go | Logout via Ctrl+L. | **Fixed** |
| I-L48 | styles.go | Color palette matches FE_DESIGN.md. | **Fixed** |

</details>

---

## Summary

| Severity | Count | Status |
|----------|-------|--------|
| Critical | 3 | **All resolved** |
| High | 14 | **All resolved** |
| Medium | 45 | **All resolved** |
| Low | 48 | **All resolved** |
| **Total** | **110** | **All resolved** |

---

*Updated: 2026-03-09*
