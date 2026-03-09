# Phase 2 -- Frontend Polish and UX

Status: **Draft (Revised)**
Last updated: 2026-03-09

---

## Overview

Phase 1 delivered a functionally complete frontend: all routes, stores, API integration, design tokens, and core features are wired up. Phase 2 focuses on making Seam feel *great* -- polishing the visual experience, fixing UX rough edges, adding missing user-facing features, and improving day-to-day usability.

### Guiding principles

1. **Feel before features.** A smooth, responsive, tactile interface earns trust. Polish what exists before adding new capabilities.
2. **Remove friction.** Every extra click, confirmation dialog, or missing feedback loop is a papercut. Find them and fix them.
3. **Respect the design system.** FE_DESIGN.md is the source of truth. Where current code deviates, bring it back in line.
4. **Ship incrementally.** Each task should produce a visible improvement. No large refactors that leave the app broken mid-way.

---

## Audit of Current State

### What works well

- All 8 routes exist and render correctly
- Design tokens (colors, typography, spacing, radii) are defined in `variables.css` and used throughout
- Sidebar navigation with collapsible state (CSS width transition), inline search with keyboard nav, project/tag sections
- Note editor with CodeMirror, split/preview modes, toolbar, 1s debounced auto-save, flush-on-unmount
- Capture modal with text/URL/voice/template support, CSS enter animation
- Command palette (Cmd+K) with navigation, CSS enter animation, focus trap, arrow key nav
- Knowledge graph with Cytoscape, filters, minimap, `prefers-reduced-motion` support
- Ask Seam chat with WebSocket streaming (token-by-token), HTTP fallback, 60s stream timeout
- Toast notification system with CSS enter animation
- **Page transitions already implemented** -- Layout.tsx wraps `<Outlet>` in `AnimatePresence` + `motion.div` (fade + y:4px, 250ms, correct easing)
- **Note card hover animation already implemented** -- 2px left border "seam line" transitions smoothly via CSS `transition` on `border-color` at `--duration-fast`
- **Right panel stagger animations** -- Related Notes and Backlinks items use `motion.div` with stagger (opacity 0->1, y 4->0, 30ms delay)
- **Right panel project assignment** -- `<select>` dropdown in the right panel allows moving notes between projects
- **Ask Seam citation title resolution** -- AskPage.tsx resolves note IDs to titles client-side via `getNote()` with a module-level cache (falls back to truncated IDs during loading)

### What needs work

| Area | Issue |
|---|---|
| **Loading states** | InboxPage, ProjectPage show plain "Loading..." text. App.tsx auth check shows a bare `<div>` with inline styles. NoteEditorPage uses a Loader2 spinner (better but not a skeleton). No skeleton placeholders anywhere. |
| **Error handling UX** | Errors are stored in Zustand but rarely surfaced to the user visually. Many catches are silent. No retry affordances. |
| **Exit animations** | Modals (CaptureModal, CommandPalette) and toasts have CSS enter animations but no exit animations -- elements vanish instantly on unmount. FE_DESIGN.md Section 8 specifies modal dismiss and toast exit animations. |
| **Stagger animations** | Note card lists (InboxPage, ProjectPage, SearchPage) appear all at once. FE_DESIGN.md specifies staggered entrance. Only the right panel's Related Notes/Backlinks have stagger. |
| **Responsive behavior** | Only 2 `@media` breakpoints exist (640px, 1024px) affecting sidebar/layout only. No responsive CSS for note editor, search, ask, graph, or timeline pages. Missing: hamburger menu below 640px, right panel auto-show above 1440px, editor single-view-only on mobile. |
| **No settings page** | Logging out is hidden behind a gear icon. No account settings, no display preferences, no keyboard shortcut reference. |
| **Note editor UX** | No title editing inline (title is read-only `<h1>` in preview). No word/character count. `window.confirm()` for delete (not a styled modal). The "More" menu only has "Delete". |
| **Sidebar polish** | Tags section is plain. No project context menu (rename/delete from sidebar). Section collapse state (`projectsExpanded`, `tagsExpanded`) resets on page refresh (local `useState`, no persistence). Content items don't fade during sidebar width transition. |
| **Search UX** | Semantic search results show a percentage badge (`{Math.round(score * 100)}%`) but not the visual relevance bar from FE_DESIGN.md Section 7.4. |
| **Ask Seam UX** | Citation titles are resolved client-side with N+1 `getNote()` calls (slow, shows truncated IDs during loading). No conversation history persistence. No "clear conversation" button. No suggested starter questions. |
| **Graph UX** | No tooltip on node hover showing note preview. No "center on node" action. Filter panel has no animation. |
| **Timeline** | Basic implementation. Needs sticky date headers, smooth scrolling, and the "today" indicator from FE_DESIGN.md. |
| **Accessibility** | Skip-to-content link exists. ARIA labels are generally present. But: no `aria-live` regions for search results or streaming chat. Focus management in modals could be tighter. No visible keyboard shortcut hints in the UI. |
| **Performance** | Note list renders all notes at once (no virtualization). No lazy loading of routes -- all pages are eagerly imported. CodeMirror extensions array recreated on every render. |
| **`window.confirm()` usage** | Used in NoteEditorPage (1 place: delete) and CaptureModal (3 places: template overwrite, Escape with unsaved content, backdrop click with unsaved content). Should be replaced with styled ConfirmModal. |

---

## Task Breakdown

### Track A: Visual Polish and Motion

These tasks bring the UI in line with FE_DESIGN.md and add the tactile feel that makes Seam distinctive.

#### ~~A1. Page transition animations~~ DONE

Already implemented in `Layout.tsx`:
- `AnimatePresence mode="wait"` + `motion.div` keyed by `location.pathname`
- Fade + y:4px, 250ms, easing `[0.16, 1, 0.3, 1]`
- Matches FE_DESIGN.md Section 8 spec exactly

No further work needed.

#### A2. Modal and palette exit animations

Enter animations already exist as CSS `@keyframes` in both CaptureModal and CommandPalette. **Only exit animations are missing** -- both components return `null` when closed, causing instant unmount.

**Implementation approach**: wrap each modal's root element with `AnimatePresence` from `motion/react` so that exit animations run before unmount. The CSS `@keyframes` enter animations should be replaced with `motion` initial/animate props for consistency (enter and exit in one system).

- CaptureModal: exit with `opacity 1->0` + `translateY 0->4px`, `--duration-normal` (250ms), `--ease-in`
- CommandPalette: exit with `opacity 1->0` + `scale 1->0.98`, `--duration-normal`, `--ease-in`
- Backdrop for both: exit `opacity 1->0`, `--duration-normal`
- Ensure focus management still works with animated mount/unmount -- `AnimatePresence` `onExitComplete` callback should handle focus restoration
- **Also address**: CaptureModal uses `window.confirm()` in 3 places. Replace with ConfirmModal from B3.

#### A3. Note card stagger animation
- InboxPage, ProjectPage, SearchPage: stagger note card entrance
- Each card fades in + translateY 4px->0, 30ms delay between items
- Use `motion` `staggerChildren` (same pattern already used in NoteEditorPage right panel for Related Notes and Backlinks)
- Ensure no layout shift during animation

#### A4. Toast enter/exit animations

Toast enter animation already exists as CSS `@keyframes toastIn` (fade + slide-up, `--duration-normal`, `--ease-out`). **Exit animation is missing** -- toasts are removed from the array instantly, causing immediate unmount.

- Add exit: `opacity 1->0`, `--duration-fast` (150ms), `--ease-in`
- Stack animation: existing toasts slide up smoothly when new one appears
- Implementation: wrap toast list in `AnimatePresence` from `motion/react`, convert each toast to `motion.div` with layout animation. Replace CSS `@keyframes toastIn` with `motion` initial/animate/exit for consistency.
- Note: max 3 toasts visible (current `.slice(-2)` before adding -- verify this logic is correct for 3-toast limit)

#### A5. Loading skeletons
- Replace all "Loading..." text with skeleton placeholders
- Skeleton elements: pulsing rectangles matching the shape of the content they replace
- Pages that need skeletons:
  - **InboxPage** -- shows plain "Loading..." text
  - **ProjectPage** -- shows plain "Loading..." text
  - **NoteEditorPage** -- shows Loader2 spinner (upgrade to content-shaped skeleton)
  - **App.tsx auth check** -- shows bare `<div>` with inline styles (upgrade to full-page skeleton or splash)
  - **SearchPage** -- needs skeleton during search execution
  - **GraphPage** -- needs skeleton during graph data load
  - **TimelinePage** -- needs skeleton during timeline data load
  - **AskPage** -- needs skeleton when loading conversation history (after B7)
- Skeleton component: `<Skeleton width height borderRadius />` with CSS pulse animation
- Color: `--bg-elevated` with `--bg-surface` pulse overlay

#### A6. Relevance bar for semantic search
- SearchPage: when in semantic mode, render a thin horizontal bar below each result
- Width proportional to score (0-100%), color `--accent-tertiary`
- Height: 3px, border-radius: `--radius-full`
- Currently shows `{Math.round(result.score * 100)}%` as a text badge -- keep the badge but add the visual bar
- Matches FE_DESIGN.md Section 7.4

#### ~~A7. Note card hover refinement~~ DONE

Already implemented in `NoteCard.tsx`:
- `border-left: 2px solid transparent` transitions to `border-left-color: var(--accent-primary)` on hover
- Background transitions to `--bg-elevated` via CSS `transition: background var(--duration-fast) var(--ease-standard), border-color var(--duration-fast) var(--ease-standard)`
- Matches FE_DESIGN.md Section 6.2

No further work needed.

#### A8. Sidebar transition smoothness

Sidebar width collapse/expand already has a CSS `transition: width var(--duration-normal) var(--ease-out)`. **What's missing:**
- Content items should fade out before width shrinks, fade in after width expands (sequenced animation)
- Currently content disappears instantly via conditional rendering when `sidebarCollapsed` toggles
- Implementation: use `motion` to orchestrate content fade (opacity 0, 100ms) before sidebar width shrinks, and content fade-in after width expands

#### A9. Right panel transition smoothness
- Right panel (in NoteEditorPage) slide in/out should use CSS transform (not display toggle)
- Animate `translateX(100%)` -> `translateX(0)` on open, reverse on close
- Duration: 200ms, easing: `--ease-out`

### Track B: UX Improvements and Missing Features

#### B1. Settings page
Create `/settings` route with the following sections:

- **Account**: username (read-only), email (read-only), change password form (see B1-BE-auth), logout button
- **Appearance**: editor default view mode (editor/split/preview), default right panel state (open/closed), sidebar default state (expanded/collapsed)
- **Keyboard shortcuts**: read-only reference card showing all shortcuts from FE_DESIGN.md Section 11
- **About**: app version (injected at build time via Vite `define` from `package.json` version field), link to docs

Settings are **server-persisted** in the per-user SQLite database so they survive device/browser changes. See B1-BE below for backend work. The sidebar gear icon should navigate to `/settings` instead of logging out directly.

**Frontend:**
- New `useSettingsStore` (Zustand) -- fetches settings on login, caches in memory, writes back on change
- `useSettingsStore` is the **source of truth** for persisted UI preferences. On load, it fetches from the server and hydrates. `uiStore` continues to own ephemeral UI state (command palette open, capture modal open, etc.) but must read `sidebarCollapsed` and `rightPanelOpen` from `useSettingsStore` instead of managing its own copies. Remove those fields from `uiStore` and have components read from `useSettingsStore` directly.
- `editorViewMode` is currently **local state in NoteEditorPage** (not in uiStore). Lift it to `useSettingsStore` so the default persists across sessions.
- Optimistic updates: apply setting immediately in the UI, PUT to server in background, revert on failure
- Settings shape: `{ editor_view_mode, right_panel_open, sidebar_collapsed }` (extensible JSON)
- **Default values** (used when a user has no saved settings): `editor_view_mode: "split"`, `right_panel_open: true`, `sidebar_collapsed: false`, `sidebar_projects_expanded: true`, `sidebar_tags_expanded: true`

#### B1-BE. Settings backend
New `internal/settings` package following existing handler/service/store pattern.

**Migration** (`migrations/user/003_settings.sql`):
```sql
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

Key-value design rather than a single JSON blob -- simpler partial updates, no read-modify-write races.

**Endpoints** (mounted at `/api/settings`, protected by auth middleware):
- `GET /api/settings` -- returns all settings as `{ "editor_view_mode": "split", ... }`
- `PUT /api/settings` -- accepts a partial JSON object, upserts each key
- `DELETE /api/settings/:key` -- resets a single setting to default

**Store interface**:
```go
type Store interface {
    GetAll(ctx context.Context) (map[string]string, error)
    Set(ctx context.Context, key, value string) error
    Delete(ctx context.Context, key string) error
}
```

**Service**: thin layer validating keys against an allowlist and enforcing value constraints.

**Allowlist and constraints:**

| Key | Valid values | Default |
|---|---|---|
| `editor_view_mode` | `editor`, `split`, `preview` | `split` |
| `right_panel_open` | `true`, `false` | `true` |
| `sidebar_collapsed` | `true`, `false` | `false` |
| `sidebar_projects_expanded` | `true`, `false` | `true` |
| `sidebar_tags_expanded` | `true`, `false` | `true` |

Any key not in this list is rejected with 400. Extend this table when new settings are added.

**API client** additions (`web/src/api/client.ts`):
- `getSettings(): Promise<Record<string, string>>`
- `updateSettings(settings: Record<string, string>): Promise<void>`

#### B1-BE-auth. Account management backend
Extend `internal/auth` to support password changes and user profile retrieval.

**No migration needed.** The `email` column already exists in `migrations/server/001_users.sql` as `email TEXT NOT NULL UNIQUE`. The `User` struct and all queries in `internal/auth/store.go` already read/write the email field. Registration already accepts email.

**New endpoints** (added to existing `/api/auth` routes):
- `PUT /api/auth/password` -- accepts `{ current_password, new_password }`, validates current password, hashes and stores new one. Returns 204 on success, 400 if current password wrong or new password too short (min 8 chars).
- `GET /api/auth/me` -- returns `{ id, username, email }` for the authenticated user (for the Settings account section).
- `PUT /api/auth/email` -- accepts `{ email }`, validates format, updates. Returns 204.

**Frontend**: Settings account section calls `getMe()` to populate fields, `changePassword()` and `updateEmail()` for mutations.

**API client** additions:
- `getMe(): Promise<{ id: string, username: string, email: string }>`
- `changePassword(currentPassword: string, newPassword: string): Promise<void>`
- `updateEmail(email: string): Promise<void>`

#### B2. Note editor: inline title editing
- Title should be editable directly at the top of the editor pane (above the CodeMirror instance)
- Currently the title is only shown as a read-only `<h1>` in the preview pane
- Large input field, `--font-display` 600, `--font-size-xl`
- Save behavior: **debounced** (same 1s debounce as body changes). Typing updates a local ref; the debounce timer fires `updateNote` with `{ title }`. No explicit save on blur or Enter -- the debounce handles it naturally. If the user navigates away before the debounce fires, flush immediately (same pattern as body auto-save in the existing `handleChange` / cleanup effect).
- The `UpdateNoteReq` type already supports `title?: string` -- the API is ready
- On initial create (title is "Untitled"): auto-focus title, select all
- Consider: title changes may need to update frontmatter on disk. Verify backend behavior when `updateNote` receives a `title` field.

#### B3. Note editor: enhanced "More" menu
- Add menu items: "Duplicate note", "Copy link" (copies `/notes/{id}` to clipboard), "Export as Markdown" (downloads `.md` file)
- **Note**: "Move to project" already exists in the right panel as a `<select>` dropdown. Add it to the "More" menu as well for discoverability (submenu with project list, "Inbox" as first option for `project_id: null`).
- Replace `window.confirm()` delete dialog with a styled ConfirmModal
- **Also replace** `window.confirm()` in CaptureModal (3 occurrences: template overwrite, Escape with unsaved, backdrop click with unsaved)
- Add "Open in new tab" option
- Style the dropdown menu properly with dividers between groups

#### B4. Note editor: word count and metadata bar
- Bottom bar (below editor, above save status): show word count, character count, reading time estimate
- Format: "324 words / 1,842 chars / 2 min read"
- `--font-ui` 300, `--font-size-xs`, `--text-tertiary`
- Only visible in editor or split mode (not preview-only)

#### B5. Note editor: tag editing in right panel
- Current: tags are displayed as read-only `<span>` elements with `#{tag}` styling, no editing
- Add: inline tag input at the bottom of the tags section
- Type a tag name, press Enter or comma to add
- Click the X on a tag pill to remove
- Tags auto-complete from existing tags (use `tags` from uiStore's `fetchTags()`)
- Updates note via `updateNote` with debounce

#### B6. Ask Seam: server-side citation titles
- Currently AskPage.tsx resolves citation titles **client-side** via N+1 `getNote()` calls with a module-level cache. This is slow (shows truncated IDs while resolving) and wasteful.
- **Backend change**: modify `ChatResult` in `internal/ai/chat.go` from `Citations []string` to a struct with `ID` and `Title` fields. The `retrieveContext` method already fetches note data (for building context) -- add title to the citation collection instead of discarding it.
- Update the WebSocket `chat.done` payload in `cmd/seamd/main.go` to send `[{ "id": "...", "title": "..." }]` instead of `["id1", "id2"]`.
- Update `ChatResult` type in `web/src/api/types.ts` to match: `citations: Array<{ id: string, title: string }>`.
- **Frontend**: simplify AskPage.tsx citation rendering -- remove the client-side title resolution cache (`noteTitleCache`, `citationTitles` state, `fetchCitationTitles`) and render titles directly from the response.
- Each citation badge: note title (truncated to ~30 chars), pill style per FE_DESIGN.md Section 7.5. Clicking a citation navigates to `/notes/{id}`.
- Add a "clear conversation" button in the header area

#### B7. Ask Seam: starter suggestions and conversation persistence
- When chat is empty, show 3-4 suggested questions as clickable chips
- Examples: "What are my recent notes about?", "Summarize my [project] notes", "Find connections between..."
- Chips: `--bg-surface` background, `--border-default` border, `--text-secondary` text
- Clicking a chip fills the input and submits
- Conversations are persisted server-side (see B7-BE) and restored on page visit
- Sidebar or header shows a "New conversation" button to start fresh

#### B7-BE. Chat history backend
Server-side persistence for Ask Seam conversations.

**Migration** (`migrations/user/003_chat_history.sql` -- or `004` if settings migration uses `003`):
```sql
CREATE TABLE IF NOT EXISTS conversations (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content         TEXT NOT NULL,
    citations       TEXT,  -- JSON array of {id, title} objects, nullable
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation
    ON messages(conversation_id, created_at);
```

**Endpoints** (mounted at `/api/chat`, protected):
- `POST /api/chat/conversations` -- create a new conversation, returns `{ id, title, created_at }`
- `GET /api/chat/conversations` -- list conversations, most recent first. Pagination via `?limit=20&offset=0` (defaults: limit 20, max 100, offset 0). Returns `X-Total-Count` header consistent with other list endpoints.
- `GET /api/chat/conversations/:id` -- get conversation with all messages
- `DELETE /api/chat/conversations/:id` -- delete a conversation
- `POST /api/chat/conversations/:id/messages` -- append a message (accepts `{ role, content, citations }`)

**Store interface**:
```go
type Store interface {
    CreateConversation(ctx context.Context, id, title string) error
    ListConversations(ctx context.Context, limit, offset int) ([]Conversation, int, error) // returns total count
    GetConversation(ctx context.Context, id string) (*Conversation, []Message, error)
    DeleteConversation(ctx context.Context, id string) error
    AddMessage(ctx context.Context, msg Message) error
}
```

**Auto-title** (server-side, in `AddMessage`): when a message with `role=assistant` is added and the conversation's `title` is empty, the service sets `title` to a truncation of the first `role=user` message in that conversation (first 80 chars, trimmed to last word boundary). This keeps the logic in one place and requires no extra client coordination.

**WebSocket + REST integration -- design decision:**

The current WebSocket `chat.ask` handler in `cmd/seamd/main.go` is **stateless** -- the client sends the full message history each time. Two approaches for adding conversation persistence:

- **Option A (simpler, recommended)**: keep the WebSocket stateless. Client continues sending full history via the existing `history` field. Persistence is purely REST-side: client POSTs user/assistant messages after each exchange. No WebSocket protocol changes needed.
- **Option B**: add `conversation_id` to the WebSocket `chat.ask` payload. Backend loads prior messages from the database and appends them to the AI prompt context. More efficient for long conversations (no need to send full history over the wire) but requires WebSocket handler to depend on the chat store (tighter coupling).

**Recommendation: Option A.** Keeps concerns separated. The WebSocket layer does AI streaming; the REST layer does persistence. Revisit Option B only if history payloads become large enough to matter (unlikely for a local-first single-user system).

**Integration flow (Option A):**
1. User opens AskPage. Client calls `GET /api/chat/conversations?limit=1` to load the most recent conversation. If one exists, calls `GET /api/chat/conversations/:id` to restore messages. If none, UI starts in empty state with starter suggestions.
2. User sends a message. If no active conversation, client calls `POST /api/chat/conversations` first to create one and stores the returned `id`.
3. Client calls `POST /api/chat/conversations/:id/messages` with `{ role: "user", content }` to persist the user message.
4. Client sends the question via WebSocket with the existing `chat.ask` message type, including full history as today.
5. WebSocket streams the assistant response as before. Client accumulates the streamed tokens locally.
6. Once the stream completes (`chat.done` message received), client calls `POST /api/chat/conversations/:id/messages` with `{ role: "assistant", content: <full response>, citations: [...] }`.
7. If the POST in step 3 or 6 fails, show an error toast ("Failed to save message") but do NOT discard the local message -- the user can still see it in the current session.

**Frontend changes** (`AskPage.tsx`):
- On mount: load most recent conversation (or start new) per flow above
- After each exchange: POST both user message and assistant response per flow above
- "New conversation" button creates a fresh conversation via POST, clears local messages
- Conversation list in a dropdown in the page header showing recent conversations (limit 10), with "View all" linking to a full list

**API client** additions:
- `createConversation(): Promise<Conversation>`
- `listConversations(limit?: number, offset?: number): Promise<{ conversations: Conversation[], total: number }>`
- `getConversation(id: string): Promise<{ conversation: Conversation, messages: Message[] }>`
- `deleteConversation(id: string): Promise<void>`
- `addChatMessage(conversationId: string, message: { role: string, content: string, citations?: Array<{id: string, title: string}> }): Promise<void>`

**Types** additions (`web/src/api/types.ts`):
```ts
export interface Conversation {
  id: string
  title: string
  created_at: string
  updated_at: string
}

export interface ChatHistoryMessage {
  id: string
  conversation_id: string
  role: 'user' | 'assistant'
  content: string
  citations?: Array<{ id: string; title: string }>
  created_at: string
}
```

#### B8. Sidebar: project context menu
- Right-click or click a "..." icon on project row to show context menu
- Options: "Rename", "Delete" (with confirmation), "New note in project"
- "Rename" shows an inline edit input (like the create project flow)
- "Delete" shows a styled modal with two options:
  - "Keep notes" (default): moves all notes to Inbox. Uses `DELETE /api/projects/:id?cascade=inbox` (existing behavior, already the default).
  - "Delete everything": deletes the project and all its notes. Uses `DELETE /api/projects/:id?cascade=delete`.

**No backend changes needed.** The existing `DELETE /api/projects/:id` endpoint already supports `?cascade=inbox` (default) and `?cascade=delete`. Both modes run in a transaction in the service layer (`internal/project/service.go`). The original plan proposed `?cascade=true` but the backend already uses different parameter values -- the frontend just needs to use `?cascade=delete`.

#### B9. Sidebar: collapsible section memory
- Remember which sections (Projects, Tags) are expanded/collapsed across sessions
- Currently managed via `useState(true)` in Sidebar.tsx -- resets on page refresh
- Persist via the settings endpoint (B1-BE): keys `sidebar_projects_expanded`, `sidebar_tags_expanded`
- Read initial values from `useSettingsStore` on mount

#### B10. Improved error feedback
- Surface errors as toast notifications instead of silently catching
- Add error toasts for: note save failure, project create/delete failure, search failure, AI assist failure, voice capture failure
- Toast type: `error` with `--status-error` accent
- Include brief retry suggestion where applicable

### Track C: Responsive and Accessibility

#### C1. Responsive layout implementation
Implement the breakpoint behavior from FE_DESIGN.md Section 5:

- **< 640px**: Sidebar hidden (hamburger toggle), right panel hidden, editor single-view only
- **640-1024px**: Sidebar collapsed to 52px icons, right panel hidden (toggle), editor split or single
- **1024-1440px**: Sidebar full 260px, right panel hidden by default (toggle)
- **> 1440px**: Full layout with right panel visible by default

Currently only 2 `@media` queries exist (in `Layout.module.css` and `Sidebar.module.css`) for the 640px and 1024px sidebar breakpoints. **Missing responsive CSS** for: NoteEditorPage (right panel auto-show, single-view-only on mobile), SearchPage, AskPage, GraphPage, TimelinePage.

Add a fixed-position hamburger menu button (top-left, 48x48px tap target) visible only below 640px. Sidebar slides in as an overlay on mobile with a semi-transparent backdrop (`--bg-overlay`). Tapping the backdrop or navigating to a route closes the overlay. The `sidebarOpen` field in uiStore can serve as the overlay toggle state.

#### C2. Keyboard shortcut discoverability
- Add subtle keyboard shortcut hints next to relevant UI elements
- Sidebar search: show "/" hint inside the input (like GitHub)
- Command palette items: show shortcut on the right side of each row
- Settings page keyboard shortcuts section (from B1)

#### C3. ARIA live regions
- Search results (sidebar and full page): wrap in `aria-live="polite"` so screen readers announce result count changes
- Ask Seam streaming: mark streaming message area as `aria-live="polite"`
- Toast container: ensure `role="status"` and `aria-live="polite"` are set

#### C4. Focus management
- Modal open: focus first interactive element, trap Tab cycling (CaptureModal already does this, CommandPalette already does this -- verify both)
- Modal close: return focus to the element that triggered it (CommandPalette has focus restoration, verify CaptureModal does too)
- Route change: focus main content area (for screen reader users)
- Dropdown menus: arrow key navigation, Escape to close and restore focus

### Track D: Performance

#### D1. Route-level code splitting
- Use `React.lazy` + `Suspense` for all page components in `App.tsx`
- Currently all 8 page components are statically imported at the top of `App.tsx`
- Show a lightweight loading placeholder during chunk load (use Skeleton from A5)
- Reduces initial bundle size significantly (Cytoscape, CodeMirror are large)

#### D2. Note list virtualization
- InboxPage and ProjectPage: use virtualized list for large note collections
- Use `@tanstack/react-virtual` (headless, pairs well with existing CSS Modules approach; `react-window` is heavier and opinionated about container styling)
- Maintain current card styling and stagger animation on initial load
- Only render ~20 visible cards + buffer

#### D3. Debounce and memoization audit
- Review all components for unnecessary re-renders
- Ensure CodeMirror extensions array is memoized (currently recreated on every render in NoteEditorPage)
- Memoize expensive computations (markdown rendering, search results)
- Use `React.memo` on NoteCard, Sidebar sections

---

## Implementation Order

Recommended sequence, grouping by impact and dependency:

### Sprint 1: Feel (1 week)
- A5 -- Loading skeletons (immediate visual improvement)
- A2 -- Modal/palette exit animations (enter already done, just need exit)
- A4 -- Toast exit animations (enter already done, just need exit + layout)
- B10 -- Improved error feedback (pairs with toast animation)

### Sprint 2: Editor (1 week)
- B2 -- Inline title editing
- B3 -- Enhanced "More" menu + ConfirmModal (replaces `window.confirm` in NoteEditorPage AND CaptureModal)
- B4 -- Word count bar
- B5 -- Tag editing in right panel
- A9 -- Right panel transition smoothness

### Sprint 3: Navigation and Discoverability (1 week)
- B1-BE -- Settings backend (migration, handler, service, store)
- B1-BE-auth -- Account management backend (password change, /me, email endpoints -- no migration needed)
- B1 -- Settings page (frontend)
- B8 -- Sidebar project context menu (no backend changes needed -- existing API supports both cascade modes)
- B9 -- Collapsible section memory (uses settings endpoint)
- A8 -- Sidebar transition smoothness
- C2 -- Keyboard shortcut discoverability

### Sprint 4: Search and AI (1 week)
- B6 -- Ask Seam citation titles (backend + frontend)
- B7-BE -- Chat history backend (migration, handler, service, store)
- B7 -- Ask Seam starter suggestions + conversation persistence
- A6 -- Semantic search relevance bar
- A3 -- Note card stagger animation

### Sprint 5: Responsive and Performance (1 week)
- C1 -- Responsive layout implementation
- D1 -- Route-level code splitting
- D2 -- Note list virtualization
- C3 -- ARIA live regions
- C4 -- Focus management
- D3 -- Debounce and memoization audit

---

## New Dependencies

| Package | Purpose | Track |
|---|---|---|
| `@tanstack/react-virtual` | List virtualization for note lists | D2 |

No other new dependencies. `motion` v12.35.1 is already installed and used in Layout.tsx (page transitions) and NoteEditorPage.tsx (right panel stagger). All new animation work uses it or CSS transitions.

---

## Files Likely Affected

| File | Tasks |
|---|---|
| `web/src/App.tsx` | D1 (lazy routes) |
| `web/src/components/Layout/Layout.tsx` | C1 |
| `web/src/components/Sidebar/Sidebar.tsx` | A8, B8, B9, C1, C2 |
| `web/src/components/Modal/CaptureModal.tsx` | A2 (exit animation), B3 (replace `window.confirm`) |
| `web/src/components/Modal/Modal.module.css` | A2 (replace CSS `@keyframes` with motion) |
| `web/src/components/CommandPalette/CommandPalette.tsx` | A2 (exit animation), C2, C4 |
| `web/src/components/CommandPalette/CommandPalette.module.css` | A2 (replace CSS `@keyframes` with motion) |
| `web/src/components/Toast/ToastContainer.tsx` | A4 (exit animation + layout), B10 |
| `web/src/components/Toast/Toast.module.css` | A4 (replace CSS `@keyframes` with motion) |
| `web/src/components/NoteCard/NoteCard.tsx` | A3 |
| `web/src/pages/NoteEditor/NoteEditorPage.tsx` | B2, B3, B4, B5, A9 |
| `web/src/pages/Search/SearchPage.tsx` | A6 |
| `web/src/pages/Ask/AskPage.tsx` | B6, B7 |
| `web/src/pages/Inbox/InboxPage.tsx` | A3, A5, D2 |
| `web/src/pages/Project/ProjectPage.tsx` | A3, A5, D2 |
| `web/src/pages/Graph/GraphPage.tsx` | A5 |
| `web/src/pages/Timeline/TimelinePage.tsx` | A5 |
| `web/src/pages/Login/LoginPage.tsx` | (minor: loading state) |
| `web/src/stores/uiStore.ts` | B1 (remove `sidebarCollapsed`, `rightPanelOpen`; keep `sidebarOpen`, ephemeral state) |
| `web/src/api/client.ts` | B1 (settings API), B1-BE-auth (auth API), B7-BE (chat API) |
| `web/src/api/types.ts` | B6 (citation type change), B7-BE (Conversation, ChatHistoryMessage types) |
| **New**: `web/src/stores/settingsStore.ts` | B1 |
| **New**: `web/src/pages/Settings/SettingsPage.tsx` | B1 |
| **New**: `web/src/pages/Settings/SettingsPage.module.css` | B1 |
| **New**: `internal/settings/handler.go` | B1-BE |
| **New**: `internal/settings/service.go` | B1-BE |
| **New**: `internal/settings/store.go` | B1-BE |
| **New**: `migrations/user/003_settings.sql` | B1-BE |
| `internal/auth/handler.go` | B1-BE-auth (password change, /me, email endpoints) |
| `internal/auth/service.go` | B1-BE-auth |
| `internal/ai/chat.go` | B6 (change `Citations []string` to struct with ID + Title) |
| `cmd/seamd/main.go` | B6 (update WS `chat.done` payload to include titles) |
| `internal/server/server.go` | B1-BE, B7-BE (mount `/api/settings` and `/api/chat` routes) |
| **New**: `internal/chat/handler.go` | B7-BE |
| **New**: `internal/chat/service.go` | B7-BE |
| **New**: `internal/chat/store.go` | B7-BE |
| **New**: `migrations/user/004_chat_history.sql` | B7-BE |
| **New**: `web/src/components/Skeleton/Skeleton.tsx` | A5 |
| **New**: `web/src/components/Skeleton/Skeleton.module.css` | A5 |
| **New**: `web/src/components/ConfirmModal/ConfirmModal.tsx` | B3 (used by NoteEditorPage, CaptureModal, B8) |
| **New**: `web/src/components/ConfirmModal/ConfirmModal.module.css` | B3 |

### Removed from original plan

| File | Reason |
|---|---|
| ~~`migrations/server/002_add_email.sql`~~ | Email column already exists in `001_users.sql` |
| ~~`internal/auth/store.go`~~ | No schema change needed -- email already in User struct and queries |
| ~~`internal/project/handler.go`~~ | Cascade delete already works with `?cascade=inbox` / `?cascade=delete` |
| ~~`internal/project/store.go`~~ | `DeleteWithNotes` unnecessary -- cascade logic already in service layer |

---

## Open Questions

1. ~~**Persistence scope for settings.**~~ **Resolved.** Server-persisted in per-user SQLite. See B1/B1-BE.

2. ~~**Note list pagination vs infinite scroll vs virtualization.**~~ **Resolved.** Paginate with "load more" for datasets > 100 notes, virtualize the rendered list.

3. ~~**Chat history persistence.**~~ **Resolved.** Server-side chat history endpoint. See B7-BE.

4. ~~**Mobile-first or desktop-first for responsive?**~~ **Resolved.** Desktop-first. Mobile should be usable for reading, capture, and search. Full editing on mobile deferred.

5. ~~**WebSocket conversation_id vs stateless history.**~~ **Resolved.** Keep WebSocket stateless (Option A). Client sends full history as today. Persistence is REST-only. See B7-BE.

6. ~~**Project cascade delete API design.**~~ **Resolved.** Backend already supports `?cascade=inbox` (default) and `?cascade=delete`. No changes needed. Frontend uses existing parameter values.

7. ~~**Email migration needed?**~~ **Resolved.** No. Email column already exists in `001_users.sql`.

---

## Success Criteria

- All animations from FE_DESIGN.md Section 8 are implemented (enter AND exit for modals, toasts; stagger for note lists)
- No "Loading..." plain text anywhere -- all loading states have skeletons
- Settings page with account info, preferences, and keyboard shortcut reference
- Note editor supports inline title editing, tag editing, word count
- Responsive layout works at all four breakpoints (640/1024/1440px)
- All user-facing errors surface as toast notifications
- All `window.confirm()` calls replaced with styled ConfirmModal
- Ask Seam citations render titles from the server (no N+1 client-side resolution)
- Ask Seam conversations persist across page visits
- Lighthouse accessibility score >= 90
- Initial bundle size reduced by >= 30% via code splitting
