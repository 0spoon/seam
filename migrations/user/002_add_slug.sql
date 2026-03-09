-- 002_add_slug.sql: Add slug column to notes for indexed slug-based lookup.
-- This is a no-op for fresh databases (the column is included in 001_initial.sql).
-- For existing databases upgraded from an older schema, the column needs to be added.
-- The actual ALTER TABLE is handled in Go code (migrations.Run) because SQLite
-- does not support ADD COLUMN IF NOT EXISTS. The Go runner checks for the
-- column before attempting the ALTER TABLE.

-- The index is always safe to create (IF NOT EXISTS).
CREATE INDEX IF NOT EXISTS idx_notes_slug ON notes(slug);
