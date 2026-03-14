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
  - Agent-memory project auto-creation with per-user caching
  - Knowledge search with semantic-to-FTS fallback
- `internal/agent/briefing.go` - Budget allocation with proportional redistribution, word-boundary truncation
- All 25+ service tests pass, briefing tests pass

## Phase 3: MCP Server + Tools
**Status: COMPLETE**

- `internal/mcp/server.go` - MCP server with auth middleware (WithHTTPContextFunc for JWT), logging middleware, tool handler middleware; AgentService interface includes ContextGather
- `internal/mcp/tools.go` - 12 tools registered and fully implemented:
  - Session tools: session_start, session_plan_set, session_progress_update, session_context_set, session_end, session_list
  - Memory tools: memory_read, memory_write, memory_append, memory_list, memory_delete
  - Context: context_gather (uses service's searchKnowledge with budget truncation)
- `internal/mcp/logging.go` - Logging middleware for tool calls
- `internal/server/server.go` - MCPHandler field in Config, mounted at /api/mcp, CORS includes Mcp-Session-Id
- All MCP server and tool tests pass

## Phase 4: Wiring (main.go)
**Status: COMPLETE**

- `cmd/seamd/main.go` - Agent store, service, and MCP server created and wired:
  - `agent.NewSQLStore()` for data layer
  - `agent.NewService()` with NoteService, ProjectService, SearchService, UserDBManager
  - `mcp.New()` with AgentService
  - `mcpSrv.Handler(jwtMgr)` passed as MCPHandler to server.Config
- Full build succeeds, all tests pass

## Test Summary
- **160+ tests** across all packages (agent store, service, types, briefing, MCP server, tools, logging)
- All tests passing

## Deferred to v2
- `AIQueue`/embed task enqueuing in agent service (watcher suppression means agent notes won't auto-embed; explicit enqueue needed for scope filtering)
- Embedder metadata for scope filtering (`agent:true`, `memory_type`, `session_name`)
- ChromaDB `where` filter support for `scope: agent|user` vs `scope: all`
- notes_search, notes_read, notes_list, notes_create MCP tools (not in current test expectations)
- Tool call audit persistence to agent_tool_calls table (currently slog-only)
- WebSocket events for agent note changes (real-time UI updates)
