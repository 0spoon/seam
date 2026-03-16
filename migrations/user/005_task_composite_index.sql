-- Add composite index for the common task listing query that sorts by
-- done status then updated_at.
CREATE INDEX IF NOT EXISTS idx_tasks_done_updated ON tasks(done, updated_at DESC);
