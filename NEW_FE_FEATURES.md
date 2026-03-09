# New Frontend Features Plan

Nine features to transform Seam from a capable note-taking system into a knowledge tool users live in daily. Each feature targets a specific UX gap identified through critical analysis of the current implementation against the "where ideas connect" vision.

---

## Feature 1: Daily Notes (The Gravity Well)

### Motivation

Every successful PKM tool (Obsidian, Logseq, Roam) has daily notes as a primary entry point. It eliminates "where should I put this?" friction and creates a daily habit loop. Without it, Seam is a tool users visit to organize; with it, Seam is a place users think. The `daily-log` template and Timeline page already exist -- this feature connects them into a first-class workflow.

### Backend Changes

| Change | Details |
|--------|---------|
| New endpoint | `GET /api/notes/daily?date=YYYY-MM-DD` -- returns the daily note for the given date, creating it if it does not exist |
| Auto-creation logic | Uses `daily-log` template, title = `"YYYY-MM-DD DayOfWeek"`, auto-tags `#daily`, auto-assigns to "Journal" project (auto-created on first use) |
| Quick-append endpoint | `POST /api/notes/{id}/append` -- appends a timestamped line to an existing note body without replacing it. Request body: `{"text": "..."}`. Prepends `\n- HH:MM -- ` before the text. |

No schema changes -- daily notes are regular notes with tag/project conventions.

### Frontend Changes

| Component | Details |
|-----------|---------|
| New route `/today` | Calls `GET /api/notes/daily?date=today`, redirects to `/notes/:id` |
| Sidebar "Today" button | Prominent position between search and Inbox. Shows formatted date (e.g., "Mon, Mar 9"). |
| Quick-append input | Collapsible single-line input at bottom of sidebar. `Cmd+.` global shortcut. Appends timestamped line to today's note, clears input, shows toast confirmation. No page navigation. |
| Calendar widget | Mini calendar on Timeline page. Days with daily notes are dot-indicated. Clicking a day navigates to that day's daily note. |
| Editor: prev/next nav | When viewing a daily note (detected by `#daily` tag), show left/right arrow buttons in the title area to navigate to adjacent daily notes. |
| `noteStore` additions | `fetchOrCreateDaily(date)`, `appendToNote(id, text)` |
| API client additions | `getDailyNote(date)`, `appendToNote(id, text)` |

### Implementation Steps

1. Backend: `POST /api/notes/{id}/append` endpoint (reusable beyond daily notes)
2. Backend: `GET /api/notes/daily` endpoint with auto-creation logic
3. Frontend: API client + store methods
4. Frontend: `/today` route + sidebar "Today" button
5. Frontend: Quick-append input + `Cmd+.` shortcut
6. Frontend: Calendar widget on Timeline page
7. Frontend: Previous/next day navigation in editor

---

## Feature 2: Wikilink Hover Preview + Direct Navigation (The Flow Keeper)

### Motivation

The "seam" metaphor is about connections, but exploring connections currently breaks flow. Clicking `[[SomeNote]]` in the preview pane redirects to `/search?q=target` -- a search page, not the note. There is no hover preview anywhere. For a connection-focused tool, this is the highest-impact UX fix. Compare to Obsidian, where hovering a link shows the target note inline and clicking navigates directly.

### Backend Changes

| Change | Details |
|--------|---------|
| New endpoint | `GET /api/notes/resolve?title=...` -- resolves a wikilink target string to a note ID using the existing link resolution algorithm (title match > filename match > slug match). Returns `{note_id, title, snippet}` or `{dangling: true}`. |
| Note response enhancement | Optionally include `resolved_links: [{target, note_id}]` in `GET /api/notes/:id` response so the frontend has link targets without extra round-trips. |

### Frontend Changes

| Component | Details |
|-----------|---------|
| `LinkPreviewCard` component | Floating popover positioned near the hovered link. Shows: note title, first ~200 chars of body (rendered markdown, sanitized), tags (colored pills), project name. Appears after 300ms hover delay, disappears on mouseout with 150ms grace period. |
| Preview pane: hover behavior | Attach `mouseenter`/`mouseleave` listeners to `[data-wikilink]` elements. On hover, fetch note preview via `resolveWikilink()` with client-side LRU cache (~50 entries). |
| Preview pane: click behavior | Change from `navigate('/search?q=...')` to `navigate('/notes/:resolvedId')`. For dangling links (no matching note), show a small popover: "Create [[TargetTitle]]?" with a create button. |
| Backlinks panel: hover preview | Same `LinkPreviewCard` on hover over backlink entries in the right panel. |
| Related notes panel: hover preview | Same `LinkPreviewCard` on hover over related note entries. |
| Wikilink resolution cache | In-memory `Map<string, {noteId, title, snippet}>`. Invalidated on `note.changed` WebSocket events. |
| API client addition | `resolveWikilink(title): Promise<ResolvedLink>` |

### Implementation Steps

1. Backend: `GET /api/notes/resolve` endpoint
2. Frontend: API client method + in-memory cache
3. Frontend: `LinkPreviewCard` component (positioning, animation, content)
4. Frontend: Hover behavior on preview pane wikilinks
5. Frontend: Direct navigation on click (replace search redirect)
6. Frontend: Dangling link "Create note?" prompt
7. Frontend: Hover on backlinks + related notes panels

---

## Feature 3: Knowledge Gardening Mode (The AI Differentiator)

### Motivation

Seam's AI knows about orphan notes, suggested links, semantic similarity, untagged notes, and inbox notes -- but this intelligence is scattered across different panels and entirely pull-based. A dedicated gardening flow turns passive AI into an active collaborator that guides users through maintaining their knowledge base. No competitor has this. The metaphor: if your knowledge base is a garden, Seam should help you tend it.

### Backend Changes

| Change | Details |
|--------|---------|
| New endpoint | `GET /api/review/queue?limit=20` -- returns a prioritized review queue. Aggregates: orphan notes (from graph store), untagged notes (notes with zero tags), inbox notes (no project), notes with pending auto-link suggestions, high-similarity note pairs (from ChromaDB). Each item includes `type`, `note_id`, `reason`, and `suggestions[]`. |
| New endpoint | `POST /api/ai/suggest-tags` -- given a note ID, uses Ollama to suggest tags from the user's existing tag vocabulary based on note content. Returns `{tags: [{name, confidence}]}`. |
| New endpoint | `POST /api/ai/suggest-project` -- given a note ID, uses Ollama to classify which existing project the note best fits based on content similarity. Returns `{projects: [{id, name, confidence}]}`. |
| New AI task types | `suggest_tags` and `suggest_project` added to the task queue (background priority). |
| Review item schema | `{type: "orphan" | "untagged" | "inbox" | "similar_pair" | "stale", note_id: string, note_title: string, note_snippet: string, suggestions: [{action, target, reason}], created_at: string}` |

### Frontend Changes

| Component | Details |
|-----------|---------|
| New route `/review` | Knowledge gardening page. |
| `ReviewQueue` component | Card-based queue. Each card shows: note title, preview snippet, reason for surfacing (e.g., "This note has no connections"), and context-appropriate action buttons. Cards animate out on action/dismiss. |
| Action buttons per type | **Orphan**: "Link to [suggestion]" (one-click insert wikilink), "Skip" / **Untagged**: "Add [suggested tag]" (one-click), "Add custom tag" (input), "Skip" / **Inbox**: "Move to [suggested project]" (one-click), "Move to..." (dropdown), "Skip" / **Similar pair**: "Link these" (insert mutual wikilinks), "Merge" (open both in tabs), "They're different" / **Stale**: "Review note" (navigate to editor), "Looks good" |
| Progress summary | Session stats bar at top: "8 notes reviewed -- 3 links created -- 2 tags added -- 1 project assigned". Persists for the current session. |
| Sidebar nav item | "Garden" navigation item with a badge showing count of items needing attention. Badge count fetched on app init and refreshed periodically (every 5 minutes) or on `note.changed` WebSocket events. |
| `reviewStore` (Zustand) | `queue: ReviewItem[]`, `stats: {reviewed, linked, tagged, moved}`, `isLoading`, `fetchQueue()`, `dismissItem(id)`, `applyAction(id, action)` |
| Empty state | "Your garden is tended" message with a decorative illustration when queue is empty. |

### Implementation Steps

1. Backend: `POST /api/ai/suggest-tags` endpoint + AI task type
2. Backend: `POST /api/ai/suggest-project` endpoint + AI task type
3. Backend: `GET /api/review/queue` aggregation endpoint
4. Frontend: `reviewStore` + API client methods
5. Frontend: `/review` page with `ReviewQueue` component
6. Frontend: Action handlers (link, tag, move, dismiss) with optimistic UI
7. Frontend: Progress summary bar
8. Frontend: Sidebar "Garden" nav item with badge

---

## Feature 4: Smart Command Palette (The Power-User Accelerator)

### Motivation

The current command palette has 6 fixed navigation commands. Obsidian's has 100+. For keyboard-driven users, the command palette is the primary interaction surface. It should be the fastest way to do anything in Seam: find a note, run a command, navigate to a project, or trigger an AI action. Currently it is a navigation menu; it should be a command center.

### Backend Changes

None -- uses existing search endpoints and note API.

### Frontend Changes

| Component | Details |
|-----------|---------|
| Multi-mode input | **Default (empty query)**: show recent notes (last 10 opened). **Typing without prefix**: fuzzy note title search via FTS. **`>` prefix**: command search. **`#` prefix**: tag filter (navigate to `/?tag=...`). **`@` prefix**: project navigation. Mode indicator shown as a subtle label in the input. |
| Recent notes tracking | Store last 10 opened note IDs + titles in `localStorage` (key: `seam_recent_notes`). Update on every note editor mount. Show as default palette entries with clock icon. |
| Fuzzy matching algorithm | Replace substring filter with fzf-style matching: characters must appear in order but not consecutively. Highlight matched characters in results. Score by: consecutive matches > word-start matches > other. |
| Expanded command registry | Central `commandRegistry.ts` file. Each command: `{id, label, shortcut?, icon, action: () => void, when?: () => boolean}`. `when` predicate controls contextual visibility (e.g., "Delete note" only shows on a note editor page). |
| New commands to register | New note (`Cmd+N`), Search (`/`), Graph, Timeline, Ask Seam, Garden/Review, Today (`Cmd+.`), Toggle sidebar (`Cmd+\`), Toggle right panel, Toggle view mode (editor/split/preview), Toggle zen mode (`Cmd+Shift+Z`), Duplicate current note, Delete current note, Export current note as markdown, Insert template (opens submenu), AI: Expand selection, AI: Summarize selection, AI: Extract actions, Open settings, Reindex embeddings, all projects as individual entries. |
| Visual improvements | Results grouped by category (Recent / Notes / Commands / Projects / Tags). Matched characters highlighted with accent color. Keyboard shortcut badges right-aligned. Active section header. |
| Note search integration | When typing without prefix, debounced (150ms) FTS call via `searchFTS`. Results show title + truncated snippet. Enter navigates to the note. |

### Implementation Steps

1. Frontend: `commandRegistry.ts` with all command definitions
2. Frontend: Recent notes tracking in `localStorage`
3. Frontend: Fuzzy matching algorithm (standalone utility)
4. Frontend: Multi-mode input parsing (prefix detection, mode switching)
5. Frontend: Note search mode with FTS integration
6. Frontend: Visual refresh (grouping, match highlighting, shortcut badges)
7. Frontend: Register all new commands with `when` predicates

---

## Feature 5: Connection Status + Local Draft Safety (The Trust Foundation)

### Motivation

Seam's identity is "local-first," but the web client has zero offline resilience. If the server restarts while a user is mid-sentence, the debounced save may never fire. There is no connection indicator, no draft cache, no tab-close warning. The WebSocket reconnects silently with exponential backoff, but nothing tells the user their real-time features are degraded. For a tool people put their thinking into, data safety is trust, and trust is retention.

### Backend Changes

None.

### Frontend Changes

| Component | Details |
|-----------|---------|
| WS client state events | Extend `ws.ts` to emit typed state change events: `connected`, `disconnected`, `reconnecting`. Add a `subscribe(callback)` pattern for state changes (separate from message subscriptions). |
| `useConnectionStatus` hook | Subscribes to WS state events. Returns `{status: 'connected' | 'reconnecting' | 'offline', lastConnectedAt: Date | null}`. |
| `ConnectionStatus` component | Small indicator in sidebar footer (next to username). Green dot = connected, amber pulsing dot = reconnecting, red dot = offline. Tooltip shows: status text, last connected time, reconnect attempt count. On "offline", shows "Edits are saved locally" reassurance text. |
| `beforeunload` handler | In `NoteEditorPage`, register `window.addEventListener('beforeunload', handler)` when the editor has unsaved changes (`unsaved` state is already tracked). Prevents accidental tab/window close. Clean up on unmount. |
| Local draft persistence | On every editor change (title or body), write `{noteId, title, body, savedAt: Date.now()}` to `localStorage` under key `seam_draft_${noteId}`. Clear the key on successful API save confirmation (after the debounced save resolves). Cap total draft storage at 20 entries (evict oldest). |
| Draft restoration | On `NoteEditorPage` mount, after fetching the note from the server, check `localStorage` for a draft. If a draft exists and its `savedAt` is newer than the server's `updated_at`, show a banner: "Local draft found (saved X minutes ago). [Restore] [Discard]". Restore loads the draft into the editor; Discard deletes the localStorage entry. |
| Offline queue (stretch) | `localStorage`-based queue of failed write operations (note create, update, tag, project move). On WS `connected` event, replay queued operations in order. Show "X pending changes syncing..." indicator during replay. |
| `uiStore` additions | `connectionStatus: string`, `setConnectionStatus(status)` |

### Implementation Steps

1. Frontend: WS client state change event system
2. Frontend: `useConnectionStatus` hook
3. Frontend: `ConnectionStatus` component in sidebar footer
4. Frontend: `beforeunload` handler in `NoteEditorPage`
5. Frontend: Local draft write-on-change logic in editor
6. Frontend: Draft restoration banner on editor mount
7. Frontend: Draft storage cap and eviction
8. Frontend: Offline queue with replay (stretch goal)

---

## Feature 6: Slash Commands (Editor Enhancement)

### Motivation

Formatting is toolbar-only (besides Cmd+B/I). Slash commands let users format and insert content without leaving the keyboard or reaching for the mouse, following the pattern established by Notion, Logseq, and many modern editors. This is especially important for the capture flow -- users should be able to structure their thoughts as they type.

### Backend Changes

None.

### Frontend Changes

| Component | Details |
|-----------|---------|
| CodeMirror slash extension | New extension that detects `/` typed at the start of a line or after a newline character. Triggers the slash menu. Typing additional characters after `/` filters the menu. Backspacing past `/` closes it. |
| `SlashMenu` component | Floating menu positioned below the cursor line (similar to the existing wikilink autocomplete dropdown). Keyboard navigable: arrow keys to move, Enter to select, Escape to dismiss. Shows command icon + label + description. |
| Slash command definitions | `/h1`, `/h2`, `/h3` -- insert heading prefix (`#`, `##`, `###`) / `/bold` -- wrap selection or insert `**bold**` / `/italic` -- wrap selection or insert `*italic*` / `/code` -- insert fenced code block / `/list` -- insert `- ` bullet / `/checklist` -- insert `- [ ] ` checkbox / `/link` -- insert `[[` and trigger wikilink autocomplete / `/template` -- show submenu of available templates / `/divider` -- insert `---` / `/date` -- insert today's date (`YYYY-MM-DD`) / `/time` -- insert current time (`HH:MM`) / `/quote` -- insert `> ` blockquote / `/table` -- insert a markdown table skeleton |
| Template submenu | When `/template` is selected, the slash menu transitions to show available templates (fetched from `GET /api/templates/`). Selecting one inserts the template body at the cursor position (with variable substitution applied). |
| Discovery hint | On first use (tracked in `localStorage`), show a subtle hint in the editor: "Tip: Type / for commands" that dismisses on click or after 5 seconds. |

### Implementation Steps

1. Frontend: Slash command trigger detection (CodeMirror extension)
2. Frontend: `SlashMenu` component with positioning and keyboard navigation
3. Frontend: Command definitions with insertion logic
4. Frontend: Template submenu integration
5. Frontend: First-use discovery hint

---

## Feature 7: Focus/Zen Mode (Writing Enhancement)

### Motivation

The editor has three view modes (editor/split/preview) but no distraction-free writing mode. When users are in deep writing flow, all the chrome -- sidebar, right panel, toolbar, metadata -- competes for attention. A zen mode strips everything away and centers the writing surface, turning Seam into a focused writing tool when needed.

### Backend Changes

| Change | Details |
|--------|---------|
| Settings allowlist | Add `zen_mode_typewriter: "true" | "false"` to the settings validation allowlist to persist the typewriter scrolling preference. |

### Frontend Changes

| Component | Details |
|-----------|---------|
| Zen mode toggle | Toolbar button (Maximize2 icon from Lucide) and keyboard shortcut `Cmd+Shift+Z`. Available only on the note editor page. |
| Zen mode layout | Hides: sidebar, right panel, toolbar, note metadata section. Centers the editor with `max-width: 720px` and generous horizontal padding. Title input remains visible (simplified styling). Word count bar remains at bottom (simplified). |
| Floating exit control | Small, semi-transparent "Exit" button in the top-right corner. Fades in on mouse movement, fades out after 2 seconds of inactivity. `Escape` also exits zen mode. |
| Typewriter scrolling | Optional (toggled via a small gear icon on the floating control or via settings). Keeps the active cursor line vertically centered in the viewport as the user types. Implemented as a CodeMirror extension that scrolls the editor on cursor movement. |
| Transition animation | Entering zen mode: sidebar slides left, right panel slides right, toolbar fades up, editor container smoothly centers and expands. Reverse on exit. Duration: 350ms with the standard ease-out curve. Respects `prefers-reduced-motion`. |
| `uiStore` additions | `zenMode: boolean`, `toggleZenMode()`, `setZenMode(boolean)` |
| Keyboard handling | `Cmd+Shift+Z` toggles. `Escape` exits (only when zen mode is active; does not conflict with other Escape uses since zen mode hides those UI elements). |
| Persistence | Zen mode is not persisted across page loads (always starts off). Typewriter scrolling preference is persisted via `settingsStore`. |

### Implementation Steps

1. Frontend: `uiStore` zen mode state + toggle
2. Frontend: Zen mode CSS transitions (hide sidebar, right panel, toolbar; center editor)
3. Frontend: Floating exit control component
4. Frontend: Toolbar button + `Cmd+Shift+Z` shortcut
5. Frontend: Typewriter scrolling CodeMirror extension
6. Frontend: Settings integration for typewriter preference

---

## Feature 8: Bulk Operations (Productivity)

### Motivation

With a growing knowledge base, one-at-a-time operations become tedious. There is no way to select multiple notes for batch tagging, project moves, or deletion. This is table-stakes for a productivity tool once users have more than a few dozen notes.

### Backend Changes

| Change | Details |
|--------|---------|
| New endpoint | `PATCH /api/notes/bulk` -- accepts `{note_ids: string[], action: "add_tag" | "remove_tag" | "move" | "delete", params: {tag?: string, project_id?: string}}`. Validates all note IDs belong to the requesting user. Executes within a single SQLite transaction. Returns `{success: number, failed: number, errors?: string[]}`. |
| Input validation | Max 100 note IDs per request. All standard input validation (tag names, project ID existence, path safety). |

### Frontend Changes

| Component | Details |
|-----------|---------|
| Selection mode trigger | `Cmd+Click` on a note card toggles its selection and enters selection mode. Alternatively, a "Select" button in the page header enters selection mode (all cards show checkboxes). |
| Note card changes | When in selection mode, each `NoteCard` shows a checkbox on the left. Checked state uses the accent color. Cards have a subtle highlight when selected. |
| Range selection | `Shift+Click` selects all notes between the last selected and the clicked note (standard range-select behavior). |
| Select all / deselect | "Select all" / "Deselect all" buttons in the page header during selection mode. `Cmd+A` selects all visible notes when in selection mode. |
| `BulkActionBar` component | Fixed bottom bar (slides up with animation) when >= 1 note is selected. Contains: selected count ("3 notes selected"), action buttons: "Add tag" (dropdown with existing tags + text input for new), "Move to project" (dropdown of projects + Inbox), "Delete" (destructive, with confirmation modal). "Cancel" exits selection mode. |
| `noteStore` additions | `selectedNoteIds: Set<string>`, `isSelectionMode: boolean`, `toggleNoteSelection(id)`, `selectAll(ids)`, `clearSelection()`, `bulkAction(action, params)` |
| API client addition | `bulkUpdateNotes(noteIds, action, params)` |
| Post-action behavior | After bulk action completes, show toast with result ("3 notes moved to Project X"), clear selection, refresh note list. |
| Escape handling | `Escape` exits selection mode (clears all selections). |

### Implementation Steps

1. Backend: `PATCH /api/notes/bulk` endpoint with transaction wrapping
2. Frontend: API client method
3. Frontend: Selection state in `noteStore`
4. Frontend: Note card checkbox rendering in selection mode
5. Frontend: `BulkActionBar` component with action dropdowns
6. Frontend: Range selection (`Shift+Click`) and `Cmd+A`
7. Frontend: Post-action toast and list refresh

---

## Feature 9: Note Version History (Safety Net)

### Motivation

For a writing tool, accidental edits and content loss are inevitable. The `content_hash` column already exists in the notes table, proving change-detection infrastructure is in place. A lightweight version history completes the safety story -- users can write fearlessly knowing they can always go back.

### Backend Changes

| Change | Details |
|--------|---------|
| New migration | `migrations/user/009_note_versions.sql` (or next available number). Creates `note_versions` table. |
| Table schema | `note_versions (id TEXT PRIMARY KEY, note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE, version INTEGER NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, content_hash TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(note_id, version))` |
| Version capture | In `note.Service.Update()`, after a successful update where `content_hash` has changed, insert a new version row. Version number = max existing version + 1 (or 1 if none). |
| Retention | Cap at 50 versions per note. On insert, if count exceeds 50, delete the oldest rows. Configurable via server config. |
| New endpoint | `GET /api/notes/{id}/versions?limit=20&offset=0` -- list versions (id, version, title, content_hash, created_at). Most recent first. Returns `X-Total-Count`. |
| New endpoint | `GET /api/notes/{id}/versions/{version}` -- get full version content (title + body). |
| New endpoint | `POST /api/notes/{id}/versions/{version}/restore` -- copies the version's title and body back to the note (which itself creates a new version). Returns the updated note. |

### Frontend Changes

| Component | Details |
|-----------|---------|
| Version history panel | New collapsible section in the editor right panel, below metadata. Header: "History" with version count badge. Lists versions as compact rows: relative timestamp ("2 hours ago"), version number, truncated title delta if title changed. |
| Diff view | Clicking a version row expands an inline diff view below it. Uses a lightweight diff library (`diff` or `jsdiff` npm package) to show additions (green) and deletions (red) compared to the current note content. Alternatively, a side-by-side view in a modal for larger diffs. |
| Restore action | Each version row has a "Restore" button (with confirmation: "Restore to version from [timestamp]? This will create a new version with the current content before restoring."). On confirm, calls restore endpoint, refreshes editor content. |
| Version indicator | Small text in editor metadata: "Version 12" or "12 versions" linking to the history section. |
| API client additions | `listVersions(noteId, limit?, offset?)`, `getVersion(noteId, version)`, `restoreVersion(noteId, version)` |
| Performance | Versions list loads lazily (only when the History section is expanded). Diff computation happens client-side after fetching the version body. |

### Implementation Steps

1. Backend: Migration for `note_versions` table
2. Backend: Version capture logic in note update service
3. Backend: Retention/cleanup logic (cap at 50)
4. Backend: `GET /api/notes/{id}/versions` endpoint
5. Backend: `GET /api/notes/{id}/versions/{version}` endpoint
6. Backend: `POST /api/notes/{id}/versions/{version}/restore` endpoint
7. Frontend: API client methods
8. Frontend: Version history panel in editor right panel
9. Frontend: Diff view component
10. Frontend: Restore flow with confirmation

---

## Implementation Priority Matrix

| Priority | Feature | Effort | Impact | Dependencies |
|----------|---------|--------|--------|--------------|
| **P0** | 2. Wikilink Hover Preview | Medium | Very High | None |
| **P0** | 5. Connection Status + Draft Safety | Medium | High | None |
| **P1** | 1. Daily Notes | Medium | Very High | None |
| **P1** | 4. Smart Command Palette | Medium | High | None |
| **P2** | 3. Knowledge Gardening | Large | Very High | AI endpoints exist |
| **P2** | 6. Slash Commands | Small | Medium | None |
| **P2** | 7. Focus/Zen Mode | Small | Medium | None |
| **P3** | 8. Bulk Operations | Medium | Medium | Backend endpoint |
| **P3** | 9. Note Version History | Large | Medium | DB migration |

**P0** items can be built in parallel and address the most fundamental UX gaps (broken connection flow, data safety). **P1** items create the daily-use habit loop and power-user efficiency. **P2** items leverage AI differentiation and polish the writing experience. **P3** items are important but less urgent productivity and safety features.

### Suggested Build Order

**Sprint 1 (P0 -- Foundation):** Features 2 + 5 in parallel. Two developers can work on these simultaneously with zero overlap.

**Sprint 2 (P1 -- Retention):** Features 1 + 4 in parallel. Daily Notes creates the habit; Smart Palette creates the efficiency.

**Sprint 3 (P2 -- Differentiation):** Feature 3 (Knowledge Gardening) is the largest and most impactful. Features 6 + 7 are small and can fill gaps.

**Sprint 4 (P3 -- Completeness):** Features 8 + 9. Bulk operations and version history round out the productivity story.
