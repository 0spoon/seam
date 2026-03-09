# Phase 2 -- Frontend Polish and UX

Status: **Draft**
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
- Sidebar navigation with collapsible state, inline search, project/tag sections
- Note editor with CodeMirror, split/preview modes, toolbar, auto-save
- Capture modal with text/URL/voice/template support
- Command palette (Cmd+K) with navigation
- Knowledge graph with Cytoscape, filters, minimap
- Ask Seam chat with WebSocket streaming
- Toast notification system

### What needs work

| Area | Issue |
|---|---|
| **Loading states** | Most pages show plain text "Loading..." with no skeleton or spinner. The initial auth check shows a bare `<div>` with inline styles. |
| **Error handling UX** | Errors are stored in Zustand but rarely surfaced to the user visually. Many catches are silent. No retry affordances. |
| **Animations/transitions** | FE_DESIGN.md Section 8 specifies enter/exit animations for modals, page transitions, note card stagger, toast enter/exit. Currently most of these are missing -- elements just appear/disappear instantly. |
| **Responsive behavior** | FE_DESIGN.md Section 5 specifies three breakpoints (640/1024/1440px) with sidebar collapse, panel hiding, and layout changes. Current responsive CSS is minimal or absent. |
| **No settings page** | Logging out is hidden behind a gear icon. No account settings, no display preferences, no keyboard shortcut reference. |
| **Note editor UX** | No title editing inline. No frontmatter editing. No drag-and-drop for images. No word/character count. `window.confirm()` for delete (not a styled modal). The "More" menu only has "Delete". No "Move to project" option. |
| **Sidebar polish** | Tags section is plain. No project reordering. Project creation inline form works but could be smoother. No project context menu (rename/delete). |
| **Search UX** | No keyboard navigation of search results on the full search page. No recent searches. Semantic search results show a percentage number but not the visual relevance bar from FE_DESIGN.md. |
| **Ask Seam UX** | Citations show truncated IDs instead of note titles. No conversation history persistence. No "clear conversation" button. No suggested starter questions. |
| **Graph UX** | No tooltip on node hover showing note preview. No "center on node" action. Filter panel has no animation. |
| **Timeline** | Basic implementation. Needs sticky date headers, smooth scrolling, and the "today" indicator from FE_DESIGN.md. |
| **Accessibility** | Skip-to-content link exists. ARIA labels are generally present. But: no `aria-live` regions for search results or streaming chat. Focus management in modals could be tighter. No visible keyboard shortcut hints in the UI. |
| **Performance** | Note list renders all notes at once (no virtualization). Large graphs may be slow. No lazy loading of routes. |

---

## Task Breakdown

### Track A: Visual Polish and Motion

These tasks bring the UI in line with FE_DESIGN.md and add the tactile feel that makes Seam distinctive.

#### A1. Page transition animations
- Add fade+slide-up animation on route changes using `motion` (framer-motion successor)
- Wrap `<Outlet>` in Layout with `AnimatePresence` + `motion.div`
- Duration: 250ms, easing: `--ease-out`
- Respect `prefers-reduced-motion`

#### A2. Modal enter/exit animations
- CaptureModal: animate backdrop opacity 0->1, modal translateY 8px->0 + opacity
- CommandPalette: opacity 0->1 + scale 0.98->1
- Use `motion` for orchestrated enter/exit
- Ensure focus management still works with animated mount/unmount

#### A3. Note card stagger animation
- InboxPage, ProjectPage, SearchPage: stagger note card entrance
- Each card fades in + translateY 4px->0, 30ms delay between items
- Use `motion` `staggerChildren`
- Ensure no layout shift during animation

#### A4. Toast enter/exit animations
- Toast enter: opacity 0->1 + translateY 8px->0, 250ms ease-out
- Toast exit: opacity 1->0, 150ms ease-in
- Stack animation: existing toasts slide up smoothly when new one appears
- Currently toasts appear/disappear instantly

#### A5. Loading skeletons
- Replace all "Loading..." text with skeleton placeholders
- Skeleton elements: pulsing rectangles matching the shape of the content they replace
- Pages that need skeletons: InboxPage, ProjectPage, NoteEditorPage, SearchPage, GraphPage, TimelinePage, AskPage (conversation history load from B7)
- Skeleton component: `<Skeleton width height borderRadius />` with CSS pulse animation
- Color: `--bg-elevated` with `--bg-surface` pulse overlay

#### A6. Relevance bar for semantic search
- SearchPage: when in semantic mode, render a thin horizontal bar below each result
- Width proportional to score (0-100%), color `--accent-tertiary`
- Height: 3px, border-radius: `--radius-full`
- Matches FE_DESIGN.md Section 7.4

#### A7. Note card hover refinement
- Ensure the 2px left border "seam line" animates in on hover (not just appears)
- Background transition to `--bg-elevated` should be smooth (150ms)
- Matches FE_DESIGN.md Section 6.2

#### A8. Sidebar transition smoothness
- Sidebar collapse/expand should animate width (250ms, `--ease-out`)
- Content items should fade out before width shrinks, fade in after width expands

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
- `useSettingsStore` is the **source of truth** for persisted UI preferences. On load, it fetches from the server and hydrates. `uiStore` continues to own ephemeral UI state (command palette open, capture modal open, etc.) but must read `sidebarCollapsed`, `rightPanelOpen`, and `editorViewMode` from `useSettingsStore` instead of managing its own copies. Remove those fields from `uiStore` and have components read from `useSettingsStore` directly.
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
Extend `internal/auth` to support password changes and email storage.

**Migration** (`migrations/server/002_add_email.sql`):
```sql
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
```

**New endpoints** (added to existing `/api/auth` routes):
- `PUT /api/auth/password` -- accepts `{ current_password, new_password }`, validates current password, hashes and stores new one. Returns 204 on success, 400 if current password wrong or new password too short (min 8 chars).
- `GET /api/auth/me` -- returns `{ id, username, email }` for the authenticated user (for the Settings account section).
- `PUT /api/auth/email` -- accepts `{ email }`, validates format, updates. Returns 204.

**Registration change**: add optional `email` field to the register endpoint request body. Store it in the `users` table.

**Frontend**: Settings account section calls `getMe()` to populate fields, `changePassword()` and `updateEmail()` for mutations.

**API client** additions:
- `getMe(): Promise<{ id: string, username: string, email: string }>`
- `changePassword(currentPassword: string, newPassword: string): Promise<void>`
- `updateEmail(email: string): Promise<void>`

#### B2. Note editor: inline title editing
- Title should be editable directly at the top of the editor pane (above the CodeMirror instance)
- Large input field, `--font-display` 600, `--font-size-xl`
- Save behavior: **debounced** (same 1s debounce as body changes). Typing updates a local ref; the debounce timer fires `updateNote`. No explicit save on blur or Enter -- the debounce handles it naturally. If the user navigates away before the debounce fires, flush immediately (same pattern as body auto-save).
- On initial create (title is "Untitled"): auto-focus title, select all

#### B3. Note editor: enhanced "More" menu
- Add menu items: "Move to project" (submenu with project list -- include "Inbox" as the first option representing `project_id: null` for removing a note from its current project), "Duplicate note", "Copy link" (copies `/notes/{id}` to clipboard), "Export as Markdown" (downloads `.md` file)
- Replace `window.confirm()` delete dialog with a styled confirmation modal
- Add "Open in new tab" option
- Style the dropdown menu properly with dividers between groups

#### B4. Note editor: word count and metadata bar
- Bottom bar (below editor, above save status): show word count, character count, reading time estimate
- Format: "324 words / 1,842 chars / 2 min read"
- `--font-ui` 300, `--font-size-xs`, `--text-tertiary`
- Only visible in editor or split mode (not preview-only)

#### B5. Note editor: tag editing in right panel
- Current: tags are displayed as pills, no editing
- Add: inline tag input at the bottom of the tags section
- Type a tag name, press Enter or comma to add
- Click the X on a tag pill to remove
- Tags auto-complete from existing tags (use `tags` from uiStore)
- Updates note via `updateNote` with debounce

#### B6. Ask Seam: citation titles
- Currently citations show truncated note IDs (`noteId.slice(0, 8)`)
- **Backend change**: modify the WebSocket `ask` response to include `{ id, title }` objects in the citations array instead of bare note IDs. The AI handler already has access to the note store to resolve titles at response time.
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

**Migration** (`migrations/user/004_chat_history.sql`):
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

**WebSocket + REST integration flow:**
1. User opens AskPage. Client calls `GET /api/chat/conversations?limit=1` to load the most recent conversation. If one exists, calls `GET /api/chat/conversations/:id` to restore messages. If none, UI starts in empty state.
2. User sends a message. If no active conversation, client calls `POST /api/chat/conversations` first to create one and stores the returned `id`.
3. Client calls `POST /api/chat/conversations/:id/messages` with `{ role: "user", content }` to persist the user message.
4. Client sends the question via WebSocket with the existing `ask` message type. The WebSocket payload gains an optional `conversation_id` field so the backend can include conversation context (prior messages) in the AI prompt.
5. WebSocket streams the assistant response as before. Client accumulates the streamed tokens locally.
6. Once the stream completes (final WebSocket message received), client calls `POST /api/chat/conversations/:id/messages` with `{ role: "assistant", content: <full response>, citations: [...] }`.
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

#### B8. Sidebar: project context menu
- Right-click or click a "..." icon on project row to show context menu
- Options: "Rename", "Delete" (with confirmation), "New note in project"
- "Rename" shows an inline edit input (like the create project flow)
- "Delete" shows a styled modal with two options:
  - "Keep notes" (default): moves all notes to Inbox (sets `project_id = NULL`), then deletes the project. Uses the existing `DELETE /api/projects/:id` endpoint.
  - "Delete everything": deletes the project and all its notes in one operation. Uses `DELETE /api/projects/:id?cascade=true` (see B8-BE).

#### B8-BE. Project cascade delete backend
Extend the existing `DELETE /api/projects/:id` endpoint in `internal/project/handler.go`:

- Accept optional query parameter `?cascade=true`
- When `cascade=false` (default, current behavior): set `project_id = NULL` on all notes in the project, then delete the project
- When `cascade=true`: delete all notes belonging to the project (and their tags, links, FTS entries), then delete the project
- Both operations must run in a single transaction
- Add a `DeleteWithNotes(ctx, db, projectID)` method to the project store that handles the cascade

#### B9. Sidebar: collapsible section memory
- Remember which sections (Projects, Tags) are expanded/collapsed across sessions
- Persist via the settings endpoint (B1-BE): keys `sidebar_projects_expanded`, `sidebar_tags_expanded`

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

Add a fixed-position hamburger menu button (top-left, 48x48px tap target) visible only below 640px. Sidebar slides in as an overlay on mobile with a semi-transparent backdrop (`--bg-overlay`). Tapping the backdrop or navigating to a route closes the overlay.

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
- Modal open: focus first interactive element, trap Tab cycling (CaptureModal already does this, verify CommandPalette does too)
- Modal close: return focus to the element that triggered it
- Route change: focus main content area (for screen reader users)
- Dropdown menus: arrow key navigation, Escape to close and restore focus

### Track D: Performance

#### D1. Route-level code splitting
- Use `React.lazy` + `Suspense` for all page components
- Show a lightweight loading placeholder during chunk load
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
- A1 -- Page transition animations
- A2 -- Modal enter/exit animations
- A4 -- Toast enter/exit animations
- B10 -- Improved error feedback (pairs with toast animation)

### Sprint 2: Editor (1 week)
- B2 -- Inline title editing
- B3 -- Enhanced "More" menu
- B4 -- Word count bar
- B5 -- Tag editing in right panel
- A7 -- Note card hover refinement
- A9 -- Right panel transition smoothness

### Sprint 3: Navigation and Discoverability (1 week)
- B1-BE -- Settings backend (migration, handler, service, store)
- B1-BE-auth -- Account management backend (password change, email, /me endpoint)
- B1 -- Settings page (frontend)
- B8-BE -- Project cascade delete backend
- B8 -- Sidebar project context menu
- B9 -- Collapsible section memory (uses settings endpoint)
- A8 -- Sidebar transition smoothness
- C2 -- Keyboard shortcut discoverability

### Sprint 4: Search and AI (1 week)
- B7-BE -- Chat history backend (migration, handler, service, store)
- B6 -- Ask Seam citation titles
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

No other new dependencies. `motion` (framer-motion) is already installed. All animation work uses it or CSS transitions.

---

## Files Likely Affected

| File | Tasks |
|---|---|
| `web/src/App.tsx` | D1 (lazy routes) |
| `web/src/components/Layout/Layout.tsx` | A1, C1 |
| `web/src/components/Sidebar/Sidebar.tsx` | A8, B8, B9, C1, C2 |
| `web/src/components/Modal/CaptureModal.tsx` | A2 |
| `web/src/components/CommandPalette/CommandPalette.tsx` | A2, C2, C4 |
| `web/src/components/Toast/ToastContainer.tsx` | A4, B10 |
| `web/src/components/NoteCard/NoteCard.tsx` | A3, A7 |
| `web/src/components/EmptyState/EmptyState.tsx` | (minor adjustments) |
| `web/src/pages/NoteEditor/NoteEditorPage.tsx` | B2, B3, B4, B5, A9 |
| `web/src/pages/Search/SearchPage.tsx` | A6 |
| `web/src/pages/Ask/AskPage.tsx` | B6, B7, B7-BE |
| `web/src/pages/Inbox/InboxPage.tsx` | A3, A5, D2 |
| `web/src/pages/Project/ProjectPage.tsx` | A3, A5, D2 |
| `web/src/pages/Graph/GraphPage.tsx` | A5 |
| `web/src/pages/Timeline/TimelinePage.tsx` | A5 |
| `web/src/pages/Login/LoginPage.tsx` | (minor: loading state) |
| `web/src/stores/uiStore.ts` | B1 (remove persisted fields), B9 |
| `web/src/api/client.ts` | B1 (settings API), B1-BE-auth (auth API), B7-BE (chat API), B8-BE (cascade param) |
| **New**: `web/src/stores/settingsStore.ts` | B1 |
| **New**: `web/src/pages/Settings/SettingsPage.tsx` | B1 |
| **New**: `web/src/pages/Settings/SettingsPage.module.css` | B1 |
| **New**: `internal/settings/handler.go` | B1-BE |
| **New**: `internal/settings/service.go` | B1-BE |
| **New**: `internal/settings/store.go` | B1-BE |
| **New**: `migrations/user/003_settings.sql` | B1-BE |
| **New**: `migrations/server/002_add_email.sql` | B1-BE-auth |
| `internal/auth/handler.go` | B1-BE-auth (password change, /me, email endpoints) |
| `internal/auth/service.go` | B1-BE-auth |
| `internal/auth/store.go` | B1-BE-auth (email column) |
| `internal/project/handler.go` | B8-BE (cascade query param) |
| `internal/project/store.go` | B8-BE (DeleteWithNotes method) |
| `internal/ai/handler.go` | B6 (include note titles in citations), B7-BE (conversation_id in ask) |
| `internal/server/server.go` | B1-BE, B7-BE (mount `/api/settings` and `/api/chat` routes) |
| **New**: `internal/chat/handler.go` | B7-BE |
| **New**: `internal/chat/service.go` | B7-BE |
| **New**: `internal/chat/store.go` | B7-BE |
| **New**: `migrations/user/004_chat_history.sql` | B7-BE |
| **New**: `web/src/components/Skeleton/Skeleton.tsx` | A5 |
| **New**: `web/src/components/Skeleton/Skeleton.module.css` | A5 |
| **New**: `web/src/components/ConfirmModal/ConfirmModal.tsx` | B3, B8 |
| **New**: `web/src/components/ConfirmModal/ConfirmModal.module.css` | B3, B8 |

---

## Open Questions

1. ~~**Persistence scope for settings.**~~ **Resolved.** Server-persisted in per-user SQLite. See B1/B1-BE.

2. ~~**Note list pagination vs infinite scroll vs virtualization.**~~ **Resolved.** Paginate with "load more" for datasets > 100 notes, virtualize the rendered list.

3. ~~**Chat history persistence.**~~ **Resolved.** Server-side chat history endpoint. See B7-BE below.

4. ~~**Mobile-first or desktop-first for responsive?**~~ **Resolved.** Desktop-first. Mobile should be usable for reading, capture, and search. Full editing on mobile deferred.

---

## Success Criteria

- All animations from FE_DESIGN.md Section 8 are implemented
- No "Loading..." plain text anywhere -- all loading states have skeletons
- Settings page with account info, preferences, and keyboard shortcut reference
- Note editor supports inline title editing, tag editing, word count
- Responsive layout works at all four breakpoints
- All user-facing errors surface as toast notifications
- Lighthouse accessibility score >= 90
- Initial bundle size reduced by >= 30% via code splitting
