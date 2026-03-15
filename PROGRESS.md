# MCP Agent Memory System - Implementation Progress

## Phase 1: Data Layer (Store + Types)
**Status: COMPLETE**

- `internal/agent/types.go` - Session, Briefing, ToolCallRecord, MemoryItem, KnowledgeHit, SiblingFinding structs; Store/DBTX interfaces; validation functions; constants
- `internal/agent/store.go` - SQLStore with full CRUD: CreateSession, GetSession, GetSessionByName, UpdateSession, ListSessions, ListChildSessions, ReconcileChildren, LogToolCall, ListToolCalls
- `migrations/user/002_agent_sessions.sql` - agent_sessions and agent_tool_calls tables
- `migrations/migrations.go` - UserSQL002 embedded, UserMigrations() updated
- `internal/testutil/testutil.go` - TestUserDB runs all user migrations
- All 23 store tests pass, all type tests pass

## Phase 2: Service Layer
**Status: COMPLETE**

- `internal/agent/service.go` - Full service implementation:
  - Session lifecycle: SessionStart (with parent resolution + orphan reconciliation), SessionEnd, SessionList
  - Session notes: SessionPlanSet, SessionProgressUpdate, SessionContextSet
  - Memory CRUD: MemoryWrite, MemoryRead, MemoryAppend, MemoryList, MemoryDelete
  - ContextGather: budgeted search with semantic-to-FTS fallback and truncation
  - LogToolCall: persists tool call audit records to user DB
  - Agent-memory project auto-creation with per-user caching
  - Knowledge search with semantic-to-FTS fallback
- `internal/agent/briefing.go` - Budget allocation with proportional redistribution, word-boundary truncation
- All 25+ service tests pass, briefing tests pass

## Phase 3: MCP Server + Tools
**Status: COMPLETE**

- `internal/mcp/server.go` - MCP server with auth middleware (WithHTTPContextFunc for JWT), rate limiting middleware, logging middleware, tool handler middleware; AgentService interface includes ContextGather
- `internal/mcp/tools.go` - 12 tools registered and fully implemented with input validation and sanitized error handling:
  - Session tools: session_start, session_plan_set, session_progress_update, session_context_set, session_end, session_list
  - Memory tools: memory_read, memory_write, memory_append, memory_list, memory_delete
  - Context: context_gather (uses service's searchKnowledge with budget truncation)
- `internal/mcp/logging.go` - Logging middleware: structured slog entries + DB persistence via ToolCallLogger
- `internal/server/server.go` - MCPHandler field in Config, mounted at /api/mcp, CORS includes Mcp-Session-Id
- All MCP server and tool tests pass

## Phase 4: Wiring (main.go)
**Status: COMPLETE**

- `cmd/seamd/main.go` - Agent store, service, and MCP server created and wired:
  - `agent.NewSQLStore()` for data layer
  - `agent.NewService()` with NoteService, ProjectService, SearchService, UserDBManager
  - `mcp.New()` with AgentService and ToolCallLogger (agentSvc implements both)
  - `mcpSrv.Handler(jwtMgr)` passed as MCPHandler to server.Config
- Full build succeeds, all tests pass

## Phase 5: Testing
**Status: COMPLETE**

- `internal/integration/agent_e2e_test.go` - 9 integration tests (build tag: integration):
  - `TestE2E_AgentSessionLifecycle` - Full lifecycle: start -> plan -> progress -> context -> resume -> end -> verify completed
  - `TestE2E_HierarchicalSessions` - Parent -> child-a (complete) -> child-b (sees sibling findings)
  - `TestE2E_OrphanChildReconciliation` - Child starts before parent, gets reconciled when parent starts
  - `TestE2E_MemoryCRUDLifecycle` - Write -> read -> upsert -> append -> list by category -> delete -> verify
  - `TestE2E_ContextGather_WithRealFTS` - FTS search across agent knowledge notes
  - `TestE2E_ContextGather_BudgetTruncation` - Budget enforcement with many large notes
  - `TestE2E_MixedSessionAndMemory` - Sessions and knowledge working together
  - `TestE2E_UserIsolation` - Two users cannot see each other's data
  - `TestE2E_MCPEndpoint_Accessible` - MCP endpoint reachable via HTTP
- `TestSQLStore_ReconcileChildren_SkipsGrandchildren` - Verifies direct-child-only reconciliation
- All integration tests pass

## Phase 6: Polish
**Status: COMPLETE**

### Error handling
- MCP tool handlers no longer leak internal error details (file paths, DB errors)
- Domain errors mapped to user-safe messages via `sanitizeError()`:
  - `ErrNotFound` -> "not found"
  - `ErrSessionNotActive` -> "session is not active"
  - `ErrFindingsTooLong` -> "findings exceed maximum length (1500 chars)"
  - `ErrFindingsRequired` -> "findings are required"
  - `ErrInvalidSessionName` -> "invalid session name"
  - All others -> "internal error"

### Input validation
- Category: max 100 chars, no control characters
- Name: max 200 chars, no control characters
- Content: max 512 KB
- Query: max 10 KB
- Session list limit: capped at 100

### Rate limiting
- Per-user rate limiting on MCP tool calls: 60 req/min, burst 20
- Implemented as `WithToolHandlerMiddleware` (runs before logging)
- Stale limiter eviction every 5 minutes (10-minute TTL)

### Tool call audit logging
- `ToolCallLogger` interface wired to `agent.Service.LogToolCall`
- Every tool call persisted to `agent_tool_calls` table with: tool name, arguments, result (truncated), error, duration, timestamp
- `cmd/seamd/main.go` passes `agentSvc` as both `AgentService` and `ToolCallLogger`

### ReconcileChildren fix
- Fixed grandchild reconciliation: `ReconcileChildren` now uses `LIKE parent/% AND NOT LIKE parent/%/%` to match only direct children
- New test: `TestSQLStore_ReconcileChildren_SkipsGrandchildren` verifies the fix

### Cleanup
- Removed unused `NoteReader` interface from `mcp/server.go`
- Added MCP documentation section to `README.md`

## Test Summary
- **170+ tests** across all packages
- 9 integration tests (agent e2e with real filesystem and SQLite)
- All tests passing

## V1 Deferred Items (resolved in V2)
- ~~`AIQueue`/embed task enqueuing~~ -- Resolved: agent notes embed with `scope: "agent"` metadata
- ~~Embedder metadata for scope filtering~~ -- Resolved: `EmbedPayload.Scope` -> ChromaDB metadata
- ~~ChromaDB `where` filter support~~ -- Resolved: `QueryWithFilter` method added
- ~~notes_search, notes_read, notes_list, notes_create MCP tools~~ -- Already implemented in V1 (service + handlers complete)
- ~~WebSocket events for agent note changes~~ -- Resolved: `WSNotifier` interface + `HubWSNotifier` adapter

## Phase 7: MCP V2
**Status: COMPLETE**

### Scope-Filtered Search
- `internal/ai/task.go` - `EmbedPayload.Scope` field ("agent" or "user")
- `internal/ai/embedder.go` - `EmbedNote` accepts optional `extraMeta` for scope metadata; `HandleEmbedTask` passes scope from payload; default scope "user" on all embeddings
- `internal/ai/chroma.go` - `QueryWithFilter` method: ChromaDB query with `where` metadata filter
- `internal/search/fts.go` - `SearchScoped`: FTS with include/exclude project filter
- `internal/search/semantic.go` - `SearchScoped`: semantic search with ChromaDB `where` clause
- `internal/search/service.go` - `SearchFTSScoped`, `SearchSemanticScoped` methods
- `internal/agent/service.go` - `searchKnowledgeScoped` with scope-based filter resolution; `ContextGather` now accepts `scope` parameter; `enqueueEmbed` uses "agent" scope, `enqueueEmbedWithScope` for explicit scope
- `internal/mcp/tools.go` - `context_gather` handler wires `scope` param (was previously ignored)

### memory_search Tool
- `internal/agent/service.go` - `MemorySearch` method: searches agent knowledge only via scoped FTS/semantic
- `internal/mcp/tools.go` - `memory_search` tool definition + handler
- `internal/mcp/server.go` - `AgentService` interface updated

### WebSocket Events for Agent Changes
- `internal/agent/types.go` - `WSNotifier` interface defined
- `internal/agent/wsnotifier.go` - `HubWSNotifier` adapter: bridges `ws.Hub` to `WSNotifier`
- `internal/agent/service.go` - Events emitted on:
  - `agent.session_started` (SessionStart)
  - `agent.session_ended` (SessionEnd)
  - `agent.memory_changed` (MemoryWrite create/update, MemoryDelete)
  - `agent.note_created` (NotesCreate)
- `cmd/seamd/main.go` - `HubWSNotifier` wired into agent service config

### Session Metrics Tool
- `internal/agent/types.go` - `SessionMetrics` struct
- `internal/agent/store.go` - `GetSessionMetrics`: aggregate queries over agent_tool_calls
- `internal/agent/service.go` - `SessionMetrics` method: combines tool call stats with session note counts
- `internal/mcp/tools.go` - `session_metrics` tool definition + handler

### Tests
- `internal/mcp/v2_tools_test.go` - memory_search, session_metrics, context_gather scope tests
- `internal/agent/v2_service_test.go` - MemorySearch, SessionMetrics, ContextGather scope, WSNotifier event tests
- `internal/search/v2_fts_test.go` - SearchScoped include/exclude project filter tests
- Updated mock in `server_test.go` for V2 interface changes

### MCP Tool Count
- V1: 16 tools (session x6, memory x5, context_gather, notes x4)
- V2: 18 tools (+memory_search, +session_metrics)
