# AGENTS.md

Instructions for AI coding agents operating in this repository.

## Project overview

Seam is a local-first, AI-powered knowledge system. Go backend (REST + WebSocket), Bubble Tea TUI, React web frontend. Single-user, single machine. Notes are plain `.md` files on disk. AI via Ollama (local). See `docs/` for architecture, API reference, and feature details.

## Build and run

```bash
make build                # build seamd (server) + seam (TUI) to ./bin/
make run                  # build and run the server
make dev-web              # run React dev server (Vite, port 5173, proxies /api to :8080)
make clean                # remove build artifacts + web/dist

# Optional ChromaDB container (chosen via `make init`)
make chroma-up            # start the Seam-managed ChromaDB container
make chroma-down          # stop and remove the container
make chroma-logs          # follow container logs
make chroma-status        # show container status

# Service install (also offers an optional Chroma supervisor)
make install-service      # install seamd as launchd (macOS) or systemd --user (Linux)
make uninstall-service    # remove the service(s)
```
Note: When working on tasks, use `seam` MCP tools to track session progress and store memories.

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
- Single `seam.db` at `{data_dir}/seam.db` for everything (owner account, notes, projects, links, FTS, AI tasks, settings, etc.).
- Migrations: single embedded SQL file (`migrations/001_initial.sql`) via `go:embed`. Run on DB open. Must be idempotent.
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
- Test helpers in `internal/testutil/`: `TestDB(t)`, `TestDataDir(t)`.

### Security invariants

- Path traversal: reject any file path containing `..`, absolute paths, or null bytes.
- Owner identity: resolve user ID from JWT in middleware, pass to service layer. Never accept user ID from request body/params.
- Input validation: sanitize note titles, project names, tags for filesystem safety (no `/`, `\`, `..`, `\x00`).
- SSRF: URL capture must reject private IPs, localhost, `file://` protocol.

## Common pitfalls

The patterns below have been re-introduced multiple times across audit passes. Treat
this section as a checklist before opening a PR.

### Meta-rules

1. **Propagate every fix.** When you fix a buggy pattern, `grep` the entire repo for
   other instances of the same pattern and fix them all in the same change. A bug fix
   that touches only the file flagged in the report is incomplete by default. The
   "Forbidden APIs" and "Required APIs" lists below double as grep targets.
2. **Never re-introduce a banned pattern in new code.** When adding a new file or
   package, scan it against the lists below before declaring done.
3. **After any interface, store-method, schema, or migration change, run
   `make build && make test` before declaring done.** Search the test tree
   (`*_test.go`, `mock*.go`, `fake*.go`) for the changed symbol and update mocks. Test
   mocks falling out of sync with production interfaces is a recurring class of break.
4. **No fake results on error.** Helpers must not swallow an error and return a
   plausible-looking dummy value (e.g. `mustMarshal` returning `{"error":"..."}`). The
   LLM and downstream code cannot distinguish a real result from a fabricated one.
   Return the error.

### Forbidden APIs

| Pattern | Why | Use instead |
|---------|-----|-------------|
| `ulid.MustNew` | Panics if `crypto/rand` entropy fails. Crashes the server in request paths. | `ulid.New(ulid.Now(), rand.Reader)` + return the error |
| `time.Parse(...)` with discarded error (`_, _ = time.Parse(...)`) | Silently produces zero-value timestamps that propagate into responses. | Capture the error and `slog.Warn(...)` (do not return zero-value times to clients) |
| `os.WriteFile` on a `.md` file | Non-atomic; a crash mid-write corrupts the source-of-truth file. | `note.AtomicWriteFile` (writes to temp file in same dir + `fsync` + `rename`) |
| `s[:N]` on text destined for users, LLMs, or HTTP responses | Slices by byte and splits multi-byte UTF-8 (CJK, emoji) producing invalid UTF-8. | `string([]rune(s)[:N])` or a `runeSafeTruncate` helper. Pair with `utf8.RuneCountInString` for budget math, never `len()`. |
| `err == ErrXxx`, `err == sql.ErrNoRows` | Breaks silently when an upstream wraps the error. | `errors.Is(err, ErrXxx)` |
| `_ := json.Marshal(...)`, `_ := json.Unmarshal(...)` | Marshal can fail (NaN floats, unsupported types). Unmarshal failures silently produce zero-value structs. | Check the error; `slog.Warn` and propagate or fall through. |
| `_ := result.RowsAffected()` (or no `RowsAffected` check at all on UPDATE/DELETE) | Updates/deletes against missing rows look like success; not-found bugs hide. | Check `n, err := result.RowsAffected(); ... if n == 0 { return ErrNotFound }` |
| `close(ch)` outside `sync.Once.Do(...)` in a `Close()` method | Double-`close` panics. Multiple shutdown paths exist. | `closeOnce sync.Once`; `h.closeOnce.Do(func() { close(h.done) })` |
| `string + "/" + string` for filesystem paths | Inconsistent with codebase, breaks on Windows, easier to introduce traversal. | `filepath.Join(a, b)` |
| `context.Background()` inside an HTTP handler or any goroutine spawned by one | Leaks request-scoped values and disconnects shutdown signaling. | Derive from request `ctx` (or use `context.WithoutCancel(ctx)` for background work that must outlive the response). For long-lived service goroutines, take a context in the constructor and select on it. |
| `os.Stat` or `filepath.WalkDir` in a path that crosses user data | Follows symlinks; a malicious or stray symlink leaks files outside the notes dir. | `os.Lstat` and explicit `d.Type()&os.ModeSymlink != 0` skip in `WalkDir` callbacks. |
| `strings.Contains(err.Error(), "...")` for status mapping | Couples the handler to error message text; fragile across provider changes. | Define typed sentinel errors; map them with `errors.Is`. |
| `mustMarshal`-style helpers that fabricate fake JSON on error | LLM/caller cannot distinguish real `{"error":...}` from fake. | Return `(json.RawMessage, error)` and propagate. |
| Returning `(nil, nil)` from a store method when the row is missing | Future callers forget the nil check and dereference. | Return `(nil, ErrNotFound)`. |
| Goroutine started with no `done` channel / `WaitGroup` for shutdown | Background work continues after `userdb.CloseAll()` and writes to a closed DB. | Constructor takes `done chan struct{}`; `Run` selects on it; shutdown closes the channel and waits. Match the existing `aiQueueDone`/`schedDone` pattern in `cmd/seamd/main.go`. |
| New `migrations/*.sql` file without an entry in `migrations/migrations.go` | The file is created but never `go:embed`-ed; the migration silently does not run. | When adding a SQL file, also add `//go:embed user/NNN_*.sql` and append to `UserMigrations()`. Verify with a fresh `seam.db`. |
| `time.Sleep` for synchronization | Flaky tests, masks real ordering bugs. | Channels, `sync.WaitGroup`, or `t.Eventually` patterns. |

### Required APIs and patterns

- **Atomic markdown writes**: every write to a `.md` file goes through
  `note.AtomicWriteFile` (`internal/note/service.go`). Includes `Create`, `Update`,
  `Reindex`, the rollback path inside `Update`, the `cmd/seamd/main.go` frontmatter
  updater closure, and `template/service.go`.
- **DB-then-file ordering**: any operation that touches both the DB and the filesystem
  must commit the DB transaction *first*, then perform the filesystem mutation in a
  post-commit step. See `BulkAction` in `note/service.go` for the canonical pattern
  (`pendingMoves`/`filesToDelete`). On rollback, also undo any partial filesystem
  state — the `Update` move path must `os.Remove` the new-path file before reverting
  the rename.
- **`Set*` setter functions are mutex-protected**: any `Service.Set*` method that
  installs a dependency callback after construction must hold a `sync.RWMutex`.
  Reads go through a `getX()` accessor. See `capture/service.go`'s
  `getSummarizeFunc()` for the canonical pattern.
- **`rows.Err()` after every loop**: every `for rows.Next() { ... }` is followed by
  `if err := rows.Err(); err != nil { ... }`. The `rowserrcheck` linter enforces this.
- **`http.MaxBytesReader` on every body decode**: every JSON or multipart handler
  starts with `r.Body = http.MaxBytesReader(w, r.Body, 1<<20)` (or `25<<20` for voice
  upload). Includes `template/handler.go:apply`, `capture/handler.go` voice path,
  every assistant handler.
- **LIKE wildcard escaping**: any user input fed into `LIKE` must go through an
  escape that handles `\`, `%`, and `_` *in that order* (escape `\` first so the
  later wildcard escapes are not re-doubled), and the query must include
  `ESCAPE '\'`. `escapeLIKE` is for `LIKE` only — never apply it to `=` comparisons
  (it transforms `_` and `%`, which become literal characters in equality match).
- **FTS5 MATCH sanitization**: every FTS5 `MATCH` on user input goes through
  `sanitizeMemoryFTSQuery` (or an equivalent that strips operators and quotes terms).
  Raw user text in `MATCH` causes SQLite errors and a class of injection.
- **LLM message-history slicing must respect tool-use grouping**: any positional cut
  of conversation history must use `safeRecentBoundary`-style logic to avoid
  orphaning a `tool` message from its parent assistant `tool_calls`. OpenAI and
  Anthropic both reject orphaned tool results with a 400.
- **Slice copying when extending shared slices**: `append(args, ...)` may share the
  backing array. If the resulting slice is later mutated independently, copy first:
  `dst := append(append([]T{}, args...), extra...)`. Especially in SQL arg builders.
- **Sentinel errors via `errors.New`**, not `fmt.Errorf` (without `%w`). Reserve
  `fmt.Errorf` for wrapped errors with a `%w` verb.
- **`filepath.Join` everywhere**, including in tests and in `cmd/`.

### Verification before declaring done

1. `make build && make test` (must include `*_test.go` updates if any interface
   signature changed).
2. `make lint` (catches `ulid.MustNew`, missing `rows.Err()`, blank-discarded errors,
   and a handful of other patterns automatically — see `.golangci.yml`).
3. For any change that touches a recurring pattern from this section, grep the repo
   for other instances of the same pattern and fix them in the same change.

## Accepted designs (don't "fix")

These look like bugs and have been flagged by past audits as bugs. They are
intentional. Don't "fix" them without first reading the rationale and changing
the rationale.

- **`task.Service.ToggleDone` holds a DB transaction across file I/O.** The
  task and its parent note must be updated atomically with the file write
  to the source-of-truth `.md`. Splitting the write out of the tx
  re-introduces an orphan window.
- **`task.Service.SyncNote` matches duplicate-content tasks by order, not
  by content hash.** When a note has two tasks with the same body text, we
  preserve their state by position rather than by collision-prone hashing.
  Fragile-looking but deliberate.
- **Deep semantic-search pagination + recency returns empty past
  `limit*3`.** The fallback recency layer is bounded so a pathological
  client cannot pin a request walking the full notes table. Empty results
  past that boundary are the intended signal.
- **Path-traversal validation does not resolve symlinks.** `FilePath`
  values come from the internal `notes` table, never from request input,
  so the source is already trusted. Adding `filepath.EvalSymlinks` here
  would create an unrelated TOCTOU race.
- **`note.toggleCheckboxInFile` has no file-level lock.** Single-user
  invariant: only one writer at a time. Adding a per-file mutex here
  buys nothing today and would have to be torn out if the architecture
  ever returns to multi-tenant.
- **Service / store APIs still take a `userID` parameter even though it
  is always `userdb.DefaultUserID` in production.** This is the forward
  path back to multi-tenant. Don't strip it.

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
| `README.md` | Project overview and quick start |
| `BRAND.md` | Visual identity, colors, fonts, logo usage |
| `docs/getting-started.md` | Prerequisites, installation, configuration |
| `docs/architecture.md` | System diagram, tech stack, data format, project structure |
| `docs/ai.md` | LLM providers, AI features, task queue |
| `docs/api.md` | REST endpoints, WebSocket events |
| `docs/mcp.md` | MCP agent memory tools |
| `docs/development.md` | Build, test, lint commands |
| `docs/security.md` | Security model and invariants |
| `seam-server.yaml.example` | Server configuration template |
| `migrations/001_initial.sql` | Complete database schema (single flattened migration) |
| `docker/chroma-compose.yml` | Pinned ChromaDB container with bind-mounted volume under `${SEAM_DATA_DIR}/chromadb` |
| `scripts/chroma-supervisor.sh` | Wakes Docker on demand, waits for the daemon, then `exec`s `docker compose up` for the chroma service. Run by the optional supervisor unit. |
| `scripts/install-service.sh` | Installs seamd as launchd/systemd, optionally installs the chroma supervisor as a sibling unit |
| `scripts/uninstall-service.sh` | Symmetric removal of both service units |
