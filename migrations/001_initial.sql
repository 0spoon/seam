-- 001_initial.sql: Complete schema for seam.db (single-user system).
--
-- Note: PRAGMA journal_mode=WAL and PRAGMA foreign_keys=ON are set by the
-- Go code on every connection open (they are connection-level, not schema-level).
-- Do NOT put PRAGMAs in migration files.

-- ============================================================
-- Owner identity and authentication
-- ============================================================

CREATE TABLE IF NOT EXISTS owner (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL REFERENCES owner(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_owner ON refresh_tokens(owner_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at);

CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    scopes       TEXT NOT NULL DEFAULT '["*"]',
    last_used_at TEXT,
    expires_at   TEXT,
    created_at   TEXT NOT NULL,
    revoked_at   TEXT
);

-- ============================================================
-- Projects and notes
-- ============================================================

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notes (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL,
    project_id      TEXT REFERENCES projects(id) ON DELETE SET NULL,
    file_path       TEXT NOT NULL UNIQUE,
    body            TEXT NOT NULL DEFAULT '',
    content_hash    TEXT NOT NULL,
    source_url      TEXT,
    transcript_source INTEGER NOT NULL DEFAULT 0,
    slug            TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notes_project ON notes(project_id);
CREATE INDEX IF NOT EXISTS idx_notes_updated ON notes(updated_at);
CREATE INDEX IF NOT EXISTS idx_notes_slug ON notes(slug);

CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS note_tags (
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag_id);

CREATE TABLE IF NOT EXISTS links (
    source_note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    target_note_id  TEXT REFERENCES notes(id) ON DELETE SET NULL,
    link_text       TEXT NOT NULL,
    PRIMARY KEY (source_note_id, link_text)
);

CREATE INDEX IF NOT EXISTS idx_links_target ON links(target_note_id);

-- ============================================================
-- Full-text search
-- ============================================================

-- External content FTS5 table synced with notes via triggers.
CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
    title,
    body,
    content='notes',
    content_rowid='rowid',
    tokenize='porter'
);

-- Triggers to keep FTS in sync with the notes table.
CREATE TRIGGER IF NOT EXISTS notes_fts_insert AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END;

CREATE TRIGGER IF NOT EXISTS notes_fts_delete AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, body)
    VALUES('delete', old.rowid, old.title, old.body);
END;

CREATE TRIGGER IF NOT EXISTS notes_fts_update AFTER UPDATE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, body)
    VALUES('delete', old.rowid, old.title, old.body);
    INSERT INTO notes_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END;

-- ============================================================
-- AI task queue
-- ============================================================

CREATE TABLE IF NOT EXISTS ai_tasks (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    priority    INTEGER NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    payload     TEXT NOT NULL,
    result      TEXT,
    error       TEXT,
    created_at  TEXT NOT NULL,
    started_at  TEXT,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_ai_tasks_status ON ai_tasks(status, priority, created_at);

-- ============================================================
-- Settings
-- ============================================================

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- ============================================================
-- Conversations and messages (chat history)
-- ============================================================

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
    citations       TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation
    ON messages(conversation_id, created_at);

-- ============================================================
-- Note versions (history)
-- ============================================================

CREATE TABLE IF NOT EXISTS note_versions (
    id TEXT PRIMARY KEY,
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(note_id, version)
);

CREATE INDEX IF NOT EXISTS idx_note_versions_note_id ON note_versions(note_id);

-- ============================================================
-- Agent sessions and tool call audit log
-- ============================================================

-- Agent session tracking (lifecycle metadata).
-- Actual session content (plan, progress, context) stored as notes in agent-memory project.
-- Sessions form a tree via parent_session_id (derived from "/" naming convention).
CREATE TABLE IF NOT EXISTS agent_sessions (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL UNIQUE,
    parent_session_id TEXT REFERENCES agent_sessions(id) ON DELETE SET NULL,
    status            TEXT NOT NULL DEFAULT 'active',
    findings          TEXT,
    metadata          TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_status ON agent_sessions(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_name ON agent_sessions(name);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_parent ON agent_sessions(parent_session_id);

-- Tool call audit log. Every MCP tool invocation is recorded.
-- session_id is nullable because some tools (notes_search, notes_read, notes_list,
-- memory_search) can be called outside of an active session.
CREATE TABLE IF NOT EXISTS agent_tool_calls (
    id          TEXT PRIMARY KEY,
    session_id  TEXT REFERENCES agent_sessions(id) ON DELETE CASCADE,
    tool_name   TEXT NOT NULL,
    arguments   TEXT NOT NULL DEFAULT '{}',
    result      TEXT,
    error       TEXT,
    duration_ms INTEGER,
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_session ON agent_tool_calls(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_tool ON agent_tool_calls(tool_name, created_at);

-- ============================================================
-- Task tracking (checkbox items extracted from notes)
-- ============================================================

CREATE TABLE IF NOT EXISTS tasks (
    id          TEXT PRIMARY KEY,
    note_id     TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    line_number INTEGER NOT NULL,
    content     TEXT NOT NULL,
    done        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_note ON tasks(note_id);
CREATE INDEX IF NOT EXISTS idx_tasks_done ON tasks(done);
CREATE INDEX IF NOT EXISTS idx_tasks_updated ON tasks(updated_at);
CREATE INDEX IF NOT EXISTS idx_tasks_done_updated ON tasks(done, updated_at DESC);

-- ============================================================
-- Webhooks
-- ============================================================

CREATE TABLE IF NOT EXISTS webhooks (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL DEFAULT '',
    event_types TEXT NOT NULL,
    filter      TEXT NOT NULL DEFAULT '{}',
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webhooks_active ON webhooks(active);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id          TEXT PRIMARY KEY,
    webhook_id  TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,
    payload     TEXT NOT NULL,
    status_code INTEGER,
    response    TEXT,
    error       TEXT,
    duration_ms INTEGER,
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries(created_at);
