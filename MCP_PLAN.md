# MCP Agent Memory System - Implementation Plan

## Overview

Make AI agents first-class citizens in Seam by exposing an MCP (Model Context Protocol)
server that gives agents persistent, structured long-term memory. Agents use Seam to
store session plans, track progress, accumulate domain knowledge, and search across both
their own memory and the user's knowledge base.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Protocol | MCP only (Streamable HTTP) | Standard for Claude Code, Cursor, Windsurf. Integrated into existing HTTP server. |
| Storage | Single `agent-memory` project | All agent notes in one project, organized by tags. Keeps user space clean. |
| Sessions | Named sessions | Human-readable names (e.g., "refactor-auth"), mapped to ULIDs internally. Agents resume by name. |
| Session metadata | Hybrid (table + notes) | `agent_sessions` table for lifecycle tracking, notes for actual content (plan, progress, context). |
| Access scope | Full read + write with attribution | Agent reads all user notes, creates user notes tagged `created-by:agent`. |
| Knowledge mutation | Full CRUD | Agent decides when to create, update, restructure, or delete knowledge notes. |
| Embeddings | Same ChromaDB collection + metadata filter | Agent notes embedded alongside user notes. Metadata `agent:true` enables filtered search. |
| MCP library | `github.com/mark3labs/mcp-go` (v0.45.0) | MIT license, well-maintained, supports Streamable HTTP transport. Pre-v1 (unstable API). |
| MCP features | Tools only (no resources/prompts) | Simpler. Tools cover all agent needs. Resources add complexity without benefit here. |
| Auth | `WithHTTPContextFunc` on StreamableHTTPServer | mcp-go provides `HTTPContextFunc` to inject context from HTTP request. Use it to extract JWT and inject user ID. Existing `server.AuthMiddleware` returns JSON errors on 401, which is compatible. |

## Memory Taxonomy

All agent notes live flat under the `agent-memory/` project directory. Organization is
via tags, not subdirectories (see "File Paths" section for rationale).

| Memory Type | Lifecycle | Note Title Pattern | Tags |
|---|---|---|---|
| SESSION_PLAN | Per-session | `Session Plan: {name}` | `session:{name}`, `type:plan`, `status:active` |
| SESSION_PROGRESS | Per-session, updated per-task | `Session Progress: {name}` | `session:{name}`, `type:progress`, `status:active` |
| SESSION_CONTEXT | Per-session, evolving | `Session Context: {name}` | `session:{name}`, `type:context`, `status:active` |
| DOMAIN_KNOWLEDGE | Persistent across sessions | `Knowledge: {category} - {name}` | `type:knowledge`, `domain:{category}` |
| GENERAL_NOTES | Persistent across sessions | `{name}` | `type:general` |

All agent notes also carry the tag `created-by:agent`.

Session notes for hierarchical sessions flatten `/` to ` - ` in the title (e.g.,
session `refactor-auth/analyze` has plan note titled
"Session Plan: refactor-auth - analyze"). The tag `session:refactor-auth/analyze`
preserves the exact hierarchy. The filename is derived by `project.Slugify` and is
not used for lookup (tags are canonical).

## Subagent Coordination & Context Budgeting

### Problem

Agents (and their subagents) need relevant context from past sessions and accumulated
knowledge, but context windows are expensive. Dumping raw notes into context "muds" the
window with low-signal content. We need:

1. **Automatic relevant context** on session start -- no extra tool call needed
2. **Hierarchical sessions** so subagent work flows back to the parent automatically
3. **Size budgets** so retrieved context stays compact (measured in characters, not tokens,
   since token counts vary by model -- characters are a reliable proxy)

### Hierarchical Sessions via Naming Convention

Sessions form an implicit tree using `/` as a path separator:

```
refactor-auth                          <- root session (main agent)
refactor-auth/analyze-middleware       <- child session (subagent A)
refactor-auth/implement-api-keys      <- child session (subagent B)
refactor-auth/implement-api-keys/db   <- grandchild (sub-subagent)
```

Rules:
- `session_start("refactor-auth/analyze-middleware")` automatically links to
  parent session `refactor-auth`
- The parent does NOT need to exist first (subagent can start independently)
- When a child calls `session_end`, its `findings` are stored as a compact summary
  and tagged with the parent session name for automatic retrieval

### The Briefing: Automatic Context on Session Start

`session_start` returns a **briefing** -- a pre-assembled, size-budgeted context
package. The agent gets everything it needs in one call, no follow-up search required.

**Briefing assembly (server-side):**

```
session_start(name, max_context_chars=4000)
  |
  |-- 1. Session state (if resuming)
  |     Plan + last progress entry. Truncated to 30% of budget.
  |
  |-- 2. Parent context (if child session)
  |     Parent's plan. Truncated to 20% of budget.
  |
  |-- 3. Sibling findings (if siblings completed)
  |     Compact findings from completed sibling sessions.
  |     Most recent first. Truncated to 25% of budget.
  |
  |-- 4. Relevant knowledge (always)
  |     Semantic search on session name + parent plan.
  |     Top matches from knowledge base. Truncated to 25% of budget.
  |
  |-- Total <= max_context_chars (default 4000)
```

Each section is independently truncated to its budget slice. If a section is empty
(e.g., no siblings), its budget redistributes to the others.

**Budget defaults:**

| Agent Type | Default `max_context_chars` | Rationale |
|---|---|---|
| Main agent | 4000 (~1000 tokens) | Has full session, needs broader context |
| Subagent | 3000 (~750 tokens) | Focused task, needs less background |
| Deep subagent (2+ levels) | 2000 (~500 tokens) | Very focused, minimal context |

The agent can override `max_context_chars` in the `session_start` call.

### Compact Findings: What Flows Between Agents

When an agent calls `session_end`, the `findings` parameter is **required** (not
optional). This is the compact summary that parent and sibling agents will see.

Guidelines for findings (enforced by character limit):
- Max 1500 characters (~375 tokens)
- Should answer: "What did you discover/accomplish/decide?"
- Server rejects findings over the limit

The full session detail (plan, progress log, context notes) remains in the notes for
human review or deep retrieval, but only the compact findings flow into other agents'
briefings.

### Context Gather: On-Demand Search (Also Budgeted)

`context_gather` is for mid-session retrieval when the agent needs more context.
It also respects a size budget:

```
context_gather(query, max_context_chars=3000, scope=all)
  |
  |-- Semantic search across notes (filtered by scope)
  |-- Results ranked by relevance
  |-- Each result: title + snippet (not full body)
  |-- Total output truncated to max_context_chars
```

Snippets are extracted around the matching chunks, not full note bodies. This keeps
results dense with relevant content.

### Data Flow Example

```
Main Agent (session: "refactor-auth")
  |
  |-- session_start("refactor-auth")
  |     Briefing: {
  |       knowledge: ["Go middleware patterns", "auth module notes"]  // from past sessions
  |     }
  |
  |-- session_plan_set(plan with 3 tasks)
  |
  |-- spawns Subagent A
  |     session_start("refactor-auth/analyze-middleware")
  |       Briefing: {
  |         parent_plan: "refactor-auth plan (truncated to budget)"
  |         knowledge: ["middleware patterns"]
  |       }
  |     ... does work ...
  |     session_end(findings: "Current middleware uses chain-of-responsibility.
  |       Auth check is in ValidateToken(). Rate limiting not implemented.
  |       JWT validation at internal/auth/middleware.go:45.")
  |
  |-- spawns Subagent B
  |     session_start("refactor-auth/implement-api-keys")
  |       Briefing: {
  |         parent_plan: "refactor-auth plan (truncated)"
  |         sibling_findings: [
  |           "analyze-middleware: Current middleware uses chain-of-responsibility..."
  |         ]
  |         knowledge: ["API key best practices"]
  |       }
  |     ... does work, informed by Subagent A's findings ...
  |     session_end(findings: "API key table added. Store + service implemented.
  |       Keys are SHA-256 hashed. Prefix 'sk_' for identification.")
  |
  |-- Main agent calls context_gather("subagent results for refactor-auth")
  |     Returns: both subagents' findings, ranked and budgeted
  |
  |-- session_end(findings: "Auth refactored to support API keys + JWT.
  |     Rate limiting added. 3 new files, 2 modified. All tests pass.")
```

## Agent Lifecycle Flow

```
1. Agent connects via MCP (Streamable HTTP POST to /api/mcp)
   - Authenticates with Bearer JWT token (Authorization header)
   - MCP session established (Mcp-Session-Id response header)

2. Agent calls session_start(name: "refactor-auth", max_context_chars: 4000)
   - Server creates or resumes the named session
   - Returns: session status + briefing (parent plan, sibling findings,
     relevant knowledge -- all within the character budget)

3. Agent calls session_plan_set(tasks: [...])
   - Creates/updates the session plan note in agent-memory project

4. For each task:
   a. session_progress_update(task: "...", status: "in_progress")
   b. Agent does its work (external to Seam)
   c. Optionally: context_gather() for mid-task retrieval (budgeted)
   d. session_progress_update(task: "...", status: "completed", notes: "...")
   e. Optionally: memory_write() or memory_append() to update domain knowledge
   f. Optionally: spawn subagent with child session name

5. Agent calls session_end(findings: "compact summary of what was accomplished")
   - Session status set to "completed"
   - Findings stored for parent/sibling retrieval
   - Session notes tagged status:completed
```

## Database Schema

### New migration: `migrations/user/002_agent_sessions.sql`

> **Note:** The current codebase has only one user migration (`001_initial.sql`).
> This migration must also be registered in `migrations/migrations.go` (add a new
> `//go:embed` var and append to `UserMigrations()`).
>
> **Critical testutil change:** `internal/testutil/testutil.go`'s `TestUserDB(t)` calls
> `OpenTestDB(t, migrations.UserSQL)` which only passes the 001 SQL string. It does NOT
> iterate `UserMigrations()`. To pick up the new 002 migration, `TestUserDB` must be
> changed to either:
>   (a) Concatenate all migration SQL strings: `migrations.UserSQL + "\n" + migrations.UserSQL002`, or
>   (b) Loop through `migrations.UserMigrations()` and execute each `Migration.SQL` in order.
> Option (b) is more maintainable for future migrations. Note that `OpenTestDB` runs
> `db.Exec(sql)` directly (not `migrations.Run`), so there is no version tracking in
> test DBs -- this is intentional and fine for tests.

```sql
-- Agent session tracking (lifecycle metadata).
-- Actual session content (plan, progress, context) stored as notes in agent-memory project.
-- Sessions form a tree via parent_session_id (derived from "/" naming convention).
CREATE TABLE IF NOT EXISTS agent_sessions (
    id                TEXT PRIMARY KEY,                -- ULID
    name              TEXT NOT NULL UNIQUE,            -- hierarchical name ("refactor-auth/analyze")
    parent_session_id TEXT REFERENCES agent_sessions(id) ON DELETE SET NULL,  -- NULL = root session
    status            TEXT NOT NULL DEFAULT 'active',  -- active, completed, archived
    findings          TEXT,                            -- compact summary (max 1500 chars), set on session_end
    metadata          TEXT NOT NULL DEFAULT '{}',      -- JSON: agent identity, config
    created_at        TEXT NOT NULL,                   -- RFC3339
    updated_at        TEXT NOT NULL                    -- RFC3339
);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_status ON agent_sessions(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_name ON agent_sessions(name);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_parent ON agent_sessions(parent_session_id);

-- Tool call audit log. Every MCP tool invocation is recorded.
-- session_id is nullable because some tools (notes_search, notes_read, notes_list,
-- memory_search) can be called outside of an active session.
CREATE TABLE IF NOT EXISTS agent_tool_calls (
    id          TEXT PRIMARY KEY,                -- ULID
    session_id  TEXT REFERENCES agent_sessions(id) ON DELETE CASCADE,  -- nullable for session-less calls
    tool_name   TEXT NOT NULL,
    arguments   TEXT NOT NULL DEFAULT '{}',      -- JSON-encoded arguments
    result      TEXT,                            -- JSON-encoded result (nullable)
    error       TEXT,                            -- error message (nullable)
    duration_ms INTEGER,                         -- execution time
    created_at  TEXT NOT NULL                    -- RFC3339
);
CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_session ON agent_tool_calls(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_tool ON agent_tool_calls(tool_name, created_at);
```

## Package Structure

### `internal/agent/` -- Agent Domain Package

```
internal/agent/
  types.go      -- Session, ToolCallRecord, MemoryCategory structs and constants
  store.go      -- SQLite CRUD for agent_sessions and agent_tool_calls
  service.go    -- Business logic: session lifecycle, memory operations, context gathering
  store_test.go
  service_test.go
```

**Key interfaces and types:**

```go
// Session represents an agent working session.
// Sessions form a tree via ParentSessionID (derived from "/" in the name).
type Session struct {
    ID              string
    Name            string      // hierarchical: "refactor-auth/analyze-middleware"
    ParentSessionID string      // ULID of parent, empty for root sessions
    Status          string      // "active", "completed", "archived"
    Findings        string      // compact summary (max 1500 chars), set on session_end
    Metadata        Metadata    // agent identity, config
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type Metadata struct {
    AgentName string `json:"agent_name,omitempty"` // "claude-code", "cursor", etc.
}

// Briefing is the size-budgeted context package returned by session_start.
type Briefing struct {
    Session         *Session          `json:"session"`                    // session metadata
    Plan            string            `json:"plan,omitempty"`             // existing plan (if resuming)
    LastProgress    string            `json:"last_progress,omitempty"`    // latest progress entry
    ParentPlan      string            `json:"parent_plan,omitempty"`     // parent session's plan (if child)
    SiblingFindings []SiblingFinding  `json:"sibling_findings,omitempty"` // completed siblings' findings
    Knowledge       []KnowledgeHit    `json:"knowledge,omitempty"`       // relevant knowledge matches
}

type SiblingFinding struct {
    SessionName string `json:"session_name"`
    Findings    string `json:"findings"`
}

type KnowledgeHit struct {
    Title   string  `json:"title"`
    Snippet string  `json:"snippet"`
    Source  string  `json:"source"` // "agent-memory" or project name
    Score   float64 `json:"score"`
}

const (
    MaxFindingsChars       = 1500
    DefaultMaxContextChars = 4000
)

// ToolCallRecord is an audit log entry for a single MCP tool invocation.
type ToolCallRecord struct {
    ID         string
    SessionID  string
    ToolName   string
    Arguments  string    // JSON
    Result     string    // JSON (nullable)
    Error      string    // error message (nullable)
    DurationMs int64
    CreatedAt  time.Time
}

// Store is the data access layer for agent sessions and tool calls.
// All methods take a *sql.DB parameter (the per-user database), matching the
// pattern used by note.SQLStore and other stores in the codebase. The Service
// holds the userdb.Manager and calls manager.Open(ctx, userID) to get the DB
// handle, then passes it to Store methods.
type Store interface {
    CreateSession(ctx context.Context, db DBTX, s *Session) error
    GetSession(ctx context.Context, db DBTX, id string) (*Session, error)
    GetSessionByName(ctx context.Context, db DBTX, name string) (*Session, error)
    UpdateSession(ctx context.Context, db DBTX, s *Session) error
    ListSessions(ctx context.Context, db DBTX, status string, limit, offset int) ([]*Session, error)
    ListChildSessions(ctx context.Context, db DBTX, parentID string) ([]*Session, error)
    ReconcileChildren(ctx context.Context, db DBTX, parentID, parentName string) (int64, error) // link orphan children
    LogToolCall(ctx context.Context, db DBTX, tc *ToolCallRecord) error
    ListToolCalls(ctx context.Context, db DBTX, sessionID string, limit int) ([]*ToolCallRecord, error)
}

// DBTX is satisfied by both *sql.DB and *sql.Tx, matching note.DBTX and project.DBTX.
// Note: both note and project packages define their own identical DBTX interface.
// The agent package should define its own as well (same pattern, no shared definition).
type DBTX interface {
    ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Note: search.FTSStore.Search takes *sql.DB (not DBTX). The agent service must
// pass the *sql.DB from userdb.Manager.Open() to the Searcher, not a transaction.

// NoteCreator abstracts note.Service for agent use (avoids circular imports).
// Method signatures match the actual note.Service methods exactly.
//
// Implementation note: note.Service's constructor takes concrete types
// (*note.SQLStore, *note.VersionStore, *project.Store, userdb.Manager,
// WriteSuppressor, *slog.Logger), not interfaces for the stores.
// But the agent package only needs an interface to the *Service (not the store),
// so this NoteCreator interface is satisfied by *note.Service directly.
//
// note.Service.Create does NOT call validate.Name() on the title -- it only
// checks for empty title. The handler layer calls validate.Name(). Since the
// agent service calls note.Service directly (bypassing the handler), titles
// with characters like ":" are allowed. However, note.Service calls
// project.Slugify(title) to generate the filename, which strips everything
// except [a-z0-9-]. A title like "Session Plan: refactor-auth" becomes the
// slug "session-plan-refactor-auth".
type NoteCreator interface {
    Create(ctx context.Context, userID string, req note.CreateNoteReq) (*note.Note, error)
    Update(ctx context.Context, userID, noteID string, req note.UpdateNoteReq) (*note.Note, error)
    Get(ctx context.Context, userID, noteID string) (*note.Note, error)
    List(ctx context.Context, userID string, filter note.NoteFilter) ([]*note.Note, int, error)
    Delete(ctx context.Context, userID, noteID string) error
    AppendToNote(ctx context.Context, userID, noteID, text string) (*note.Note, error)
}

// ProjectCreator abstracts project.Service for auto-creating the agent-memory project.
// project.Service.Create takes bare strings (userID, name, description), not a struct.
//
// GetBySlug exists on project.Store but NOT on project.Service. Options:
//   (a) Add GetBySlug to project.Service (small change, used by note.Service.Reindex
//       via project.Store already -- promoting it to the service is natural).
//   (b) Use List + filter in the agent service (no changes to project package, but
//       loads all projects into memory).
//   (c) Cache the agent-memory project ID in the agent.Service after first lookup.
// Recommend (a) + (c): add GetBySlug to project.Service AND cache the project ID
// in agent.Service to avoid repeated lookups.
type ProjectCreator interface {
    Create(ctx context.Context, userID, name, description string) (*project.Project, error)
    List(ctx context.Context, userID string) ([]*project.Project, error)
    GetBySlug(ctx context.Context, userID, slug string) (*project.Project, error)
}

// Searcher abstracts search across notes (FTS + semantic).
// Signatures match search.Service.SearchFTS and search.Service.SearchSemantic.
//
// SearchSemantic flow: search.SemanticSearcher.Search -> ollama.GenerateEmbedding
// -> chroma.Query -> deduplicate by note_id -> batch-load note bodies from SQLite
// -> extractSnippet around matching terms -> return SemanticResult[].
//
// IMPORTANT: SemanticSearcher.Search already over-fetches (limit*3, min 20) and
// deduplicates by note_id. The ChromaDB results include metadata (note_id, title,
// chunk_index, user_id). After adding "agent" metadata to agent note embeddings,
// scope filtering can be done application-side by checking the "agent" key in
// QueryResult.Metadata -- but this requires changes to SemanticSearcher since it
// currently discards the metadata after extracting note_id.
//
// For v1: either (a) modify SemanticSearcher.Search to accept a where filter and
// pass it through to chroma.Query, or (b) add a separate SearchSemanticFiltered
// method. Option (a) is cleaner.
type Searcher interface {
    SearchFTS(ctx context.Context, userID, query string, limit, offset int) ([]search.FTSResult, int, error)
    SearchSemantic(ctx context.Context, userID, query string, limit int) ([]search.SemanticResult, error)
}
```

**Service constructor:**

```go
type ServiceConfig struct {
    Store          Store
    NoteService    NoteCreator
    ProjectService ProjectCreator
    SearchService  Searcher
    Embedder       *ai.Embedder     // may be nil if AI/embeddings disabled
    AIQueue        *ai.Queue        // may be nil; used to enqueue embed tasks after note creation
    UserDBManager  userdb.Manager   // required: used to open per-user DBs for store calls
    Logger         *slog.Logger
}

func NewService(cfg ServiceConfig) *Service
```

> **Embedder dependency note:** The agent service needs the embedder to trigger
> re-embedding after creating/updating agent notes. The current embedding flow in
> the codebase works via the file watcher: file change -> watcher event ->
> noteSvc.Reindex -> embed task enqueued on aiQueue. HOWEVER, `note.Service.Create`
> calls `suppressor.IgnoreNext(absPath)` before writing the file, which suppresses
> the watcher event for 2 seconds (longer than the default 200ms debounce). This means
> agent notes created via `note.Service.Create` will NOT trigger the watcher's embed
> task enqueue. The agent service must enqueue embed tasks explicitly via `ai.Queue`
> (same pattern used by `cmd/seamd/main.go`'s `fileHandler` closure).
>
> The `AIQueue` field in ServiceConfig is used for this purpose. After creating or
> updating a note, the agent service enqueues an `ai.TaskTypeEmbed` task with the
> note ID. After deleting a note, it enqueues an `ai.TaskTypeDeleteEmbed` task.
> If `AIQueue` is nil (AI disabled), embedding is silently skipped. Always set
> `task.UserID` when constructing the task -- `Queue.Enqueue` calls
> `dbManager.Open(ctx, task.UserID)` and will fail if UserID is empty.
>
> Payload construction:
> ```go
> payload, _ := json.Marshal(ai.EmbedPayload{NoteID: note.ID})
> _ = s.aiQueue.Enqueue(ctx, &ai.Task{
>     UserID:   userID,
>     Type:     ai.TaskTypeEmbed,
>     Priority: ai.PriorityBackground,
>     Payload:  payload,
> })
> ```
>
> The Embedder is kept in config for future use (custom metadata for scope filtering),
> but is not strictly needed for v1 embedding (the queue + watcher's embed handler
> handles the actual embedding work).

**Service responsibilities:**
- Auto-creates the `agent-memory` project on first use: calls `ProjectCreator.GetBySlug`
  first; if not found, calls `ProjectCreator.Create`. Caches the project ID per user
  (e.g., `sync.Map` keyed by userID) to avoid repeated lookups. The `projects.slug`
  column is UNIQUE, so concurrent creation attempts would fail -- use GetBySlug-then-Create
  pattern to avoid this.
- Manages tag conventions on all created notes
- Maps session names to notes via tag-based lookup (not file paths)
- Delegates note CRUD to note.Service
- Enqueues embed tasks via ai.Queue after note creation/update/delete (since
  note.Service.Create suppresses watcher events, the watcher will not auto-embed)
- Delegates search to search.Service / ai.Embedder
- **Briefing assembly**: resolves parent sessions from "/" naming, queries sibling
  findings, runs semantic search, assembles everything within the character budget
- **Budget allocation**: distributes `max_context_chars` across briefing sections,
  redistributes unused budget from empty sections to remaining ones
- **Parent resolution**: on `session_start("a/b/c")`, looks up session "a/b" by name.
  If found, sets `parent_session_id`. If not found, `parent_session_id` stays empty.
- **Lazy parent reconciliation**: when a new session starts (e.g., `session_start("a/b")`),
  check for existing child sessions whose names start with `"a/b/"` and have NULL
  `parent_session_id`. Update those children to link to the newly created parent. This
  handles the case where subagents start before the parent agent. The reconciliation
  query: `UPDATE agent_sessions SET parent_session_id = ? WHERE name LIKE ? || '/%'
  AND parent_session_id IS NULL`. Add a `ReconcileChildren` method to the store.
- **Findings enforcement**: `session_end` validates `findings` is non-empty and
  within `MaxFindingsChars` (1500 chars)

### `internal/mcp/` -- MCP Server Package

```
internal/mcp/
  server.go       -- MCP server creation, Streamable HTTP handler, chi route mounting
  tools.go        -- All tool definitions (schema) and handler functions
  logging.go      -- Tool call logging middleware (via server.WithToolHandlerMiddleware)
  server_test.go
  tools_test.go
```

> **Removed `auth.go`:** Auth does not need a separate file. The `WithHTTPContextFunc`
> callback in `server.go` handles JWT validation and user ID injection (see below).
> The mcp-go library does not have a middleware interface that maps to chi middleware;
> instead it provides `HTTPContextFunc` at the transport level.

**Server setup:**

```go
// New creates the MCP server with all agent tools registered.
func New(cfg Config) *Server {
    mcpServer := mcpserver.NewMCPServer(
        "Seam Agent Memory",
        "1.0.0",
        mcpserver.WithToolCapabilities(false),
        mcpserver.WithRecovery(),
        // Auth check middleware: rejects tool calls if no user ID in context.
        // Must be registered BEFORE the logging middleware so auth failures
        // are not logged as tool calls. If mcp-go stacks middlewares in
        // registration order (outermost first), put auth first.
        mcpserver.WithToolHandlerMiddleware(authMiddleware),
        mcpserver.WithToolHandlerMiddleware(cfg.LoggingMiddleware),
    )
    // Register all tools via mcpServer.AddTools(...)
    return &Server{mcp: mcpServer, agent: cfg.AgentService}
}

// authMiddleware is the tool handler middleware for auth checking.
// It rejects tool calls when no user ID is present in context
// (i.e., HTTPContextFunc did not inject one due to missing/invalid JWT).
func authMiddleware(next mcp.ToolHandlerFunc) mcp.ToolHandlerFunc {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        if reqctx.UserIDFromContext(ctx) == "" {
            return mcp.NewToolResultError("unauthorized: valid JWT required"), nil
        }
        return next(ctx, req)
    }
}

// Handler returns an http.Handler for mounting at /mcp on the chi router.
// Auth is handled via WithHTTPContextFunc, which extracts the JWT from the
// Authorization header and injects user ID into the context. This runs on
// every HTTP request to the MCP endpoint (POST, GET, DELETE).
func (s *Server) Handler(jwtMgr *auth.JWTManager) http.Handler {
    return mcpserver.NewStreamableHTTPServer(s.mcp,
        mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
            // Extract Bearer token from Authorization header.
            // On invalid/missing token, inject a sentinel into context;
            // tool handlers check for it and return MCP error results.
            // (Cannot reject the HTTP request here -- HTTPContextFunc
            // only returns a context, not an error.)
            authHeader := r.Header.Get("Authorization")
            parts := strings.SplitN(authHeader, " ", 2)
            if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
                return ctx // no user ID in context; tools will reject
            }
            claims, err := jwtMgr.VerifyAccessToken(parts[1])
            if err != nil {
                return ctx
            }
            ctx = reqctx.WithUserID(ctx, claims.UserID)
            ctx = reqctx.WithUsername(ctx, claims.Username)
            return ctx
        }),
    )
}
```

> **Important:** `HTTPContextFunc` cannot reject requests (it returns `context.Context`,
> not an error). Tool handlers must check `reqctx.UserIDFromContext(ctx)` and return
> `mcp.NewToolResultError("unauthorized")` if empty. Alternatively, use
> `WithToolHandlerMiddleware` to add a global auth check that wraps every tool call.

**Authentication flow:**
1. Agent sends HTTP POST to `/api/mcp` with `Authorization: Bearer <jwt>` header
2. `HTTPContextFunc` validates JWT, injects user ID into context via `reqctx.WithUserID`
   and username via `reqctx.WithUsername` (both from `auth.Claims.UserID` / `.Username`)
3. Tool handler middleware checks `reqctx.UserIDFromContext(ctx)` -- rejects if empty
4. Tool handlers extract user ID from context and pass to agent.Service and note.Service

> **Note on `reqctx` package (internal/reqctx/reqctx.go):** `UserIDFromContext` and
> `WithUserID` / `WithUsername` / `WithRequestID` are the exported functions. There is
> NO `UsernameFromContext` function. **Add `UsernameFromContext` to `reqctx`** (Phase 4
> prerequisite -- trivial 3-line function matching the existing `UserIDFromContext`
> pattern). Until then, reading the username requires
> `ctx.Value(reqctx.UsernameKey).(string)` directly.

## MCP Tools

### Session Management

| Tool | Parameters | Returns |
|---|---|---|
| `session_start` | `name` (string, required), `max_context_chars` (int, default: 4000) | Creates a new session or resumes an existing one by name (`agent_sessions.name` is UNIQUE). If a session with the given name exists, returns its current state as a briefing. If new, creates the session, resolves parent from `/` naming, reconciles orphan children, and returns a fresh briefing. Briefing: session state + parent plan + sibling findings + relevant knowledge, all within budget. |
| `session_plan_set` | `session_name` (string, required), `content` (string, required, markdown) | Confirmation with note ID |
| `session_progress_update` | `session_name` (string, required), `task` (string, required), `status` (enum: pending\|in_progress\|completed\|blocked), `notes` (string, optional) | Updated progress note. Can use `note.Service.AppendToNote` since the timestamped bullet format (`\n- HH:MM -- text`) is appropriate for progress tracking. Format the append text as `[status] task: notes`. |
| `session_context_set` | `session_name` (string, required), `content` (string, required) | Confirmation with note ID |
| `session_end` | `session_name` (string, required), `findings` (string, required, max 1500 chars) | Final session status |
| `session_list` | `status` (enum: active\|completed\|archived\|all, default: active), `limit` (int, default: 20) | Array of session summaries with findings |

### Knowledge / Long-term Memory

| Tool | Parameters | Returns |
|---|---|---|
| `memory_read` | `category` (string, required), `name` (string, required) | Note content (title + body) |
| `memory_write` | `category` (string, required), `name` (string, required), `content` (string, required) | Confirmation with note ID |
| `memory_append` | `category` (string, required), `name` (string, required), `content` (string, required) | Updated note. **Do NOT use `note.Service.AppendToNote`** -- it prepends `\n- HH:MM -- ` (timestamped bullet format for daily notes). Instead, use `note.Service.Get` then `note.Service.Update` with the body appended. |
| `memory_list` | `category` (string, optional) | Array of {category, name, title, updated_at} |
| `memory_delete` | `category` (string, required), `name` (string, required) | Confirmation |

### Search

| Tool | Parameters | Returns |
|---|---|---|
| `memory_search` | `query` (string, required), `scope` (enum: agent\|user\|all, default: all), `limit` (int, default: 10) | Ranked results with source attribution and snippets. Uses semantic search when available (calls `Searcher.SearchSemantic`), falls back to FTS (calls `Searcher.SearchFTS`). `SearchSemantic` returns an error when semantic search is not configured (AI/ChromaDB disabled) -- the agent service must catch this and fall back to FTS. Scope filtering requires ChromaDB metadata changes (see Phase 4); without them, only `scope: all` works. |
| `notes_search` | `query` (string, required), `limit` (int, default: 10) | FTS results from user notes. Uses `search.Service.SearchFTS(ctx, userID, query, limit, 0)` which queries `notes_fts`. **Note:** FTS5 searches ALL notes in the user DB including agent-memory notes. The underlying `FTSStore.Search` does not support project or tag filtering -- it queries the FTS table directly. Post-filter by project/tag would require joining against notes/tags tables, which is not currently implemented. For v1, return unfiltered FTS results. |
| `notes_read` | `id` (string, required) | Full note content (title + body + tags). Uses `note.Service.Get`. |
| `notes_list` | `project` (string, optional -- slug or ID), `tag` (string, optional -- single tag), `limit` (int, default: 20) | Array of note summaries. Uses `note.Service.List` with `NoteFilter{ProjectID, Tag, Limit}`. `NoteFilter.Tag` is a single string (not a list). The tool should accept project slug and resolve to ID via ProjectCreator.GetBySlug. |

### User Note Creation

| Tool | Parameters | Returns |
|---|---|---|
| `notes_create` | `title` (string, required), `body` (string, required), `project` (string, optional -- slug or ID), `tags` (string[], optional) | Created note (auto-tagged `created-by:agent`). The tool must resolve project slug to ID if a slug is provided (via ProjectCreator.GetBySlug). The `created-by:agent` tag is always appended server-side. Uses `note.Service.Create` with `CreateNoteReq{Title, Body, ProjectID, Tags}`. |

### Context Gathering (Composite, Budgeted)

| Tool | Parameters | Returns |
|---|---|---|
| `context_gather` | `query` (string, required), `max_context_chars` (int, default: 3000), `scope` (enum: agent\|user\|all, default: all) | Ranked snippet results combining semantic search across the requested scope, truncated to budget. Falls back to FTS when semantic search is unavailable. Each result: title + relevance snippet (not full body). Unlike `memory_search`, results are concatenated into a single text block within the character budget, optimized for direct injection into agent context. |

## Note Conventions

### File Paths (under `agent-memory/`)

Filenames are auto-generated by `note.Service.Create` via `project.Slugify(title)`.
Example filenames (derived from titles, not directly controlled by caller):

```
session-plan-refactor-auth.md                         <- from title "Session Plan: refactor-auth"
session-progress-refactor-auth.md                     <- from title "Session Progress: refactor-auth"
session-plan-refactor-auth-analyze-middleware.md       <- from title "Session Plan: refactor-auth - analyze-middleware"
knowledge-go-middleware-patterns.md                   <- from title "Knowledge: go - middleware-patterns"
api-key-best-practices.md                             <- from title "api-key-best-practices"
```

> **Critical: `note.Service.Create` controls file paths, not the caller.**
> The current `note.Service.Create` flow is:
> 1. Generates slug via `project.Slugify(title)` (strips non-alphanumeric except hyphens)
> 2. Calls `uniqueFilename(notesDir, projectSlug, slug)` to avoid collisions
> 3. Builds `relPath = projectSlug + "/" + filename` (for notes in a project)
>
> The caller does NOT control the file path -- it's derived from the title. This means
> the "sessions/" subdirectory in the path above is NOT achievable unless we either:
>   (a) Modify `note.Service.Create` to accept an explicit relative path (breaking change), or
>   (b) Use the title to influence the slug (e.g., title = "sessions--refactor-auth--plan"
>       would produce slug "sessions--refactor-auth--plan.md" in the agent-memory/ dir), or
>   (c) Use tags to organize instead of subdirectories (simpler, no path manipulation needed).
>
> **Recommended approach (c):** Drop the `sessions/` and `knowledge/` subdirectories.
> All agent notes live flat under `agent-memory/`. Use tags (`type:plan`, `type:progress`,
> `type:knowledge`, `session:{name}`, `domain:{category}`) for organization and filtering.
> The title carries the semantic meaning (e.g., "Session Plan: refactor-auth"), and
> `project.Slugify` produces a clean filename (e.g., "session-plan-refactor-auth.md").
>
 > **Path safety for hierarchical session names:** Session names like
 > `refactor-auth/analyze-middleware` contain `/` characters. The agent service must
 > flatten these for note titles by replacing `/` with ` - ` (space-hyphen-space),
 > e.g., title becomes "Session Plan: refactor-auth - analyze-middleware". This is
 > because `project.Slugify` strips ALL non-`[a-z0-9-]` characters (including `/`)
 > WITHOUT replacing them, so `auth/analyze` would become `authanalyze` (merged
 > with no separator). Using ` - ` as the separator means Slugify produces
 > `session-plan-refactor-auth-analyze-middleware` (clean, readable, with proper
 > word boundaries). The session name stays hierarchical in the `agent_sessions`
 > table and in the `session:{name}` tag; only the note title/slug is flattened.
 > **Do NOT use `--` as separator** -- `Slugify` collapses `--` to `-`, making it
 > impossible to distinguish from natural hyphens.
>
> **Session name validation:** Allow only `[a-zA-Z0-9_-/]`, reject `..`, reject
> leading/trailing `/`, reject consecutive `/`.

### Frontmatter Example

```yaml
---
id: 01JQXYZ...
title: "Session Plan: refactor-auth"
project: agent-memory
tags:
  - session:refactor-auth
  - type:plan
  - status:active
  - created-by:agent
created: 2026-03-13T10:00:00Z
modified: 2026-03-13T10:30:00Z
---

## Goals
- Refactor the auth middleware to support API keys
- Add rate limiting per API key

## Tasks
1. [ ] Analyze current auth middleware
2. [ ] Design API key schema
3. [ ] Implement API key store
4. [ ] Update auth middleware
5. [ ] Add rate limiting
6. [ ] Write tests
```

### Embedding Metadata

Agent notes are embedded into the same per-user ChromaDB collection as regular notes,
with additional metadata for filtered search:

```json
{
  "note_id": "01JQXYZ...",
  "title": "Session Plan: refactor-auth",
  "chunk_index": 0,
  "user_id": "01JQABC...",
  "agent": "true",
  "memory_type": "plan",
  "session_name": "refactor-auth"
}
```

Search tools use metadata filters:
- `scope: agent` -> filter `agent = "true"`
- `scope: user` -> filter `agent != "true"`
- `scope: all` -> no filter

> **Prerequisite:** `ai.ChromaClient.Query` currently sends no `where` clause.
> A `QueryWithFilter` method (or options pattern) must be added to support these
> filters. See Phase 4 implementation tasks. For v1, application-side filtering
> after over-fetching is acceptable but less efficient.

## Server Wiring

In `cmd/seamd/main.go`, after existing component initialization:

```go
// Agent memory components.
// agentStore needs no constructor args -- it is stateless like note.SQLStore.
// The service holds userdb.Manager and passes the per-user *sql.DB to store methods.
agentStore := agent.NewSQLStore()
agentSvc := agent.NewService(agent.ServiceConfig{
    Store:          agentStore,
    NoteService:    noteSvc,       // *note.Service (matches existing var name in main.go)
    ProjectService: projectSvc,    // *project.Service (matches existing var name)
    SearchService:  searchSvc,     // *search.Service (matches existing var name)
    Embedder:       embedder,      // *ai.Embedder (concrete), may be nil if AI disabled
    AIQueue:        aiQueue,       // *ai.Queue, may be nil if AI disabled
    UserDBManager:  userDBMgr,     // userdb.Manager (matches existing var name)
    Logger:         logger,
})

// MCP server
mcpServer := mcp.New(mcp.Config{
    AgentService: agentSvc,
    Logger:       logger,
})
```

> **Mounting approach -- two options (choose one during implementation):**
>
> **Option A: Add `MCPHandler` to `server.Config`** and mount inside `server.New()`,
> consistent with how all other routes are mounted:
> ```go
> // In server.go, inside New(), AFTER the protected r.Group closing brace (line 173)
> // and BEFORE the SPA fallback (line 176). MCP does its own auth via HTTPContextFunc,
> // so it must NOT be inside the protected r.Group (which applies AuthMiddleware).
> if cfg.MCPHandler != nil {
>     r.Handle("/api/mcp", cfg.MCPHandler)
> }
> ```
> In `main.go`: `MCPHandler: mcpServer.Handler(jwtMgr)`.
> **`r.Handle`** (not `r.Mount`) is correct here because `StreamableHTTPServer.ServeHTTP`
> handles POST/GET/DELETE internally on the same path. Using `r.Mount` would strip the
> matched prefix from the request URL, which could break mcp-go's internal routing.
>
> **Option B: Mount after `server.New()` via `srv.Router()`** (the router is exposed):
> ```go
> srv := server.New(serverCfg)
> if mcpServer != nil {
>     srv.Router().Handle("/api/mcp", mcpServer.Handler(jwtMgr))
> }
> ```
>
> Option A is cleaner and consistent with the existing pattern where all routes are
> inside `server.New()`. Option B avoids modifying `server.Config` but breaks the
> convention. **Recommend Option A.**
>
> **Auth note:** Do NOT use `server.AuthMiddleware(jwtMgr)` as chi middleware on the
> MCP route. The mcp-go `StreamableHTTPServer` handles POST/GET/DELETE internally and
> must be the direct handler. Auth is done via `WithHTTPContextFunc` inside
> `mcpServer.Handler(jwtMgr)` (see MCP Server Package section above).
>
> **CORS note:** The MCP endpoint at `/mcp` is NOT under `/api/`, so the SPA static
> file fallback route (`r.Get("/*", ...)`) would intercept GET requests to `/mcp`.
> Either: (a) mount at `/api/mcp` instead, or (b) register the MCP handler before
> the SPA fallback. **Recommend `/api/mcp`** for consistency with all other endpoints.
>
> **Variable names:** The wiring code above uses the actual variable names from
> `cmd/seamd/main.go`: `noteSvc`, `projectSvc`, `searchSvc`, `userDBMgr`, `embedder`,
> `jwtMgr`, `logger`. Previous version of this plan used incorrect names like
> `noteService`, `projectService`, `userDBManager`.

## Implementation Phases

### Phase 1: Database + Agent Domain Package + Prerequisite Changes
- [ ] Migration `002_agent_sessions.sql` (with parent_session_id, findings columns, nullable session_id on tool_calls)
- [ ] Register migration in `migrations/migrations.go` (`//go:embed user/002_agent_sessions.sql` as `UserSQL002`, append `{Version: 2, SQL: UserSQL002}` to `UserMigrations()`)
- [ ] Update `internal/testutil/testutil.go`: change `TestUserDB` to loop through `migrations.UserMigrations()` and execute each SQL, instead of passing only `migrations.UserSQL`
- [ ] **Add `GetBySlug` to `project.Service`** (moved from Phase 4 -- needed by Phase 3 tools). Add `GetBySlug(ctx, userID, slug string) (*Project, error)` that opens the user DB and delegates to `project.Store.GetBySlug`. Then update the `ProjectCreator` interface.
- [ ] **Add `UsernameFromContext` to `internal/reqctx/reqctx.go`** (moved from Phase 4 -- trivial, needed by Phase 2 auth).
- [ ] `internal/agent/types.go` -- Session, Briefing, ToolCallRecord, KnowledgeHit, SiblingFinding, DBTX, constants, sentinel errors (ErrNotFound, ErrSessionActive, ErrFindingsTooLong, ErrFindingsRequired, ErrInvalidSessionName)
- [ ] `internal/agent/store.go` -- SQLite CRUD (sessions, tool calls, child/sibling queries; all methods take DBTX). Include `ListSiblingSessionsByParent` for briefing assembly. Include `ReconcileChildren(ctx, db, parentID, parentName)` for lazy parent-child linking when parent starts after children.
- [ ] `internal/agent/service.go` -- session lifecycle, parent resolution, memory CRUD via note.Service (holds userdb.Manager). Note-to-session mapping via tags (not file paths). Cache agent-memory project ID per user. Enqueue embed tasks via ai.Queue after note create/update/delete (watcher is suppressed by note.Service).
- [ ] `internal/agent/briefing.go` -- briefing assembly: budget allocation, section truncation, knowledge retrieval. Graceful degradation when SearchSemantic is nil (AI disabled).
- [ ] Unit tests for store, service, and briefing

### Phase 2: MCP Server Package
- [ ] Add `github.com/mark3labs/mcp-go` dependency (`go get github.com/mark3labs/mcp-go@v0.45.0`)
- [ ] `internal/mcp/server.go` -- MCP server creation with `mcpserver.NewMCPServer()`, `StreamableHTTPServer` with `WithHTTPContextFunc` for JWT auth (extracts user ID via `reqctx.WithUserID`). Auth check via `WithToolHandlerMiddleware` that rejects tools if no user ID in context.
- [ ] `internal/mcp/tools.go` -- all tool definitions (using `mcp.NewTool` + `mcp.WithString`/`mcp.WithNumber`/etc.) and handler functions (signature: `func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)`). Use `request.RequireString(key)` for required args (returns error if missing) and `request.GetInt(key, defaultValue)` for optional args with defaults. **NOTE:** verify `request.GetStringSlice(key, defaultValue)` exists in mcp-go v0.45.0 before using it for the `tags` parameter of `notes_create`; if absent, accept tags as a JSON array string and parse manually (see audit note #19).
- [ ] `internal/mcp/logging.go` -- tool call audit logging via `mcpserver.WithToolHandlerMiddleware` (wraps every tool call, records to agent_tool_calls table)

### Phase 3: Tool Implementation
- [ ] Session tools: session_start (with briefing assembly), session_plan_set, session_progress_update, session_context_set, session_end (with findings validation), session_list
- [ ] Knowledge tools: memory_read, memory_write, memory_append, memory_list, memory_delete
- [ ] Search tools: memory_search, notes_search, notes_read, notes_list
- [ ] User note creation: notes_create
- [ ] Context gathering: context_gather (budgeted semantic search with snippet extraction)

### Phase 4: Server Wiring + Deferred Changes

> **Ordering note:** `GetBySlug` and `UsernameFromContext` have been moved to Phase 1
> since they are needed by Phase 2/3. Embedder metadata and ChromaDB filtering are
> deferred (only needed for `scope: agent|user` filtering, not `scope: all`).

- [ ] Wire agent store, service, and MCP server in `cmd/seamd/main.go` (use actual var names: `noteSvc`, `projectSvc`, `searchSvc`, `userDBMgr`, `embedder`, `jwtMgr`, `logger`, `aiQueue`)
- [ ] Mount `/api/mcp` endpoint (NOT `/mcp` -- must be under `/api/` to avoid conflict with the SPA fallback route at `/*` which catches all non-`/api/` GET requests). Add `MCPHandler http.Handler` field to `server.Config` and mount conditionally inside `server.New()`, AFTER the protected `r.Group` closing brace and BEFORE the SPA fallback.
- [ ] **Add `CORS: Mcp-Session-Id` to AllowedHeaders and ExposedHeaders**: The mcp-go
      StreamableHTTP transport uses an `Mcp-Session-Id` header for session tracking. Add it
      to the CORS `AllowedHeaders` list in `server.New()` so browser-based MCP clients can
      send it, and to `ExposedHeaders` so clients can read it from responses. Current
      ExposedHeaders are `["X-Request-ID", "X-Total-Count"]`.
- [ ] Update `internal/config/config.go` if needed (MCP enable/disable flag, default budgets)
- [ ] **Optional: WebSocket events for agent note changes**: `note.Service.Create`
      suppresses watcher events, so the web UI will not see real-time updates when
      agents create/modify notes. If real-time UI updates are desired, add `ws.Hub`
      to `agent.ServiceConfig` and send `note.changed` events after agent note
      CRUD operations. The `ws.Hub.Send(userID, msg)` method exists (hub.go:90).
      This is optional for v1 but should be planned for v2.

#### Deferred to v2 (scope filtering):
- [ ] **Modify `ai.Embedder.EmbedNote`** to accept optional extra metadata (`map[string]string`):
      current signature is `EmbedNote(ctx, userID, noteID, title, body string) error`
      (internal/ai/embedder.go:87) and hardcodes metadata to exactly 4 keys
      `{note_id, title, chunk_index, user_id}` (embedder.go:116-121). Agent notes need
      `"agent": "true"`, `"memory_type"`, `"session_name"` for scope filtering. Add an
      `opts ...EmbedOption` parameter or a `metadata map[string]string` parameter. Existing
      callers (`embedder.HandleEmbedTask` at embedder.go:155) must not break -- use variadic
      opts so zero args preserves current behavior.
- [ ] **Add metadata filtering to `ai.ChromaClient.Query`**: Add `Where` field to
      `chromaQueryRequest`, add `QueryWithFilter` method, update `SemanticSearcher.Search`
      to pass through where filters. Update `bestResult` struct to preserve `agent` metadata.

### Phase 5: Testing
- [ ] Unit tests: agent store (in-memory SQLite, parent/child relationships)
- [ ] Unit tests: briefing assembly (budget allocation, section truncation, redistribution)
- [ ] Unit tests: agent service (mocked dependencies)
- [ ] Unit tests: MCP tool handlers (mocked agent service)
- [ ] Integration tests: full session lifecycle (start -> plan -> progress -> end)
- [ ] Integration tests: hierarchical sessions (parent -> child -> sibling findings flow)
- [ ] Integration tests: budgeted search across agent + user notes
- [ ] Manual testing: Claude Code as MCP client

### Phase 6: Polish
- [ ] Error handling and edge cases (duplicate sessions, orphan children, missing notes)
- [ ] Rate limiting for MCP endpoint
- [ ] Logging (structured slog entries for all MCP operations)
- [ ] Documentation in README.md

## Estimated Effort

| Phase | Duration |
|---|---|
| Phase 1: DB + Agent package + Briefing + prereqs (GetBySlug, UsernameFromContext) | ~2 days |
| Phase 2: MCP server package | ~1 day |
| Phase 4: Server wiring, CORS, mount endpoint | ~0.5 days |
| Phase 3: Tool implementation | ~2 days |
| Phase 5: Testing | ~2 days |
| Phase 6: Polish | ~1 day |
| **Total** | **~8.5 days** |

## Audit Notes (Gaps and Risks)

Issues identified during plan review against the actual codebase:

### Fixed in This Plan

1. **Auth approach corrected:** mcp-go does not use chi-style middleware. Auth is via
   `WithHTTPContextFunc` (transport-level context injection) + `WithToolHandlerMiddleware`
   (tool-level auth check). The `auth.go` file was removed from the package structure.

2. **Variable names corrected:** Server wiring code now uses actual `cmd/seamd/main.go`
   variable names (`noteSvc`, `projectSvc`, `searchSvc`, `userDBMgr`, `jwtMgr`) instead
   of incorrect ones (`noteService`, `projectService`, `userDBManager`).

3. **File path convention revised:** `note.Service.Create` controls file paths (derived
   from title via `project.Slugify`). The caller cannot specify subdirectories like
   `sessions/` or `knowledge/`. Changed to flat structure under `agent-memory/` with
   tag-based organization instead.

4. **testutil migration gap fixed:** `TestUserDB` only runs `migrations.UserSQL` (001).
   Added explicit instructions to update it for 002.

5. **MCP endpoint path:** Changed from `/mcp` to `/api/mcp` to avoid conflict with the
   SPA fallback route (`/*`) that catches all non-`/api/` GET requests.

6. **CORS header:** Added `Mcp-Session-Id` to `AllowedHeaders` for mcp-go session tracking.

7. **tool_calls session_id nullable:** Made nullable since search/read tools can be called
   outside an active session.

### Remaining Risks to Track During Implementation

1. **mcp-go is pre-v1 (v0.45.0):** API may change. Pin the exact version in go.mod and
   test thoroughly. The library is actively developed and has had breaking changes.

2. **`HTTPContextFunc` cannot reject requests:** It returns `context.Context`, not an error.
   Unauthenticated requests reach the MCP server; tool handlers must check for user ID.
   A `WithToolHandlerMiddleware` that returns `mcp.NewToolResultError("unauthorized")` for
   missing user IDs is the workaround.

3. **StreamableHTTP handles all methods on one path:** `StreamableHTTPServer.ServeHTTP`
   handles POST (RPC), GET (SSE stream), and DELETE (session close) on the same path.
   Using `r.Handle("/api/mcp", handler)` works with chi, but `r.Mount` may not route
   correctly for all methods. Test with both `r.Handle` and `r.Mount`.

4. **Semantic search scope filtering requires upstream changes:** `search.SemanticSearcher`
   and `ai.ChromaClient.Query` need modifications for metadata-based filtering. These are
   moderate changes touching two existing packages. Until done, `scope: agent|user` on
   `memory_search` and `context_gather` will not work (only `scope: all`). Consider
   shipping v1 without scope filtering and adding it as a fast follow.

5. **Embedding requires explicit enqueue (watcher is suppressed):**
    `note.Service.Create` calls `suppressor.IgnoreNext(absPath)` which suppresses the
    watcher event for 2 seconds. This means agent notes are NOT automatically embedded
    by the watcher. The agent service must explicitly enqueue embed tasks via `ai.Queue`
    after creating/updating/deleting notes. The `ai.Embedder.EmbedNote` method
    (internal/ai/embedder.go:116-121) hardcodes metadata to exactly 4 keys:
    `note_id`, `title`, `chunk_index`, `user_id`. Adding agent-specific metadata
    (`agent:true`, `memory_type`, `session_name`) requires extending the Embedder API
    (Phase 4 task). For v1, use the standard metadata (no scope filtering).
    Scope filtering is a v2 concern. Remember to set `task.UserID` when enqueuing
    (see audit note #18).

6. **`project.Slugify` strips non-alphanumeric characters and collapses `--` to `-`:**
    The regex `[^a-z0-9-]` removes ALL characters except lowercase alphanumeric and
    hyphens (including `/`, `:`, spaces), and `-{2,}` collapses consecutive hyphens.
    This has two implications:
    (a) `/` in hierarchical session names is stripped entirely (NOT replaced), so
        `auth/analyze` becomes `authanalyze` (words merged with no separator). The
        agent service must replace `/` with ` - ` (space-hyphen-space) in note titles
        before passing to `note.Service.Create`, producing clean slugs with proper
        word boundaries.
    (b) `--` cannot be used as a separator because Slugify collapses it to `-`.
    (c) Filenames are not reversible back to session names. Use tags
        (`session:{name}`) for session-to-note mapping, not filenames. The filename
        is just a human-readable slug.
    
    **Resolution:** Use tags for all lookups. The agent service calls
    `note.Service.List` with a tag filter like `session:{name}` to find session notes,
    and identifies note types by title prefix ("Session Plan:", "Session Progress:", etc.).
    Tags preserve the exact hierarchical session name (e.g., tag `session:refactor-auth/analyze`).

7. **File watcher interaction:** Agent notes written via `note.Service.Create` call
    `suppressor.IgnoreNext(absPath)` which suppresses the watcher event. The watcher will
    NOT fire for service-initiated writes, so no duplicate reindex or automatic embedding
    occurs. The agent service must handle embedding explicitly (see audit item #14).
    However, if the user edits an agent note file externally (e.g., in a text editor),
    the watcher WILL fire and reindex + embed that note. WebSocket `note.changed` events
    will NOT be sent for service-initiated agent note writes (since the watcher is
    suppressed). If the UI should reflect agent note changes in real-time, the agent
    service should send WS events explicitly via `ws.Hub.Send`.

8. **note.Service takes concrete store types:** `note.NewService` takes `*note.SQLStore`,
    `*note.VersionStore`, `*project.Store` -- not interfaces. The agent package does not
    construct a note.Service (it receives one), so this is not a problem for the agent
    package, but it means the NoteCreator interface is satisfied by `*note.Service` directly.

### Second Audit (Codebase-Verified)

Issues found during a second thorough audit of the plan against actual source code:

#### Issues Fixed in Plan

9. **`note.DBTX` includes `PrepareContext`:** The plan's `DBTX` interface (line ~363) omits
    `PrepareContext`. The actual `note.DBTX` (store.go:53-58) includes it:
    ```go
    PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
    ```
    However, `project.DBTX` does NOT include `PrepareContext`. The agent package should
    define its own `DBTX` matching whichever methods it actually needs. Since agent store
    operations are simple CRUD (no batch prepared statements), the 3-method version
    (without `PrepareContext`) is sufficient. **No change needed** -- the plan's DBTX is
    intentionally minimal and correct for agent use.

10. **`NoteFilter.Tag` is single-valued:** The plan mentions filtering by
    `session:{name}` + `type:plan` to find specific session notes, but `NoteFilter.Tag`
    accepts only a single tag string (not a list). The agent service must filter by one
    tag (e.g., `session:{name}`) and then match the title prefix (e.g., starts with
    "Session Plan:") in application code. **This works because titles follow a
    deterministic pattern** ("Session Plan: {name}", "Session Progress: {name}", etc.).
    Additionally, `ProjectID` can be combined with `Tag` in the filter (they are ANDed
    in the WHERE clause), which narrows results to the agent-memory project.

11. **`note.Service.List` DOES populate `Note.Tags`:** The `List` method calls
     `SQLStore.List` which, after scanning rows via `scanNoteRow`, calls
     `s.loadAllTags(ctx, db, notes)` (store.go:221-223) to batch-load tags for all
     returned notes. **Tags are populated on `List` results.** This means the agent
     service CAN inspect tags on listed notes if needed, though filtering by title
     prefix remains the primary identification strategy for session notes.

12. **`Mcp-Session-Id` also needs `ExposedHeaders`:** The plan mentions adding
    `Mcp-Session-Id` to `AllowedHeaders` (for browser-based MCP clients to send it),
    but it also needs to be in `ExposedHeaders` so the client can read it from responses.
    Current `ExposedHeaders` are `["X-Request-ID", "X-Total-Count"]`.

13. **`note.NewService` takes 6 parameters, not 5:** The plan's `NoteCreator` interface
    comment says the constructor takes `(*note.SQLStore, *note.VersionStore, *project.Store)`.
    The actual signature is:
    ```go
    func NewService(store *SQLStore, versionStore *VersionStore, projectStore *project.Store,
        userDBManager userdb.Manager, suppressor WriteSuppressor, logger *slog.Logger) *Service
    ```
    This does not affect the agent package (which receives `*note.Service`, not constructing
    it), but the comment in the plan should be corrected for accuracy.

14. **Watcher suppression prevents automatic embedding for service-initiated writes:**
    The file watcher's handler in `cmd/seamd/main.go:363-386` enqueues embed tasks
    after reindexing -- but only for watcher-triggered events (external file edits).
    When `note.Service.Create` writes a file, it first calls
    `suppressor.IgnoreNext(absPath)` to suppress the watcher event for 2 seconds
    (longer than the default 200ms debounce). This means agent notes created via
    `note.Service.Create` will NOT trigger the watcher's embed task enqueue.
    
    The agent service must enqueue embed tasks explicitly via `ai.Queue` after
    creating/updating/deleting notes (same pattern used by `cmd/seamd/main.go`'s
    `fileHandler` closure). The `AIQueue` field has been added to `ServiceConfig`
    for this purpose. If `AIQueue` is nil (AI disabled), embedding is silently
    skipped. The standard 4-key metadata (`note_id`, `title`, `chunk_index`,
    `user_id`) is used -- scope filtering (`scope: agent`) will not work until
    the Embedder API is extended to accept custom metadata.

16. **`search.Service.SearchSemantic` returns error when semantic is nil:** If AI/ChromaDB
    is not configured, `SearchSemantic` returns an error with message
    `"search.Service.SearchSemantic: semantic search not configured"` (verified in
    `internal/search/service.go:55`). The `Searcher` interface and agent service must
    handle this gracefully -- fall back to FTS-only search when semantic search is
    unavailable. The briefing assembly must also degrade gracefully (skip the knowledge
    section or use FTS instead). Pattern for fallback:
    ```go
    results, err := s.search.SearchSemantic(ctx, userID, query, limit)
    if err != nil {
        // Semantic unavailable -- fall back to FTS.
        ftsResults, _, ftsErr := s.search.SearchFTS(ctx, userID, query, limit, 0)
        if ftsErr != nil { return nil, ftsErr }
        // convert ftsResults to KnowledgeHit/snippet format
    }
    ```

17. **`project.Service.List` has no pagination:** `List(ctx, userID) ([]*Project, error)`
    returns all projects with no limit/offset. This is fine for the agent service's use
    case (looking up the `agent-memory` project), but the plan should cache the project
    ID per user to avoid repeated list calls.

18. **`ai.Queue.Enqueue` requires `task.UserID` to be set:** `Queue.Enqueue`
    (internal/ai/queue.go:107) calls `q.dbManager.Open(ctx, task.UserID)` to persist the
    task. If `UserID` is empty, `Open` will fail. The agent service must always set
    `task.UserID` when constructing embed/delete-embed tasks. Pattern:
    ```go
    payload, _ := json.Marshal(ai.EmbedPayload{NoteID: note.ID})
    _ = s.aiQueue.Enqueue(ctx, &ai.Task{
        UserID:   userID,           // required -- Open fails if empty
        Type:     ai.TaskTypeEmbed,
        Priority: ai.PriorityBackground,
        Payload:  payload,
    })
    ```
    `Queue.Enqueue` auto-generates the task ID and sets `CreatedAt` and `Status` if zero.

19. **`mcp-go` `request.GetStringSlice` -- verify existence before use:** The plan
    references `request.GetStringSlice(key, defaultValue)` for string arrays (e.g., the
    `tags` parameter of `notes_create`). Confirm this method exists in `mcp-go` v0.45.0
    before using it. If absent, use `request.GetString` with comma-separated parsing or
    handle tags as a JSON array string. This must be resolved during Phase 2 (dependency
    addition) before writing tool handlers in Phase 3.

20. **`NoteCreator.AppendToNote` interface inclusion rationale:** The `NoteCreator`
    interface includes `AppendToNote` solely for `session_progress_update`, which uses the
    timestamped bullet format (`\n- HH:MM -- text`) intentionally for progress logs.
    The `memory_append` tool explicitly must NOT use `AppendToNote` (uses Get + Update
    instead). This asymmetry is intentional and documented in the MCP Tools table, but
    worth flagging here so the interface contract is not accidentally "simplified" by
    removing `AppendToNote`.

21. **`WithToolHandlerMiddleware` stacking order in mcp-go:** The plan registers two
    middlewares: an auth check middleware and a logging middleware. Verify in mcp-go
    v0.45.0 whether multiple calls to `WithToolHandlerMiddleware` stack (i.e., last
    registered = outermost wrapper) or replace. If they stack, the auth check should be
    registered last so it runs outermost (before logging). If they replace, combine auth
    and logging into a single middleware function. Check mcp-go source or docs during
    Phase 2.

#### Phase Ordering (Revised)

Prerequisite changes (`GetBySlug`, `UsernameFromContext`) have been moved into Phase 1.
Embedder/ChromaDB changes deferred to v2 (scope filtering).

Recommended implementation order:
1. Phase 1 (DB + agent package + `GetBySlug` + `UsernameFromContext`)
2. Phase 2 (MCP server package)
3. Phase 4 (server wiring, CORS, mount endpoint) -- do this BEFORE Phase 3 so
   manual testing is possible during tool implementation
4. Phase 3 (tool implementation) -- can test against the running MCP endpoint
5. Phase 5 (testing)
6. Phase 6 (polish)
7. v2: Embedder/ChromaDB metadata changes (scope filtering)
