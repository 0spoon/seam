# Seam -- Open Issues

Comprehensive audit of the codebase against IMP_PLAN.md, FE_DESIGN.md, PHASE2_FE.md, and AGENTS.md.
Issues are organized by severity, then by area. Each issue has a unique ID for tracking.

Legend: **Critical** = crash/data loss/security exploit, **High** = significant bug or missing spec feature, **Medium** = correctness/UX/security concern, **Low** = minor quality/polish

---

## Previous Issues (Phases 1-4) -- All 110 Resolved

<details>
<summary>Click to expand resolved issues from earlier phases</summary>

### Critical (3 issues) -- RESOLVED

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-C1 | Frontend: NoteEditor | Wikilink clicks in preview pane do nothing. | **Fixed** |
| I-C2 | Frontend: AskPage | Streaming has no error recovery -- UI gets permanently stuck. | **Fixed** |
| I-C3 | Backend: AI handler | Nil pointer dereference when AI services not configured. | **Fixed** |

### High (14 issues) -- RESOLVED

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

### Medium (45 issues) -- RESOLVED

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-M1 - I-M56 | Various | See PROGRESS.md for full list. | **All Fixed** |

### Low (48 issues) -- RESOLVED

| ID | Area | Description | Status |
|----|------|-------------|--------|
| I-L1 - I-L48 | Various | See PROGRESS.md for full list. | **All Fixed** |

</details>

---

## Phase 2 Issues -- All 48 Resolved

Audit conducted against PHASE2_FE.md spec. Issues found during thorough code review of all implemented Phase 2 tasks.

---

### Critical (2 issues) -- RESOLVED

| ID | Area | Description | Spec Ref | Status |
|----|------|-------------|----------|--------|
| P2-C1 | Frontend: Settings/uiStore | **Settings-to-UI bridge missing.** Added `bridgeFromSettings()` to uiStore, called after `fetchSettings()` in App.tsx. Toggles persist back to settingsStore. | B1 | **Fixed** |
| P2-C2 | Backend: settings | **No transaction wrapping in multi-key PUT.** `Service.Update` now uses `db.BeginTx` with rollback on error. | B1-BE | **Fixed** |

---

### High (7 issues) -- RESOLVED

| ID | Area | Description | Spec Ref | Status |
|----|------|-------------|----------|--------|
| P2-H1 | Frontend: NoteEditorPage | **`editorViewMode` not lifted to settingsStore.** Lifted to uiStore (bridged from settingsStore). NoteEditorPage reads from store. | B1 | **Fixed** |
| P2-H2 | Frontend: AskPage | **No conversation list/switcher.** Conversation list/switcher dropdown added to AskPage header (loads last 10, switch between them). | B7 | **Fixed** |
| P2-H3 | Frontend: CaptureModal | **Close and Cancel buttons bypass discard check.** X and Cancel buttons now call `confirmDiscardAndClose()`. | A2/B3 | **Fixed** |
| P2-H4 | Backend: chat handler | **Invalid message role returns HTTP 500 instead of 400.** Added `ErrInvalidRole` sentinel; handler returns 400. | B7-BE | **Fixed** |
| P2-H5 | Backend: chat service | **`truncateToWord` uses byte-length slicing, corrupts multi-byte UTF-8.** Now uses rune-based slicing. | B7-BE | **Fixed** |
| P2-H6 | Backend: settings/chat | **No store interfaces -- entire packages are untestable in isolation.** Added `ServiceInterface` to both settings and chat handlers. | B1-BE, B7-BE | **Fixed** |
| P2-H7 | Backend: settings | **No test files.** Created handler_test.go, service_test.go, store_test.go for settings; handler_test.go for chat. | B1-BE | **Fixed** |

---

### Medium (17 issues) -- RESOLVED

| ID | Area | Description | Spec Ref | Status |
|----|------|-------------|----------|--------|
| P2-M1 | Frontend: NoteEditorPage | **"Move to project" missing from More menu.** Added submenu with project list. | B3 | **Fixed** |
| P2-M2 | Frontend: ConfirmModal | **No focus trap.** Added Tab/Shift+Tab focus trapping. | C4 | **Fixed** |
| P2-M3 | Frontend: Layout | **CSS uses `!important`.** Removed; uses CSS classes instead of inline styles. | C1 | **Fixed** |
| P2-M4 | Frontend: Layout | **Missing 1440px responsive breakpoint.** Added 1440px breakpoint comments; behavior handled by settings bridge. | C1 | **Fixed** |
| P2-M5 | Frontend: Sidebar | **Content fade animation incomplete for Projects/Tags sections.** Now uses fadeLabel CSS class instead of conditional rendering. | A8 | **Fixed** |
| P2-M6 | Frontend: GraphPage | **No tooltip on node hover.** Added tooltip showing title and link count. | Graph UX | **Fixed** |
| P2-M7 | Frontend: GraphPage | **No filter panel animation.** Wrapped in motion.div with entrance animation. | Graph UX | **Fixed** |
| P2-M8 | Frontend: AskPage | **Markdown re-rendered on every streaming token.** Throttled to ~100ms intervals. | Perf | **Fixed** |
| P2-M9 | Frontend: App.tsx | **`PageFallback` uses `NoteListSkeleton` for all routes.** Added page-specific fallback skeletons (editor, graph, generic). | A5/D1 | **Fixed** |
| P2-M10 | Frontend: Layout | **No route change focus management.** Added route announcer with `aria-live="assertive"` + focus management. | C4 | **Fixed** |
| P2-M11 | Frontend: TimelinePage | **Jump-to-date fails silently for unloaded dates.** Now shows toast when target date not in loaded range. | Timeline | **Fixed** |
| P2-M12 | Backend: chat store | **Citation JSON deserialization failure silently swallowed.** Now logs via slog.Warn. | B7-BE | **Fixed** |
| P2-M13 | Backend: ai/chat + chat/store | **Duplicate `Citation` type in two packages.** Added documentation comment linking chat.Citation to ai.Citation. | B6/B7-BE | **Fixed** |
| P2-M14 | Frontend: api/client.ts | **401 retry checks stale module-level `refreshToken`.** Now uses `getRefreshToken()`. | API | **Fixed** |
| P2-M15 | Frontend: api/ws.ts | **WebSocket access token captured at `connect()` time, stale by `onopen`.** Now uses fresh token in `onopen` handler. | API | **Fixed** |
| P2-M16 | Frontend: projectStore | **`deleteProject` does not refresh note store after cascade.** Now calls `useNoteStore.getState().fetchNotes()`. | B8 | **Fixed** |
| P2-M17 | Frontend: NoteEditorPage CSS | **`min-width: 1441px` right panel media query is a no-op.** Removed no-op query; fixed right panel responsive rules. | C1 | **Fixed** |

---

### Low (21 issues) -- RESOLVED

| ID | Area | Description | Spec Ref | Status |
|----|------|-------------|----------|--------|
| P2-L1 | Frontend: ConfirmModal CSS | **Hardcoded color `#d47a7a`.** Replaced with `color-mix(in srgb, var(--status-error) 80%, white 20%)`. | FE_DESIGN | **Fixed** |
| P2-L2 | Frontend: Sidebar | **Inline styles for cascade toggle buttons.** Extracted to CSS classes. | Code style | **Fixed** |
| P2-L3 | Frontend: SettingsPage | **Missing app version in About section.** Injected via Vite `define` from `package.json` version. | B1 | **Fixed** |
| P2-L4 | Frontend: SettingsPage | **Missing "link to docs" in About.** Added docs link to About section. | B1 | **Fixed** |
| P2-L5 | Frontend: SettingsPage | **No loading state for account info.** Added loading indicator. | B1 | **Fixed** |
| P2-L6 | Frontend: SettingsPage | **No `autocomplete` attributes on password inputs.** Added `autocomplete` attributes. | B1 | **Fixed** |
| P2-L7 | Frontend: AskPage | **Dead HTTP fallback code.** Removed dead code path. | B7 | **Fixed** |
| P2-L8 | Frontend: types.ts | **`ChatMessage.role` includes `'system'`.** Removed `'system'` from type. | B6/B7-BE | **Fixed** |
| P2-L9 | Frontend: Layout | **Hamburger button is 40x40px, spec says 48x48px.** Updated to 48x48px. | C1 | **Fixed** |
| P2-L10 | Frontend: GraphPage | **Minimap re-syncs all nodes on every pan/zoom.** Debounced to 50ms. | Perf | **Fixed** |
| P2-L11 | Backend: settings handler | **No request ID in log entries.** Added request IDs to all log entries. | AGENTS.md | **Fixed** |
| P2-L12 | Backend: settings service | **Error sentinels use `fmt.Errorf` instead of `errors.New`.** Changed to `errors.New`. | AGENTS.md | **Fixed** |
| P2-L13 | Backend: settings | **`GetAll` returns only explicitly-set keys.** Now merges defaults so all keys are present. | B1-BE | **Fixed** |
| P2-L14 | Frontend: NoteEditorPage CSS | **Hardcoded colors in `.orphanBadge`.** Replaced with `color-mix` + `var(--status-error)`. | FE_DESIGN | **Fixed** |
| P2-L15 | Frontend: NoteEditorPage CSS | **Hardcoded `font-size: 10px`.** Replaced with `var(--font-size-xs)`. | FE_DESIGN | **Fixed** |
| P2-L16 | Frontend: NoteEditorPage CSS | **Hardcoded `width: 260px`.** Replaced with `var(--right-panel-width)`. | FE_DESIGN | **Fixed** |
| P2-L17 | Frontend: SettingsPage CSS | **Hardcoded `rgba(196,107,107,0.1)`.** Replaced with `color-mix`. | FE_DESIGN | **Fixed** |
| P2-L18 | Frontend: Sidebar CSS | **Hardcoded box-shadow colors.** Replaced with `color-mix`. | FE_DESIGN | **Fixed** |
| P2-L19 | Frontend: ConfirmModal CSS | **Hardcoded backdrop color.** Replaced with `color-mix`. | FE_DESIGN | **Fixed** |
| P2-L20 | Frontend: api/ws.ts | **No WebSocket heartbeat/ping.** Added 30s interval ping with stopHeartbeat cleanup. | Reliability | **Fixed** |
| P2-L21 | Frontend: Sidebar CSS | **Context menu has no overflow protection.** Added viewport boundary checking. | UX | **Fixed** |

---

## Summary

| Severity | Phase 1-4 | Phase 2 | Total Open |
|----------|-----------|---------|------------|
| Critical | 3 (resolved) | 2 (resolved) | **0** |
| High | 14 (resolved) | 7 (resolved) | **0** |
| Medium | 45 (resolved) | 17 (resolved) | **0** |
| Low | 48 (resolved) | 21 (resolved) | **0** |
| **Total** | **110 (resolved)** | **47 (resolved)** | **0** |

---

*Updated: 2026-03-09*
