# Seam — Frontend Design Specification

Reference: [PLAN.md](./PLAN.md) for features, [IMP_PLAN.md](./IMP_PLAN.md) for implementation.

---

## 1. Design Direction

**Concept: Dark Cartography**

Seam maps your knowledge. The aesthetic draws from vintage cartography and technical draftsmanship — warm, precise, and layered — rendered in a modern dark interface. Where most knowledge tools feel cold and clinical, Seam feels like a well-worn workshop: warm amber light, aged-paper tones, and the quiet confidence of a tool built to last.

**Core principles:**
- **Warmth over sterility.** Warm off-white text on deep blue-black. Amber accents, not neon.
- **Density with clarity.** Show information without clutter. Generous line-height, tight gutters.
- **Connection as motif.** The amber accent is the "seam" — golden thread linking ideas. It appears on wikilinks, graph edges, active navigation, and hover states.
- **Restraint.** Dark theme, no gradients on surfaces, no glow effects on containers. Depth through layered background values, not shadows.

---

## 2. Typography

Four fonts, each with a distinct role. Load via Google Fonts.

```
https://fonts.googleapis.com/css2?family=Fraunces:ital,opsz,wght@0,9..144,300..900;1,9..144,300..900&family=Lora:ital,wght@0,400..700;1,400..700&family=Outfit:wght@300..800&family=IBM+Plex+Mono:ital,wght@0,400;0,500;0,600;1,400&display=swap
```

| Role | Font | Weight | Usage |
|---|---|---|---|
| **Display** | Fraunces | 500-700 | Page titles, empty-state headings, the Seam wordmark. Variable optical sizing creates warmth at large sizes. |
| **UI** | Outfit | 300-700 | Sidebar labels, buttons, form inputs, metadata, timestamps, tags. Clean geometric sans, slightly futuristic. |
| **Content** | Lora | 400-700 | Note preview (rendered markdown). Designed for screen reading. Beautiful italics. |
| **Mono** | IBM Plex Mono | 400-600 | CodeMirror editor, code blocks, frontmatter display, keyboard shortcuts. |

### Scale

Based on a 1.25 ratio. All sizes in `rem` (base 16px).

```css
--font-size-xs:    0.75rem;   /* 12px - timestamps, badges */
--font-size-sm:    0.8125rem; /* 13px - secondary text, metadata */
--font-size-base:  0.875rem;  /* 14px - body, UI text */
--font-size-md:    1rem;      /* 16px - note body content */
--font-size-lg:    1.25rem;   /* 20px - section headings */
--font-size-xl:    1.5rem;    /* 24px - page titles */
--font-size-2xl:   2rem;      /* 32px - display headings */
--font-size-3xl:   2.5rem;    /* 40px - hero/empty-state headings */

--line-height-tight:   1.3;
--line-height-normal:  1.5;
--line-height-relaxed: 1.7;   /* note content preview */

--font-display:  'Fraunces', Georgia, serif;
--font-ui:       'Outfit', system-ui, sans-serif;
--font-content:  'Lora', Georgia, serif;
--font-mono:     'IBM Plex Mono', 'Menlo', monospace;
```

### Rules

- Sidebar, buttons, inputs, tags, metadata: `--font-ui`
- Note preview (rendered markdown), chat messages: `--font-content`
- CodeMirror editor, code blocks, YAML frontmatter: `--font-mono`
- Page titles (e.g., project name, "Ask Seam" header): `--font-display`
- Headings within note previews: `--font-display` (h1-h3), `--font-content` bold (h4-h6)
- Never mix UI font into note content or content font into UI chrome

---

## 3. Color System

All colors as CSS custom properties on `:root`. No hardcoded values anywhere.

### Backgrounds

Layered from deepest (page) to highest (popovers). Each step is subtle — enough to create depth without harsh contrast.

```css
--bg-deep:       #08090d;    /* page background, deepest layer */
--bg-base:       #0f1117;    /* main content background */
--bg-surface:    #161922;    /* cards, sidebar, panels */
--bg-elevated:   #1d2130;    /* hover states, active items, dropdowns */
--bg-overlay:    #252a3a;    /* modal overlays, tooltips */
```

### Borders

```css
--border-subtle:  #1e2233;   /* dividers between sections */
--border-default: #2a3045;   /* card borders, input borders */
--border-strong:  #3a4260;   /* focus rings, emphasis borders */
```

### Text

Warm off-white hierarchy. The primary text has a parchment undertone — not pure white, not blue-white.

```css
--text-primary:   #e8e2d9;   /* primary text, headings */
--text-secondary: #9992a6;   /* secondary labels, metadata */
--text-tertiary:  #5e5a6e;   /* placeholders, disabled text */
--text-inverse:   #0f1117;   /* text on accent-colored backgrounds */
```

### Accents

The amber primary accent is the "seam" — the golden thread connecting everything. It appears on links, active states, the capture button, wikilinks, and graph edges.

```css
--accent-primary:   #c4915c; /* amber/copper - links, active states, wikilinks */
--accent-hover:     #d4a06c; /* lighter amber on hover */
--accent-muted:     rgba(196, 145, 92, 0.10); /* amber tint for backgrounds */
--accent-secondary: #6b9b7a; /* sage green - success, tags, secondary actions */
--accent-tertiary:  #7b8ec4; /* steel blue - info, semantic search */
```

### Status

```css
--status-error:   #c46b6b;   /* destructive actions, errors */
--status-warning: #c4a95c;   /* warnings, pending states */
--status-success: #6b9b7a;   /* confirmations (same as accent-secondary) */
--status-info:    #7b8ec4;   /* informational (same as accent-tertiary) */
```

### Project colors

Each project gets a color from this palette, assigned in order of creation. Used for graph nodes, project badges, and sidebar indicators.

```css
--project-1: #c4915c;  /* amber */
--project-2: #6b9b7a;  /* sage */
--project-3: #7b8ec4;  /* steel */
--project-4: #b87ba4;  /* mauve */
--project-5: #c49b6b;  /* peach */
--project-6: #6bc4b8;  /* teal */
--project-7: #9b7bc4;  /* lavender */
--project-8: #c46b6b;  /* rose */
```

### Tag colors

Tags use a hashed color from the project palette. Hash the tag name to an index (0-7) into the project color array. This gives deterministic, consistent tag coloring.

---

## 4. Spacing and Dimensions

### Spacing scale

4px base, powers of 2 for larger values.

```css
--space-1:   4px;
--space-2:   8px;
--space-3:   12px;
--space-4:   16px;
--space-5:   20px;
--space-6:   24px;
--space-8:   32px;
--space-10:  40px;
--space-12:  48px;
--space-16:  64px;
```

### Border radius

```css
--radius-sm:   4px;   /* tags, small badges */
--radius-md:   6px;   /* buttons, inputs, cards */
--radius-lg:   10px;  /* modals, panels */
--radius-xl:   14px;  /* large cards, containers */
--radius-full: 9999px; /* pills, avatars */
```

### Layout dimensions

```css
--sidebar-width:          260px;
--sidebar-collapsed:      52px;
--right-panel-width:      300px;
--topbar-height:          0px;      /* no top bar; sidebar handles navigation */
--editor-max-width:       none;     /* fills available space */
--preview-max-width:      720px;    /* readable line length in preview */
--chat-max-width:         680px;    /* centered chat column */
--note-card-min-height:   80px;
--modal-width-sm:         420px;    /* quick capture */
--modal-width-md:         560px;    /* confirmations */
--modal-width-lg:         720px;    /* templates, settings */
```

### Z-index scale

```css
--z-sidebar:   100;
--z-panel:     200;
--z-dropdown:  300;
--z-modal:     400;
--z-toast:     500;
--z-command:   600;   /* command palette above everything */
```

---

## 5. Layout Architecture

### Primary layout

No top bar. The sidebar handles all navigation. Content gets maximum vertical space.

```
+---------------+------------------------------------+-------------+
|               |                                    |             |
|   Sidebar     |         Main Content               |   Right     |
|   260px       |         (flexible)                 |   Panel     |
|   fixed       |                                    |   300px     |
|               |                                    |   optional  |
|   - Wordmark  |   [varies by route]                |             |
|   - Search    |                                    |   Backlinks |
|   - Inbox     |                                    |   Related   |
|   - Projects  |                                    |   Tags      |
|   - Tags      |                                    |             |
|               |                                    |             |
|   - User      |                                    |             |
|   - Capture   |                                    |             |
+---------------+------------------------------------+-------------+
```

### Note editor layout

Split view with optional right panel:

```
+----------+-------------------+-------------------+----------+
| Sidebar  |  Toolbar (spans editor + preview)     |  Right   |
|          +-------------------+-------------------+  Panel   |
|          |                   |                   |          |
|          |  Editor           |  Preview          | Backlinks|
|          |  (CodeMirror)     |  (rendered md)    | Related  |
|          |  font-mono        |  font-content     | Tags     |
|          |                   |                   |          |
|          |                   |                   |          |
+----------+-------------------+-------------------+----------+
```

- Editor and preview each take 50% of available width when both visible
- Toggle to editor-only or preview-only via toolbar buttons
- Right panel slides in/out independently
- Toolbar height: 40px, pinned to top of content area

### Responsive breakpoints

```css
--bp-sm:   640px;    /* stack everything vertically */
--bp-md:   1024px;   /* collapse sidebar to icons, hide right panel */
--bp-lg:   1440px;   /* full layout with all panels */
```

| Breakpoint | Sidebar | Right panel | Editor |
|---|---|---|---|
| < 640px | Hidden (hamburger toggle) | Hidden | Single view (editor or preview, not split) |
| 640-1024px | Collapsed (52px icons) | Hidden (toggle button) | Split or single |
| 1024-1440px | Full (260px) | Hidden by default (toggle) | Split |
| > 1440px | Full (260px) | Visible by default | Split |

---

## 6. Component Catalog

### 6.1 Sidebar

The left navigation panel. Always visible at `>=1024px`.

**Structure (top to bottom):**

1. **Wordmark** — "Seam" in Fraunces 600, `--font-size-lg`, `--text-primary`. No logo icon. Click returns to Inbox.
2. **Search input** — Full-width text input with a subtle border. Placeholder: "Search notes..." in `--text-tertiary`. On focus, border becomes `--accent-primary`. Press `/` anywhere to focus.
3. **Section: Inbox** — Single item with unread/unsorted count badge (amber pill). Click navigates to inbox view.
4. **Section: Projects** — Collapsible list. Each project row: a 6px colored dot (project color), project name in `--font-ui` 400, note count in `--text-tertiary`. Active project row has `--bg-elevated` background and a 2px left border in its project color.
5. **Section: Tags** — Collapsible. Top 10 tags shown as text rows with count. Click filters by tag.
6. **Divider** — `--border-subtle`, 1px, with `--space-4` vertical margin.
7. **User row** — Bottom of sidebar, pinned. Shows username in `--font-ui` 400, a small circle avatar (first letter, colored), and a settings gear icon.
8. **Capture button** — Below user row. Full-width button, `--accent-primary` background, `--text-inverse` text, `--font-ui` 600. Label: "Capture". Subtle pulse animation (see Section 8).

**Collapsed state (52px):**
- Wordmark hidden, replaced by "S" in Fraunces 600
- Search becomes a magnifying glass icon button
- Project dots visible, names hidden (tooltip on hover)
- Tags hidden
- Capture button becomes icon-only (plus sign)
- Toggle button at bottom of sidebar to expand

### 6.2 Note card

Used in project view and search results. A horizontal card, not a square tile.

```
+------------------------------------------------------------------------+
| Title of the Note                                    3 hours ago       |
| First line of the note content, truncated to two lines at most...     |
| [#tag1]  [#tag2]  [#architecture]              project-name           |
+------------------------------------------------------------------------+
```

**Specs:**
- Background: `--bg-surface`
- Border: 1px `--border-subtle`, radius `--radius-md`
- Padding: `--space-4` all sides
- Title: `--font-ui` 500, `--font-size-base`, `--text-primary`
- Preview: `--font-ui` 300, `--font-size-sm`, `--text-secondary`, max 2 lines, ellipsis overflow
- Tags: pills with `--radius-sm`, `--font-size-xs`, `--font-ui` 400, background from tag hash color at 12% opacity, text in the tag hash color
- Timestamp: `--font-ui` 300, `--font-size-xs`, `--text-tertiary`, top-right
- Project: `--font-ui` 300, `--font-size-xs`, `--text-tertiary`, bottom-right, with project color dot
- Hover: background shifts to `--bg-elevated`, 2px left border in `--accent-primary` (the "seam line")
- Click: navigates to `/notes/{id}`
- Gap between cards: `--space-2`

### 6.3 Buttons

Three variants: **primary**, **secondary**, **ghost**.

| Variant | Background | Text | Border | Hover |
|---|---|---|---|---|
| Primary | `--accent-primary` | `--text-inverse` | none | `--accent-hover` bg |
| Secondary | transparent | `--text-primary` | 1px `--border-default` | `--bg-elevated` bg |
| Ghost | transparent | `--text-secondary` | none | `--text-primary` text, `--bg-elevated` bg |

**Shared specs:**
- Font: `--font-ui` 500, `--font-size-sm`
- Padding: `--space-2` vertical, `--space-4` horizontal
- Radius: `--radius-md`
- Height: 32px (small), 36px (default), 40px (large)
- Transition: `background 150ms ease, color 150ms ease, border-color 150ms ease`
- Disabled: 40% opacity, no pointer events
- Focus: 2px outline in `--accent-primary` with 2px offset

### 6.4 Input fields

**Text input:**
- Background: `--bg-base`
- Border: 1px `--border-default`, radius `--radius-md`
- Text: `--font-ui` 400, `--font-size-base`, `--text-primary`
- Placeholder: `--text-tertiary`
- Padding: `--space-2` vertical, `--space-3` horizontal
- Height: 36px
- Focus: border `--accent-primary`, subtle box-shadow `0 0 0 2px var(--accent-muted)`
- Error: border `--status-error`

**Textarea:**
- Same as text input, but min-height 120px, resize vertical
- Used in quick capture and chat input

### 6.5 Tags

Inline pill elements.

- Background: tag color at 10% opacity
- Text: tag color at full saturation, `--font-ui` 400, `--font-size-xs`
- Padding: 2px 8px
- Radius: `--radius-sm`
- Hover: background at 18% opacity
- Prefix: `#` character included in the text

### 6.6 Modal

Used for quick capture, confirmations, template selection.

- Backdrop: `rgba(8, 9, 13, 0.75)` with `backdrop-filter: blur(4px)`
- Container: `--bg-surface`, border `--border-default`, radius `--radius-lg`
- Shadow: `0 16px 48px rgba(0, 0, 0, 0.5)`
- Max width: varies by modal type (see `--modal-width-*`)
- Padding: `--space-6`
- Title: `--font-display` 500, `--font-size-lg`, `--text-primary`
- Close: ghost icon button, top-right corner
- Enter: opens (if triggered by button); Escape: closes
- Animations: see Section 8

### 6.7 Command palette

Global shortcut: `Cmd+K` (Mac) / `Ctrl+K`.

- Positioned: centered, top 20% of viewport
- Width: `--modal-width-md` (560px)
- Appearance: same as modal but no title bar
- Search input at top, full width, auto-focused
- Results list below: icon + label + secondary text + keyboard shortcut hint
- Navigate: arrow keys, Enter to select, Escape to close
- Items: "New note", "Search notes", "Open project: {name}", "Ask Seam", "Quick capture", "Toggle sidebar", "Graph view"

### 6.8 Toast notifications

Bottom-right stack. For confirmations ("Note saved"), errors ("Failed to save"), and background task updates.

- Background: `--bg-overlay`
- Border: 1px `--border-default`, radius `--radius-md`
- Text: `--font-ui` 400, `--font-size-sm`
- Padding: `--space-3` `--space-4`
- Max width: 360px
- Duration: 4 seconds, then fade out
- Stack: newest at bottom, max 3 visible

### 6.9 Wikilink display

In the **editor** (CodeMirror): wikilinks are rendered inline with `--accent-primary` color and a dotted underline. The `[[` and `]]` delimiters are shown in `--text-tertiary` at 80% size. CodeMirror decoration plugin.

In the **preview** (rendered markdown): wikilinks become clickable links in `--accent-primary` with a solid underline. Hover: underline color transitions to `--accent-hover`. Click navigates to the linked note.

Dangling links (target does not exist): same styling but with a dashed underline and `--text-secondary` color instead of amber. Tooltip on hover: "Note does not exist yet."

### 6.10 Empty states

When a view has no content (empty inbox, new project with no notes, no search results):

- Centered vertically and horizontally in the content area
- Heading: `--font-display` 400, `--font-size-2xl`, `--text-tertiary`
- Subtext: `--font-ui` 300, `--font-size-base`, `--text-tertiary`
- Optional CTA button (primary variant)
- Background: a subtle SVG topographic contour line pattern in `--border-subtle` at 30% opacity, covering the full content area. This reinforces the cartography motif.

**Empty state text by view:**
- Inbox empty: "Nothing in the inbox" / "Capture a thought to get started"
- Project empty: "No notes yet" / "Create the first note in this project"
- Search no results: "No matches" / "Try different keywords or use semantic search"
- Tags empty: "No tags" / "Add #tags to your notes and they will appear here"

---

## 7. Screen Designs

### 7.1 Login / Register

Full-screen, centered card. No sidebar.

- Background: `--bg-deep` with the topographic contour pattern at 15% opacity
- Card: `--bg-surface`, border `--border-default`, radius `--radius-lg`, width 400px, centered
- "Seam" wordmark: `--font-display` 600, `--font-size-3xl`, `--text-primary`, centered above card
- Tagline below wordmark: "Where ideas connect" in `--font-ui` 300, `--font-size-base`, `--text-secondary`
- Form: username, email (register only), password fields stacked
- Submit button: primary variant, full width
- Toggle link: "Already have an account? Log in" / "Need an account? Register" as ghost text link below the card

### 7.2 Inbox / Project view

The default view after login. Shows a list of notes filtered by inbox or project.

**Header area** (top of main content, below sidebar):
- Title: project name or "Inbox" in `--font-display` 600, `--font-size-xl`
- Project description (if project view): `--font-ui` 300, `--font-size-sm`, `--text-secondary`, max 2 lines
- Controls row: sort dropdown (Modified / Created / Title), view toggle (list / compact), "New note" button (secondary)
- Divider below controls: `--border-subtle`

**Note list:**
- Vertical stack of note cards (Section 6.2)
- Gap: `--space-2`
- Padding: `--space-6` horizontal, `--space-4` top
- Infinite scroll or "Load more" button at bottom
- If filtered by tag, show active filter as a dismissible pill above the list

### 7.3 Note editor

The core editing experience. Route: `/notes/{id}`.

**Toolbar (40px, pinned):**
- Background: `--bg-surface`, bottom border `--border-subtle`
- Left group: bold (B), italic (I), heading (#), link, wikilink ([[), code, list, checklist. Icon buttons, ghost variant, 28px square.
- Center: nothing (spacious)
- Right group: view mode toggle (editor only | split | preview only), right panel toggle, "..." menu (delete, move to project, export)
- Active view mode button: `--accent-primary` text

**Editor pane (left):**
- CodeMirror 6 with custom dark theme (see Section 7.3a)
- Font: `--font-mono`, `--font-size-base`
- Background: `--bg-base`
- Line numbers: `--text-tertiary`, right-aligned, 48px gutter
- Active line: `--bg-surface` background highlight (very subtle)
- Selection: `--accent-muted` background
- Wikilinks: decorated per Section 6.9
- Auto-save: debounced 1s after last keystroke, subtle "Saving..." / "Saved" indicator in bottom-right of editor pane, `--font-ui` 300, `--font-size-xs`, `--text-tertiary`

**Preview pane (right):**
- Background: `--bg-base`
- Content max-width: `--preview-max-width` (720px), centered within pane
- Padding: `--space-8` horizontal, `--space-6` top
- Font: `--font-content`, `--font-size-md`, `--line-height-relaxed`
- Headings: `--font-display`
  - h1: 600 weight, `--font-size-xl`, `--text-primary`, margin-top `--space-10`
  - h2: 500 weight, `--font-size-lg`, `--text-primary`, margin-top `--space-8`
  - h3: 500 weight, `--font-size-md`, `--text-primary`, margin-top `--space-6`
- Links: `--accent-primary`, underline on hover
- Code blocks: `--font-mono`, `--bg-surface`, `--border-subtle` border, radius `--radius-md`, padding `--space-4`
- Inline code: `--font-mono`, `--bg-surface`, padding 2px 6px, radius `--radius-sm`
- Blockquotes: left border 3px `--accent-primary`, padding-left `--space-4`, `--text-secondary` text
- Horizontal rules: 1px `--border-subtle`, margin `--space-8` vertical
- Images: max-width 100%, radius `--radius-md`

**Right panel (300px, collapsible):**
- Background: `--bg-surface`, left border `--border-subtle`
- Sections separated by `--border-subtle` dividers
- Section heading: `--font-ui` 600, `--font-size-xs`, `--text-tertiary`, uppercase, letter-spacing 0.05em, padding `--space-4`

Sections:
1. **Backlinks** — list of notes linking to this note. Each row: note title as amber link, project name and date below in `--text-tertiary`. Click opens that note.
2. **Related notes** (Phase 2) — semantically similar notes. Same layout as backlinks, with similarity score as a subtle bar indicator.
3. **Tags** — tag pills for the current note. "Add tag" input at bottom.
4. **Metadata** — created/modified dates, project, file path. `--font-mono` `--font-size-xs`, `--text-tertiary`.

#### 7.3a CodeMirror theme

Custom theme matching the color system. Define as a CodeMirror `Extension`.

```
Background:           --bg-base
Foreground:           --text-primary
Cursor:               --accent-primary
Selection:            --accent-muted
Line numbers:         --text-tertiary
Active line number:   --text-secondary
Active line bg:       --bg-surface (very subtle)
Gutter bg:            --bg-base
Matching bracket:     --accent-primary underline

Syntax highlighting:
  Heading:            --accent-primary, font-weight 600
  Bold:               --text-primary, font-weight 600
  Italic:             --text-primary, italic
  Link URL:           --text-tertiary
  Link text:          --accent-primary
  Code/code block:    --accent-secondary
  List marker:        --text-tertiary
  Blockquote:         --text-secondary, italic
  Frontmatter (---):  --text-tertiary
  YAML keys:          --accent-tertiary
  YAML values:        --text-secondary
```

### 7.4 Search

Route: `/search`. Also accessible via sidebar search input (inline results) and command palette.

**Sidebar search (inline):**
- Typing in the sidebar search input shows a dropdown of up to 5 results below it
- Each result: note title + snippet with highlighted match
- Enter or click opens the note
- "View all results" link at bottom navigates to `/search?q=...`

**Full search page:**
- Search input at top, large (48px height), `--font-ui` 400, `--font-size-lg`, auto-focused
- Below input: toggle tabs "Full-text" / "Semantic" — text buttons, active has underline in `--accent-primary`
- Results: list of note cards with highlighted match snippets
- Snippet highlights: matching terms wrapped in `<mark>` with `--accent-muted` background and `--accent-primary` text
- Semantic results: show a relevance score bar (thin horizontal bar, width proportional to score, colored `--accent-tertiary`)
- No results: empty state (Section 6.10)

### 7.5 Ask Seam

Route: `/ask`. Chat interface for conversational AI over your notes.

**Layout:**
- Centered column, max-width `--chat-max-width` (680px)
- Header: "Ask Seam" in `--font-display` 600, `--font-size-xl`, with a subtitle "Chat with your notes" in `--text-secondary`
- Messages fill the middle area, scrollable
- Input area pinned to bottom

**Messages:**
- User messages: right-aligned, `--accent-muted` background, `--text-primary` text, radius `--radius-lg` (with bottom-right radius `--radius-sm` for chat-bubble shape)
- AI messages: left-aligned, `--bg-surface` background, `--text-primary` text, radius `--radius-lg` (with bottom-left radius `--radius-sm`)
- Font: `--font-content`, `--font-size-base`, `--line-height-normal`
- Timestamps: `--font-ui` 300, `--font-size-xs`, `--text-tertiary`, below each message
- Streaming: AI tokens appear with no per-token animation (just append). Cursor blinks at end of in-progress message.

**Citations:**
- After AI response completes, citations appear as a row of small linked badges below the message
- Each badge: note title truncated, `--font-ui` 400, `--font-size-xs`, `--bg-elevated` background, `--accent-primary` text, radius `--radius-full`
- Click navigates to the cited note

**Input area:**
- Textarea: 48px min-height, grows to max 160px, `--font-ui` 400, `--font-size-base`
- Send button: primary variant, right of textarea, icon-only (arrow up)
- Placeholder: "Ask about your notes..."
- `Enter` sends (no shift). `Shift+Enter` for newline.

### 7.6 Graph view

Route: `/graph`. Full-screen Cytoscape.js canvas.

**Layout:**
- Graph canvas fills entire content area (no right panel)
- Sidebar remains visible
- Filter panel: floating card, top-left of canvas, `--bg-surface`, `--border-default`, radius `--radius-lg`, shadow `--shadow-md`

**Canvas:**
- Background: `--bg-deep`
- Subtle dot grid pattern: dots at 24px intervals, `--border-subtle` color, 1px radius. This gives the graph a "blueprint paper" feel.

**Nodes:**
- Shape: rounded rectangle (pill shape, `--radius-full`)
- Background: project color at 15% opacity
- Border: 1.5px project color at 60% opacity
- Label: note title, `--font-ui` 400, `--font-size-xs`, `--text-primary`, centered below node
- Size: base 24px height, scaled by link count (more links = larger, max 2x)
- Hover: border becomes 100% opacity, background becomes 25% opacity, label becomes `--font-ui` 500
- Selected/clicked: border becomes `--accent-primary`, background `--accent-muted`

**Edges:**
- Color: `--border-default` (relaxed), `--accent-primary` (when source or target is hovered/selected)
- Width: 1px (relaxed), 2px (highlighted)
- Style: slightly curved (haystack or bezier)
- Opacity: 0.4 (relaxed), 1.0 (highlighted)

**Filter panel:**
- Project filter: checkboxes with project color dots
- Tag filter: tag pills, toggle on/off
- Date range: two date inputs (since/until)
- "Reset" ghost button
- Filters update the graph in real-time (fade out non-matching nodes)

**Controls:**
- Zoom: scroll or pinch
- Pan: click-drag on background
- Select: click node to select, click background to deselect
- Open: double-click node navigates to `/notes/{id}`
- Minimap: bottom-right, 120x80px, `--bg-surface` background with `--border-subtle` border

**Force-directed layout:** Use `fcose` (fast compound spring embedder) layout from Cytoscape. Cluster by project. Gravity toward center.

### 7.7 Timeline view

Route: `/timeline`. Notes organized by date.

**Layout:**
- Vertical scrolling timeline
- Date markers on the left: `--font-display` 500, `--font-size-lg`, `--text-secondary`. Format: "Mar 8, 2026". Sticky while scrolling through that date's notes.
- Notes branch to the right of the date marker
- A thin vertical line (1px `--border-subtle`) connects date markers

**Navigation:**
- Date picker at top to jump to a specific date
- Toggle: "Created" / "Modified" (which date to group by)
- Today indicator: `--accent-primary` dot on the timeline line

### 7.8 Quick capture modal

Triggered by: sidebar capture button, `Cmd+N`/`Ctrl+N`.

- Modal variant: `--modal-width-sm` (420px)
- Title input: large (36px height), `--font-ui` 500, placeholder "Title (optional)"
- Body textarea: min-height 160px, `--font-mono` 400, placeholder "Write your thought..."
- Below textarea: optional project selector (dropdown, default "Inbox") and tag input
- Footer: "Save to Inbox" primary button, "Cancel" ghost button
- `Cmd+Enter` / `Ctrl+Enter` saves and dismisses
- `Escape` dismisses (with confirmation if content has been entered)
- Auto-focus on body textarea (title is optional for quick thoughts)

---

## 8. Motion and Animation

### Principles

- Motion is functional, not decorative. It communicates state transitions.
- Prefer CSS transitions over JS animations. Use Motion (framer-motion) only for orchestrated sequences (staggered lists, layout animations).
- Duration: 150ms for micro-interactions (hover, focus), 250ms for panel slides, 350ms for modals and page transitions.
- Easing: `cubic-bezier(0.16, 1, 0.3, 1)` (ease-out-expo) for enters, `cubic-bezier(0.4, 0, 0.2, 1)` (ease-out) for exits.

### Catalog

```css
--ease-out:      cubic-bezier(0.16, 1, 0.3, 1);
--ease-in:       cubic-bezier(0.4, 0, 1, 1);
--ease-standard:  cubic-bezier(0.4, 0, 0.2, 1);
--duration-fast:  150ms;
--duration-normal: 250ms;
--duration-slow:  350ms;
```

| Element | Animation | Duration | Easing |
|---|---|---|---|
| Button hover | Background color transition | fast | standard |
| Input focus | Border color + box-shadow | fast | standard |
| Sidebar collapse/expand | Width transition | normal | out |
| Right panel slide in/out | Transform translateX | normal | out |
| Modal appear | Opacity 0->1 + translateY 8px->0 | slow | out |
| Modal dismiss | Opacity 1->0 + translateY 0->4px | normal | in |
| Note card hover | Background + left border | fast | standard |
| Page transition (route change) | Content opacity 0->1 + translateY 4px->0 | normal | out |
| Toast enter | Opacity 0->1 + translateY 8px->0 | normal | out |
| Toast exit | Opacity 1->0 | fast | in |
| Command palette appear | Opacity 0->1 + scale 0.98->1 | normal | out |
| Note list load | Stagger children, each fades in + translateY 4px->0, 30ms delay between items | normal | out |
| Graph node hover | Border opacity transition | fast | standard |
| Capture button pulse | Box-shadow `0 0 0 0 var(--accent-muted)` -> `0 0 0 6px transparent`, repeats every 4s | 2000ms | standard |

### Reduced motion

Respect `prefers-reduced-motion: reduce`. When active:
- All transitions become instant (duration: 0ms)
- Staggered animations disabled (all items appear simultaneously)
- Capture button pulse disabled
- Graph animations use `animate: false` in Cytoscape config

---

## 9. Iconography

Use **Lucide** icons (https://lucide.dev). MIT licensed, consistent 24px stroke icons, tree-shakeable.

Install: `lucide-react` package.

Key icons:

| Action | Icon |
|---|---|
| Search | `Search` |
| New note / capture | `Plus` |
| Inbox | `Inbox` |
| Project / folder | `Folder` |
| Tag | `Tag` |
| Settings | `Settings` |
| Bold | `Bold` |
| Italic | `Italic` |
| Heading | `Heading` |
| Link | `Link` |
| Wikilink | `Link2` |
| Code | `Code` |
| List | `List` |
| Checklist | `ListChecks` |
| Editor view | `PenLine` |
| Split view | `Columns2` |
| Preview view | `Eye` |
| Right panel toggle | `PanelRight` |
| Delete | `Trash2` |
| Close / dismiss | `X` |
| Arrow right (send) | `ArrowUp` |
| Graph | `Network` |
| Timeline | `Calendar` |
| Chat / Ask Seam | `MessageCircle` |
| Expand sidebar | `PanelLeftOpen` |
| Collapse sidebar | `PanelLeftClose` |
| Sort | `ArrowUpDown` |
| More menu | `MoreHorizontal` |
| External link | `ExternalLink` |
| Voice capture | `Mic` |
| URL capture | `Globe` |
| Template | `FileText` |
| AI assist | `Sparkles` |
| Copy | `Copy` |
| Check (saved) | `Check` |

Icon size: 16px in buttons and nav items, 20px in toolbar, 24px in empty states.
Stroke width: 1.5px (default for Lucide).

---

## 10. CSS Architecture

### Approach

CSS Modules for component-scoped styles. Global CSS variables for the design system.

### File structure

```
web/src/
  styles/
    variables.css        -- all CSS custom properties from this document
    reset.css            -- minimal CSS reset (box-sizing, margin, font smoothing)
    global.css           -- base element styles (body, a, code, mark)
    fonts.css            -- @import for Google Fonts
    codemirror-theme.css -- CodeMirror 6 dark theme overrides
    cytoscape-theme.css  -- Cytoscape.js node/edge styles (can also be inline config)
  components/
    Sidebar/
      Sidebar.tsx
      Sidebar.module.css
    NoteCard/
      NoteCard.tsx
      NoteCard.module.css
    ...
```

### Global styles (global.css)

```css
body {
  background: var(--bg-deep);
  color: var(--text-primary);
  font-family: var(--font-ui);
  font-size: var(--font-size-base);
  line-height: var(--line-height-normal);
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

a {
  color: var(--accent-primary);
  text-decoration: none;
}
a:hover {
  text-decoration: underline;
}

::selection {
  background: var(--accent-muted);
  color: var(--text-primary);
}

:focus-visible {
  outline: 2px solid var(--accent-primary);
  outline-offset: 2px;
}

/* Scrollbar styling (Webkit) */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
::-webkit-scrollbar-track {
  background: transparent;
}
::-webkit-scrollbar-thumb {
  background: var(--border-default);
  border-radius: var(--radius-full);
}
::-webkit-scrollbar-thumb:hover {
  background: var(--border-strong);
}
```

### CSS Modules conventions

- One `.module.css` per component
- Class names: camelCase (e.g., `.noteCard`, `.activeItem`)
- No nesting deeper than 2 levels
- No `!important`
- Media queries inside the module file, not in a separate responsive file
- Compose shared utilities via CSS variable references, not `composes`

### CSS variable scoping

All design tokens live on `:root` in `variables.css`. Components reference them directly. No component overrides `:root` variables. If a component needs a variant color, compute it inline (e.g., `color-mix()` or `rgba()` for opacity variants).

---

## 11. Keyboard Shortcuts

Global shortcuts, active when no input is focused (unless noted).

| Shortcut | Action |
|---|---|
| `/` | Focus sidebar search |
| `Cmd+K` / `Ctrl+K` | Open command palette |
| `Cmd+N` / `Ctrl+N` | Open quick capture modal |
| `Cmd+S` / `Ctrl+S` | Save current note (also auto-saves) |
| `Cmd+\` / `Ctrl+\` | Toggle sidebar |
| `Cmd+B` / `Ctrl+B` | Bold (in editor) |
| `Cmd+I` / `Ctrl+I` | Italic (in editor) |
| `Cmd+[` / `Ctrl+[` | Navigate back |
| `Cmd+]` / `Ctrl+]` | Navigate forward |
| `Escape` | Close modal / deselect / exit search |

---

## 12. Accessibility

- All interactive elements are keyboard-navigable (Tab, Enter, Space, Escape, arrows)
- Focus indicators: 2px `--accent-primary` outline (not hidden, even for mouse users using `:focus-visible`)
- ARIA labels on icon-only buttons
- ARIA roles on sidebar navigation (`role="navigation"`), note list (`role="list"`), modal (`role="dialog"`)
- Color contrast: all text/background combinations meet WCAG AA (4.5:1 for normal text, 3:1 for large text). The warm off-white on dark backgrounds achieves ~12:1.
- `prefers-reduced-motion` respected (Section 8)
- Semantic HTML: use `<nav>`, `<main>`, `<aside>`, `<article>`, `<section>`, `<header>`, `<footer>` appropriately
- Screen reader announcements for: toast notifications (`role="status"`), live search results (`aria-live="polite"`), streaming chat messages (`aria-live="polite"`)
- Skip-to-content link (hidden, visible on focus) that jumps past sidebar to main content

---

## 13. Additional Frontend Dependencies

Beyond those listed in IMP_PLAN.md Section 5:

```json
{
  "dependencies": {
    "lucide-react": "^0.500",
    "motion": "^12",
    "markdown-it": "^14",
    "date-fns": "^4"
  }
}
```

| Package | Purpose |
|---|---|
| `lucide-react` | Icon library (Section 9) |
| `motion` | Animation orchestration for staggered lists, layout transitions, modal enter/exit (CSS handles simple hover/focus) |
| `markdown-it` | Markdown rendering in preview pane. Lightweight, extensible, supports plugins for wikilinks and task lists. |
| `date-fns` | Date formatting ("3 hours ago", "Mar 8, 2026"). Tree-shakeable, no locale bloat. |

---

*Last updated: 2026-03-08*
