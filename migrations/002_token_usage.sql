-- Token usage tracking for AI calls (chat completions, embeddings).
-- Each row represents a single AI provider call with token counts.

CREATE TABLE IF NOT EXISTS token_usage (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    function        TEXT NOT NULL,              -- e.g. 'chat', 'assistant', 'embedding'
    provider        TEXT NOT NULL,              -- 'ollama', 'openai', 'anthropic'
    model           TEXT NOT NULL,              -- actual model name (e.g. 'gpt-4o-mini', 'llama3:8b')
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    is_local        INTEGER NOT NULL DEFAULT 0, -- 1 for Ollama (free, not billed)
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    conversation_id TEXT,                       -- optional, groups assistant turns
    created_at      TEXT NOT NULL               -- RFC3339
);

-- Dashboard: totals for a date range.
CREATE INDEX IF NOT EXISTS idx_token_usage_created ON token_usage(created_at);
-- Per-function breakdown.
CREATE INDEX IF NOT EXISTS idx_token_usage_function ON token_usage(function, created_at);
-- Per-provider breakdown.
CREATE INDEX IF NOT EXISTS idx_token_usage_provider ON token_usage(provider, created_at);
-- Per-model breakdown.
CREATE INDEX IF NOT EXISTS idx_token_usage_model ON token_usage(model, created_at);
-- Budget check: sum non-local tokens in current period.
CREATE INDEX IF NOT EXISTS idx_token_usage_billing ON token_usage(is_local, created_at);
