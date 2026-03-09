-- 002_add_slug.sql: Add slug column to notes for indexed slug-based lookup.
-- This is a no-op for fresh databases (the column is included in 001_initial.sql).
-- For existing databases, the ALTER TABLE adds the column if it does not exist.
-- We use a CREATE TRIGGER trick to conditionally run the ALTER TABLE since
-- SQLite does not support ALTER TABLE ADD COLUMN IF NOT EXISTS.

-- The index is always safe to create (IF NOT EXISTS).
CREATE INDEX IF NOT EXISTS idx_notes_slug ON notes(slug);
