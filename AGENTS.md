# AGENTS.md

Instructions for AI coding agents operating in this repository.

## Project overview

Seam is a local-first, AI-powered knowledge system. Go backend (REST + WebSocket), Bubble Tea TUI, React web frontend. Multi-user, single machine. Notes are plain `.md` files on disk. AI via Ollama (local). See README.md for architecture and feature overview.

## Build and run

```bash
make build                # build seamd (server) + seam (TUI) to ./bin/
make run                  # build and run the server
make dev-web              # run React dev server (Vite, port 5173, proxies /api to :8080)
make clean                # remove build artifacts + web/dist
```

## Testing

```bash
make test                 # all Go unit tests
make test-web             # all frontend tests (Vitest)
make test-integration     # integration tests (real filesystem, on-disk SQLite)

# Run a single Go test
go test ./internal/note/ -run TestService_Create_WritesFile -v

# Run all tests in one package
go test ./internal/note/ -v

# Run tests matching a pattern
go test ./internal/note/ -run "TestStore_.*" -v

# Run with race detector
go test -race ./internal/...

# Frontend tests
cd web && npx vitest run                    # all
cd web && npx vitest run src/api/client     # single file
```

Build tags for test tiers:
- Default: unit tests only. No filesystem, no external services.
- `//go:build integration`: real filesystem, on-disk SQLite.
- `//go:build external`: requires running Ollama and/or ChromaDB.
- `//go:build performance`: benchmarks, not for CI.

## Linting and formatting

```bash
make lint                 # golangci-lint + eslint
make fmt                  # gofmt + prettier
```

## Go style rules

### General

- Go 1.25+. No CGO. Pure Go SQLite driver (`modernc.org/sqlite`).
- Never use emojis in code or comments.
- Never include credit or attribution lines in commits, PRs, or code comments.
- Format with `gofmt`. No exceptions.

### Package structure

Each domain package follows this layout:
- `handler.go` -- HTTP handlers (chi router)
- `service.go` -- business logic
- `store.go` -- SQLite data access
- `{feature}.go` -- pure functions (parsers, etc.)
- `*_test.go` -- tests alongside source

Strict layering. No circular imports:
```
cmd/ -> internal/server -> internal/{domain packages}
internal/{domain} -> internal/userdb, internal/ws, internal/ai
```
No package imports `internal/server`. The server wires dependencies at startup.

### Naming conventions

- Interfaces: verb-noun or role name (`Store`, `Manager`, `Queue`), not `IStore`.
- Constructors: `New{Type}(deps) *Type`.
- Domain errors: `Err{Condition}` as package-level `var` -- e.g., `var ErrNotFound = errors.New("not found")`.
- Test functions: `Test{Unit}_{Scenario}_{Expected}`.
- Test files: `{source}_test.go` in the same package.
- IDs: ULID everywhere (`github.com/oklog/ulid/v2`). Never UUID.

### Error handling

- Wrap errors with context: `fmt.Errorf("note.Service.Create: %w", err)`.
- Domain errors are typed `var` sentinels, checked with `errors.Is()`.
- Handlers map domain errors to HTTP status codes:
  - `ErrNotFound` -> 404
  - `ErrInvalidCredentials` -> 401
  - `ErrUserExists` -> 409
  - Unknown -> 500 (logged with full context, sanitized response to client)
- Never expose internal error details in HTTP responses.

### Logging

- Use `log/slog` (stdlib). Structured JSON in production, text in development.
- Levels: DEBUG (per-request), INFO (lifecycle), WARN (recoverable), ERROR (failures).
- Every request has a request ID from middleware. Include it in all log entries.
- Never log note content or user data beyond IDs.

### Context

- All service and store methods take `context.Context` as the first parameter.
- Pass `ctx` through the full call chain. Never use `context.Background()` in request handlers.

### Database

- SQLite with WAL mode and foreign keys ON. Always.
- Per-user `seam.db` for notes/projects/links/FTS/AI tasks.
- Shared `server.db` for user accounts and refresh tokens.
- Migrations: embedded SQL files via `go:embed`. Run on DB open. Must be idempotent.
- FTS5 uses external-content mode (`content='notes'`) -- body text is stored in `notes.body` (denormalized from `.md` files) so that SQLite triggers can automatically sync the FTS index on insert, update, and delete. The `.md` file on disk is always source of truth; `notes.body` is a copy for FTS.
- Use `content_hash` (SHA-256 of the full file, frontmatter + body) to skip re-indexing unchanged files.

### HTTP handlers

- Use `chi` router. Mount sub-routers per domain (`r.Route("/api/notes", noteHandler.Routes)`).
- Validate all input at handler level before calling service.
- Use `httptest.NewRecorder()` + `chi.NewRouter()` for handler tests.
- Mock the service layer via interfaces, not the full server.

### Testing

- Use `testify/require` (fail fast), not `assert`.
- Table-driven tests for functions with multiple input variations.
- SQLite tests use named in-memory DBs for test isolation: `sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))`. Do NOT use `file::memory:?cache=shared` (unnamed shared-cache) because all tests sharing that URI would collide.
- External services (Ollama, ChromaDB) mocked with `httptest.NewServer()`. Never call real services in unit tests.
- Each test is independent. No shared mutable state between test functions.
- No `time.Sleep` for synchronization. Use channels or `sync.WaitGroup`.
- Test helpers in `internal/testutil/`: `TestServerDB(t)`, `TestUserDB(t)`, `TestDataDir(t)`.

### Security invariants

- Path traversal: reject any file path containing `..`, absolute paths, or null bytes.
- User isolation: resolve user ID from JWT in middleware, pass to service layer. Never accept user ID from request body/params.
- Input validation: sanitize note titles, project names, tags for filesystem safety (no `/`, `\`, `..`, `\x00`).
- SSRF: URL capture must reject private IPs, localhost, `file://` protocol.

## Frontend style rules (web/)

- React 19 + TypeScript 5 + Vite 7.
- State management: Zustand.
- Markdown editor: CodeMirror 6 with custom dark theme.
- Markdown preview: `markdown-it` with wikilink plugin.
- Graph visualization: Cytoscape.js with `fcose` layout.
- Icons: Lucide (`lucide-react`). No other icon libraries.
- Animations: CSS transitions for hover/focus, `motion` (framer-motion successor) for orchestrated sequences.
- Date formatting: `date-fns`.
- Tests: Vitest + React Testing Library.
- Dark theme only. Use CSS variables for all colors (no hardcoded values).

### Design tokens

- All colors, spacing, typography, radii, z-indexes, animation easings defined as CSS custom properties in `web/src/styles/variables.css`.
- Four font families: Fraunces (display), Outfit (UI), Lora (content), IBM Plex Mono (code). Loaded via Google Fonts in `index.html`.
- Primary accent: amber/copper (`--accent-primary: #c4915c`). This is the "seam" -- golden thread linking ideas.
- Text: warm off-white (`--text-primary: #e8e2d9`), not pure white.
- Components use CSS Modules (`.module.css` per component, camelCase class names).
- Never hardcode color values, spacing, or font sizes. Always reference CSS variables.
- Never use `!important`. No nesting deeper than 2 levels in CSS Modules.

## Key files

| File | Purpose |
|---|---|
| `README.md` | Architecture, features, API reference, getting started |
| `seam-server.yaml.example` | Server configuration template |
| `migrations/server/` | server.db SQL migrations |
| `migrations/user/` | per-user seam.db SQL migrations |
