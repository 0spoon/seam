package migrations

import (
	"database/sql"
	"fmt"
	"strings"
)

// Run applies all unapplied migrations from the given list to db.
// It creates the schema_migrations tracking table if it does not exist,
// then executes each migration whose version is higher than the current
// max version. Both the migration SQL and version recording are wrapped
// in a single transaction for atomicity.
func Run(db *sql.DB, migrations []Migration) error {
	// Create version tracking table.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		return fmt.Errorf("migrations.Run: create tracking table: %w", err)
	}

	// Find current max version.
	var current int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("migrations.Run: read current version: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}

		// Run pre-migration hooks (e.g., conditional ALTER TABLE).
		if m.PreHook != nil {
			if err := m.PreHook(db); err != nil {
				return fmt.Errorf("migrations.Run: pre-hook version %d: %w", m.Version, err)
			}
		}

		// Wrap both the migration SQL and version recording in a
		// single transaction for atomicity.
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("migrations.Run: begin tx version %d: %w", m.Version, err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("migrations.Run: version %d: %w", m.Version, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version) VALUES (?)",
			m.Version,
		); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("migrations.Run: record version %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrations.Run: commit version %d: %w", m.Version, err)
		}
	}

	return nil
}

// HasColumn checks if a table has a specific column using PRAGMA table_info.
func HasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typeName string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dfltValue, &pk); err != nil {
			continue
		}
		if strings.EqualFold(name, column) {
			return true
		}
	}
	return false
}
