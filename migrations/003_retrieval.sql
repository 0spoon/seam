-- 003_retrieval.sql
-- Retrieval pipeline: note descriptions, session project scoping,
-- retrieval telemetry.

ALTER TABLE notes ADD COLUMN description TEXT NOT NULL DEFAULT '';

ALTER TABLE agent_sessions ADD COLUMN project_slug TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_agent_sessions_project
    ON agent_sessions(project_slug, updated_at);

CREATE TABLE IF NOT EXISTS retrieval_events (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    kind         TEXT NOT NULL,
    project_slug TEXT NOT NULL DEFAULT '',
    query        TEXT NOT NULL DEFAULT '',
    items        TEXT NOT NULL DEFAULT '[]',
    hit          INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_retrieval_events_created ON retrieval_events(created_at);
CREATE INDEX IF NOT EXISTS idx_retrieval_events_kind ON retrieval_events(kind, created_at);
