-- 002_agent_sessions.sql: Agent session tracking and tool call audit log.

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
