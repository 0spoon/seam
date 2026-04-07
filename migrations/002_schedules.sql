-- 002_schedules.sql: Scheduler tables for cron-based jobs (Phase 3).
--
-- Stores both recurring (cron_expr) and one-shot (run_at) schedules. The
-- scheduler service polls this table on a ticker, runs due jobs, and
-- updates last_run_at / next_run_at to keep the polling cheap.

CREATE TABLE IF NOT EXISTS schedules (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    cron_expr     TEXT NOT NULL DEFAULT '',
    run_at        TEXT,
    action_type   TEXT NOT NULL,
    action_config TEXT NOT NULL DEFAULT '{}',
    enabled       INTEGER NOT NULL DEFAULT 1,
    last_run_at   TEXT,
    last_error    TEXT,
    next_run_at   TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_schedules_enabled_next ON schedules(enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_schedules_action ON schedules(action_type);
