# Seam Brand Guidelines

Brand reference for contributors and designers working on Seam.

## Name

**Seam** -- always capitalized when used as a proper noun in prose. Lowercase `seam` in code identifiers, CLI binary names, file paths, and env var prefixes.

| Context               | Form              | Example                                |
| --------------------- | ----------------- | -------------------------------------- |
| Prose / UI headings   | Seam              | "Ask Seam"                             |
| CLI binary            | `seam`            | `./bin/seam`                           |
| Server binary         | `seamd`           | `./bin/seamd` (Unix daemon convention) |
| Config files          | `seam-` prefix    | `seam-server.yaml`                     |
| Environment variables | `SEAM_` prefix    | `SEAM_JWT_SECRET`                      |
| Database filename     | `seam.db`         | `{data_dir}/seam.db`                   |
| localStorage keys     | `seam_` prefix    | `seam_refresh_token`                   |
| DOM events            | `seam:` namespace | `seam:command`                         |
| HTTP headers          | `X-Seam-` prefix  | `X-Seam-Signature`                     |
| JWT issuer claim      | `seam`            |                                        |
| User-Agent            | `Seam/{version}`  | `Seam/0.1.0 (knowledge-system)`        |
| Config directory      | `~/.config/seam`  |                                        |

## Tagline

**Primary tagline (marketing):** "Where ideas connect" Used on the login page and wherever a single short tagline is needed.

**Descriptor (functional):** "A local-first, AI-powered knowledge system" Used in the Settings About section and anywhere the product needs a one-line explanation.

**Origin (README only):** "The seam is the join between two pieces; knowledge gains meaning at the intersections." Context for the name. Appears only in the README, not in the product UI.

## Logo

The logo is a minimalist line drawing: a flowing curve connecting two nodes inside a circle. It represents the core concept of Seam -- the connection between ideas.

- Source file: `resources/logo.svg` (120x120, stroke-based)
- Mark variant: `resources/logo-mark.svg` (simplified for small sizes, heavier strokes)
- Favicon: `web/public/seam.svg` (derived from the mark, optimized for 16-32px)

### Logo usage rules

- Always use the amber/copper color (`#c4915c`) on dark backgrounds.
- Minimum clear space: half the logo width on all sides.
- Do not stretch, rotate, or add effects.
- For very small contexts (favicons, 16px), use the mark variant without the outer circle.

## Color Palette

Dark-only theme. The palette pairs cool blue-gray backgrounds with warm amber accents and off-white text. No light mode.

### Backgrounds

| Token           | Hex       | Usage                                 |
| --------------- | --------- | ------------------------------------- |
| `--bg-deep`     | `#08090d` | Deepest background (login, app shell) |
| `--bg-base`     | `#0f1117` | Main content area                     |
| `--bg-surface`  | `#161922` | Cards, panels                         |
| `--bg-elevated` | `#1d2130` | Elevated surfaces, dropdowns          |
| `--bg-overlay`  | `#252a3a` | Modals, overlays                      |

### Borders

| Token              | Hex       | Usage              |
| ------------------ | --------- | ------------------ |
| `--border-subtle`  | `#1e2233` | Faint separators   |
| `--border-default` | `#2a3045` | Standard borders   |
| `--border-strong`  | `#3a4260` | Emphasized borders |

### Text

| Token              | Hex       | Usage                                        |
| ------------------ | --------- | -------------------------------------------- |
| `--text-primary`   | `#e8e2d9` | Main text (warm off-white, never pure white) |
| `--text-secondary` | `#9992a6` | Secondary / muted text                       |
| `--text-tertiary`  | `#5e5a6e` | Disabled / placeholder text                  |
| `--text-inverse`   | `#0f1117` | Text on accent-colored backgrounds           |

### Accents

| Token | Hex | Usage |
| --- | --- | --- |
| `--accent-primary` | `#c4915c` | **Brand color.** Links, buttons, focus rings, active states, cursor, headings. The "golden thread linking ideas." |
| `--accent-hover` | `#d4a06c` | Hover state for primary accent |
| `--accent-muted` | `rgba(196,145,92,0.10)` | Subtle highlight, selection background |
| `--accent-secondary` | `#6b9b7a` | Sage green. Code, success states |
| `--accent-tertiary` | `#7b8ec4` | Slate blue. Info states |

### Status

| Token              | Hex       | Usage                 |
| ------------------ | --------- | --------------------- |
| `--status-error`   | `#c46b6b` | Errors (muted red)    |
| `--status-warning` | `#c4a95c` | Warnings (warm amber) |
| `--status-success` | `#6b9b7a` | Success (sage green)  |
| `--status-info`    | `#7b8ec4` | Info (slate blue)     |

### TUI colors

The TUI palette is intentionally warmer in backgrounds than the web (warm brown-tinted vs cool blue-tinted) to suit terminal rendering. The accent colors are identical across both surfaces.

| Variable       | Hex       | Notes                               |
| -------------- | --------- | ----------------------------------- |
| `colorPrimary` | `#c4915c` | Same as web `--accent-primary`      |
| `colorBg`      | `#1a1816` | Warm dark (terminal readability)    |
| `colorFg`      | `#e8e2d9` | Same as web `--text-primary`        |
| `colorSuccess` | `#6b9b7a` | Aligned with web `--status-success` |

## Typography

Four font families, each with a distinct role. Self-hosted (no third-party CDN).

| CSS Variable | Font | Role | Fallback |
| --- | --- | --- | --- |
| `--font-display` | Fraunces | Wordmark, headings, note titles | Georgia, serif |
| `--font-ui` | Outfit | All UI chrome: buttons, labels, nav, body | system-ui, sans-serif |
| `--font-content` | Lora | Note body, markdown preview | Georgia, serif |
| `--font-mono` | IBM Plex Mono | Code blocks, editor, counters | Menlo, monospace |

### Why these fonts

- **Fraunces** -- Variable serif with warm, organic character. Its optical-size axis adapts to display and text contexts. Evokes craft and thoughtfulness.
- **Outfit** -- Clean geometric sans-serif. Readable at all sizes. Neutral enough to let content breathe.
- **Lora** -- Classical text serif. Optimized for screen reading. Gives note content a literary quality.
- **IBM Plex Mono** -- Humanist monospace. Distinctive without being distracting. Good for both code and data.

## Design Language: Dark Cartography

The visual identity is called "Dark Cartography" -- evoking a hand-drawn map of connected ideas.

### Principles

1. **Warm on cool.** Amber accents on blue-gray backgrounds. Organic warmth against technological precision.
2. **Serif for substance, sans for structure.** Content uses serif fonts (literary, thoughtful). UI chrome uses geometric sans (clean, functional).
3. **Subtle texture.** Repeating wave/thread SVG patterns on login and empty states reference the "seam" metaphor -- threads connecting surfaces.
4. **No harsh edges.** Rounded borders, translucent accent colors for selections, smooth animations. Nothing should feel aggressive.
5. **Restrained motion.** CSS transitions for hover/focus. Orchestrated animations only where they communicate state changes. Always respect `prefers-reduced-motion`.

### Background pattern

A repeating SVG wave pattern evokes threads and connection. Defined once in `web/src/styles/patterns.css` and used via CSS class composition in login and empty states. The pattern uses `--border-subtle` for stroke color at low opacity.

## Feature Names

| Feature         | Display Name           | Notes                            |
| --------------- | ---------------------- | -------------------------------- |
| AI chat         | Ask Seam               | The AI persona in chat is "Seam" |
| Note capture    | Quick Capture          |                                  |
| URL bookmarking | Capture URL            |                                  |
| Voice input     | Voice Capture          |                                  |
| AI writing help | AI Writing Assist      |                                  |
| Graph view      | Knowledge Graph        |                                  |
| MCP integration | Seam (MCP server name) |                                  |

## File & Asset Inventory

| Asset              | Path                              | Purpose                         |
| ------------------ | --------------------------------- | ------------------------------- |
| Logo (full)        | `resources/logo.svg`              | Full logo with circle and curve |
| Logo (mark)        | `resources/logo-mark.svg`         | Simplified for small sizes      |
| Favicon (SVG)      | `web/public/seam.svg`             | Browser tab icon                |
| Favicon (ICO)      | `web/public/favicon.ico`          | Legacy browser fallback         |
| Apple touch icon   | `web/public/apple-touch-icon.png` | iOS home screen                 |
| Web manifest       | `web/public/manifest.json`        | App identity for browsers       |
| Background pattern | `web/src/styles/patterns.css`     | Shared wave pattern             |
| CSS variables      | `web/src/styles/variables.css`    | All design tokens               |
| Font files         | `web/public/fonts/`               | Self-hosted WOFF2 files         |
