package note

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Domain errors.
var ErrNotFound = errors.New("not found")

// Note represents a note stored in the per-user SQLite database.
type Note struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	ProjectID        string    `json:"project_id,omitempty"`
	FilePath         string    `json:"file_path"`
	Body             string    `json:"body"`
	ContentHash      string    `json:"-"`
	SourceURL        string    `json:"source_url,omitempty"`
	TranscriptSource bool      `json:"transcript_source,omitempty"`
	Tags             []string  `json:"tags"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// NoteFilter controls listing and pagination of notes.
type NoteFilter struct {
	ProjectID string
	InboxOnly bool
	Tag       string
	Since     time.Time
	Until     time.Time
	Sort      string // "created" or "modified"
	SortDir   string // "asc" or "desc"
	Limit     int
	Offset    int
}

// TagCount holds a tag name and the number of notes using it.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DBTX is an interface satisfied by both *sql.DB and *sql.Tx.
// Store methods that participate in multi-step operations accept DBTX
// so callers can pass a transaction for atomicity.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// SQLStore provides data access methods for notes against a per-user SQLite DB.
type SQLStore struct{}

// NewSQLStore creates a new SQLStore.
func NewSQLStore() *SQLStore {
	return &SQLStore{}
}

// Create inserts a note and its tags into the database.
// Accepts DBTX so it can participate in a transaction.
func (s *SQLStore) Create(ctx context.Context, db DBTX, n *Note) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, project_id, file_path, body, content_hash,
		 source_url, transcript_source, slug, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Title, nullString(n.ProjectID), n.FilePath, n.Body, n.ContentHash,
		nullString(n.SourceURL), boolToInt(n.TranscriptSource),
		slugify(n.Title),
		n.CreatedAt.Format(time.RFC3339), n.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("note.SQLStore.Create: %w", err)
	}
	return nil
}

// Get retrieves a note by ID, including its tags.
func (s *SQLStore) Get(ctx context.Context, db DBTX, id string) (*Note, error) {
	n, err := s.scanNote(db.QueryRowContext(ctx,
		`SELECT id, title, project_id, file_path, body, content_hash,
		 source_url, transcript_source, created_at, updated_at
		 FROM notes WHERE id = ?`, id,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("note.SQLStore.Get: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("note.SQLStore.Get: %w", err)
	}

	tags, err := s.loadTags(ctx, db, n.ID)
	if err != nil {
		return nil, fmt.Errorf("note.SQLStore.Get: load tags: %w", err)
	}
	n.Tags = tags
	return n, nil
}

// GetByFilePath retrieves a note by its file path.
func (s *SQLStore) GetByFilePath(ctx context.Context, db DBTX, filePath string) (*Note, error) {
	n, err := s.scanNote(db.QueryRowContext(ctx,
		`SELECT id, title, project_id, file_path, body, content_hash,
		 source_url, transcript_source, created_at, updated_at
		 FROM notes WHERE file_path = ?`, filePath,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("note.SQLStore.GetByFilePath: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("note.SQLStore.GetByFilePath: %w", err)
	}

	tags, err := s.loadTags(ctx, db, n.ID)
	if err != nil {
		return nil, fmt.Errorf("note.SQLStore.GetByFilePath: load tags: %w", err)
	}
	n.Tags = tags
	return n, nil
}

// List returns notes matching the filter along with the total count.
func (s *SQLStore) List(ctx context.Context, db DBTX, filter NoteFilter) ([]*Note, int, error) {
	var where []string
	var args []interface{}

	// C-35: ProjectID and InboxOnly are mutually exclusive. If both are
	// set, ProjectID takes precedence (inbox notes have no project).
	if filter.ProjectID != "" {
		where = append(where, "n.project_id = ?")
		args = append(args, filter.ProjectID)
	} else if filter.InboxOnly {
		where = append(where, "n.project_id IS NULL")
	}
	if filter.Tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM note_tags nt JOIN tags t ON t.id = nt.tag_id WHERE nt.note_id = n.id AND t.name = ?)")
		args = append(args, filter.Tag)
	}
	// Use the same column for time filters as for sorting. Default sort
	// column is updated_at; only "created" switches to created_at.
	timeCol := "n.updated_at"
	if filter.Sort == "created" {
		timeCol = "n.created_at"
	}
	if !filter.Since.IsZero() {
		where = append(where, timeCol+" >= ?")
		args = append(args, filter.Since.Format(time.RFC3339))
	}
	if !filter.Until.IsZero() {
		where = append(where, timeCol+" <= ?")
		args = append(args, filter.Until.Format(time.RFC3339))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	// Count total matching notes.
	countQuery := "SELECT COUNT(*) FROM notes n" + whereClause
	var total int
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("note.SQLStore.List: count: %w", err)
	}

	// Determine sort column and direction.
	sortCol := "n.updated_at"
	if filter.Sort == "created" {
		sortCol = "n.created_at"
	}
	sortDir := "DESC"
	if strings.EqualFold(filter.SortDir, "asc") {
		sortDir = "ASC"
	}

	query := fmt.Sprintf(
		`SELECT n.id, n.title, n.project_id, n.file_path, n.body, n.content_hash,
		 n.source_url, n.transcript_source, n.created_at, n.updated_at
		 FROM notes n%s ORDER BY %s %s`,
		whereClause, sortCol, sortDir,
	)

	listArgs := make([]interface{}, len(args))
	copy(listArgs, args)

	if filter.Limit > 0 {
		query += " LIMIT ?"
		listArgs = append(listArgs, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		listArgs = append(listArgs, filter.Offset)
	}

	rows, err := db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("note.SQLStore.List: %w", err)
	}
	defer rows.Close()

	var notes []*Note
	for rows.Next() {
		n, err := s.scanNoteRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("note.SQLStore.List: scan: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("note.SQLStore.List: rows: %w", err)
	}

	// Batch-load tags for all notes in a single query (avoids N+1).
	if err := s.loadAllTags(ctx, db, notes); err != nil {
		return nil, 0, fmt.Errorf("note.SQLStore.List: load tags: %w", err)
	}

	return notes, total, nil
}

// Update modifies an existing note row.
// Accepts DBTX so it can participate in a transaction.
func (s *SQLStore) Update(ctx context.Context, db DBTX, n *Note) error {
	result, err := db.ExecContext(ctx,
		`UPDATE notes SET title = ?, project_id = ?, file_path = ?, body = ?,
		 content_hash = ?, source_url = ?, transcript_source = ?, slug = ?,
		 updated_at = ?
		 WHERE id = ?`,
		n.Title, nullString(n.ProjectID), n.FilePath, n.Body,
		n.ContentHash, nullString(n.SourceURL), boolToInt(n.TranscriptSource),
		slugify(n.Title),
		n.UpdatedAt.Format(time.RFC3339), n.ID,
	)
	if err != nil {
		return fmt.Errorf("note.SQLStore.Update: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("note.SQLStore.Update: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("note.SQLStore.Update: %w", ErrNotFound)
	}
	return nil
}

// Delete removes a note by ID.
// Accepts DBTX so it can participate in a transaction.
func (s *SQLStore) Delete(ctx context.Context, db DBTX, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("note.SQLStore.Delete: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("note.SQLStore.Delete: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("note.SQLStore.Delete: %w", ErrNotFound)
	}
	return nil
}

// GetBacklinks returns all notes that link to the given note ID.
func (s *SQLStore) GetBacklinks(ctx context.Context, db DBTX, noteID string) ([]*Note, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT n.id, n.title, n.project_id, n.file_path, n.body, n.content_hash,
		 n.source_url, n.transcript_source, n.created_at, n.updated_at
		 FROM notes n
		 JOIN links l ON l.source_note_id = n.id
		 WHERE l.target_note_id = ?`, noteID,
	)
	if err != nil {
		return nil, fmt.Errorf("note.SQLStore.GetBacklinks: %w", err)
	}
	defer rows.Close()

	var notes []*Note
	for rows.Next() {
		n, err := s.scanNoteRow(rows)
		if err != nil {
			return nil, fmt.Errorf("note.SQLStore.GetBacklinks: scan: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("note.SQLStore.GetBacklinks: rows: %w", err)
	}
	return notes, nil
}

// UpdateLinks replaces all outgoing links for a note, resolving targets by
// title or filename match. Unresolved links are stored with a NULL target.
// Accepts DBTX so it can participate in a transaction.
func (s *SQLStore) UpdateLinks(ctx context.Context, db DBTX, noteID string, links []Link) error {
	// Delete existing links for this note.
	if _, err := db.ExecContext(ctx, `DELETE FROM links WHERE source_note_id = ?`, noteID); err != nil {
		return fmt.Errorf("note.SQLStore.UpdateLinks: delete: %w", err)
	}

	if len(links) == 0 {
		return nil
	}

	stmt, err := db.PrepareContext(ctx,
		`INSERT OR IGNORE INTO links (source_note_id, target_note_id, link_text) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("note.SQLStore.UpdateLinks: prepare: %w", err)
	}
	defer stmt.Close()

	for _, link := range links {
		targetID, _ := s.ResolveLink(ctx, db, link.Target)
		if _, err := stmt.ExecContext(ctx, noteID, nullString(targetID), link.Target); err != nil {
			return fmt.Errorf("note.SQLStore.UpdateLinks: insert %q: %w", link.Target, err)
		}
	}
	return nil
}

// ResolveLink finds a note by title, filename, or slug match.
// Accepts DBTX so it can participate in a transaction.
// Resolution order:
//  1. Exact match on title (case-insensitive)
//  2. Exact match on filename without .md extension (case-insensitive)
//  3. Slug match: slugify the link text and compare to slugified titles
//  4. No match -> returns empty string and ErrNotFound
func (s *SQLStore) ResolveLink(ctx context.Context, db DBTX, linkText string) (string, error) {
	var noteID string
	// Step 1: exact title match (case-insensitive).
	err := db.QueryRowContext(ctx,
		`SELECT id FROM notes WHERE LOWER(title) = LOWER(?) LIMIT 1`, linkText,
	).Scan(&noteID)
	if err == nil {
		return noteID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("note.SQLStore.ResolveLink: title: %w", err)
	}

	// Step 2: filename match (file_path is exactly "linkText.md" or ends
	// with "/linkText.md"). Two conditions with OR prevent false matches
	// like "something-linkText.md".
	escaped := escapeLIKE(linkText)
	err = db.QueryRowContext(ctx,
		`SELECT id FROM notes WHERE (LOWER(file_path) = LOWER(?) OR LOWER(file_path) LIKE LOWER(?) ESCAPE '\') LIMIT 1`,
		escaped+".md", "%/"+escaped+".md",
	).Scan(&noteID)
	if err == nil {
		return noteID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("note.SQLStore.ResolveLink: file_path: %w", err)
	}

	// Step 3: slug match using the indexed slug column. This allows
	// [[api-design-patterns]] to resolve to a note titled "API Design Patterns".
	linkSlug := slugify(linkText)
	if linkSlug != "" {
		err = db.QueryRowContext(ctx,
			`SELECT id FROM notes WHERE slug = ? LIMIT 1`, linkSlug,
		).Scan(&noteID)
		if err == nil {
			return noteID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("note.SQLStore.ResolveLink: slug: %w", err)
		}
	}

	return "", ErrNotFound
}

// escapeLIKE escapes SQL LIKE wildcard characters so they are treated as
// literals when used inside a LIKE pattern with ESCAPE '\'.
func escapeLIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// slugify converts a string to a lowercase hyphenated slug for comparison.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Keep only alphanumeric and hyphens.
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	// Collapse multiple hyphens.
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	return result
}

// ResolveDanglingLinks finds links with NULL target_note_id whose link_text
// matches the new note's title, filename, or slug, and sets their target
// to noteID.
func (s *SQLStore) ResolveDanglingLinks(ctx context.Context, db DBTX, noteID, title, filePath string) error {
	// Extract filename without extension from filePath for matching.
	filename := filePath
	if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
		filename = filePath[idx+1:]
	}
	filename = strings.TrimSuffix(filename, ".md")

	// Step 1 & 2: title and filename match via SQL.
	_, err := db.ExecContext(ctx,
		`UPDATE links SET target_note_id = ?
		 WHERE target_note_id IS NULL
		 AND (LOWER(link_text) = LOWER(?) OR LOWER(link_text) = LOWER(?))`,
		noteID, title, filename,
	)
	if err != nil {
		return fmt.Errorf("note.SQLStore.ResolveDanglingLinks: %w", err)
	}

	// Step 3: slug match. Use the note's slug to match dangling links whose
	// slugified link_text matches. We still need to iterate dangling links
	// because link_text is free-form, but we only compare against this note's
	// known slug rather than loading all notes.
	titleSlug := slugify(title)
	if titleSlug == "" {
		return nil
	}

	rows, err := db.QueryContext(ctx,
		`SELECT source_note_id, link_text FROM links WHERE target_note_id IS NULL`)
	if err != nil {
		return fmt.Errorf("note.SQLStore.ResolveDanglingLinks: query dangling: %w", err)
	}
	defer rows.Close()

	type danglingLink struct {
		sourceNoteID string
		linkText     string
	}
	var toResolve []danglingLink
	for rows.Next() {
		var dl danglingLink
		if scanErr := rows.Scan(&dl.sourceNoteID, &dl.linkText); scanErr != nil {
			continue
		}
		if slugify(dl.linkText) == titleSlug {
			toResolve = append(toResolve, dl)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("note.SQLStore.ResolveDanglingLinks: rows: %w", rowsErr)
	}

	for _, dl := range toResolve {
		_, updateErr := db.ExecContext(ctx,
			`UPDATE links SET target_note_id = ?
			 WHERE source_note_id = ? AND link_text = ? AND target_note_id IS NULL`,
			noteID, dl.sourceNoteID, dl.linkText,
		)
		if updateErr != nil {
			return fmt.Errorf("note.SQLStore.ResolveDanglingLinks: update slug match: %w", updateErr)
		}
	}

	return nil
}

// UpdateTags replaces all tags for a note. Creates new tag rows as needed.
// Accepts DBTX so it can participate in a transaction.
func (s *SQLStore) UpdateTags(ctx context.Context, db DBTX, noteID string, tags []string) error {
	// Remove existing tags for this note.
	if _, err := db.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = ?`, noteID); err != nil {
		return fmt.Errorf("note.SQLStore.UpdateTags: delete: %w", err)
	}

	if len(tags) == 0 {
		return nil
	}

	for _, tag := range tags {
		// Ensure tag exists.
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag); err != nil {
			return fmt.Errorf("note.SQLStore.UpdateTags: insert tag %q: %w", tag, err)
		}

		// Get tag ID.
		var tagID int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, tag).Scan(&tagID); err != nil {
			return fmt.Errorf("note.SQLStore.UpdateTags: get tag id %q: %w", tag, err)
		}

		// Link note to tag.
		if _, err := db.ExecContext(ctx,
			`INSERT OR IGNORE INTO note_tags (note_id, tag_id) VALUES (?, ?)`, noteID, tagID,
		); err != nil {
			return fmt.Errorf("note.SQLStore.UpdateTags: insert note_tag %q: %w", tag, err)
		}
	}
	return nil
}

// ListTags returns all tags with the count of notes using each.
func (s *SQLStore) ListTags(ctx context.Context, db DBTX) ([]TagCount, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.name, COUNT(nt.note_id) as cnt
		 FROM tags t
		 JOIN note_tags nt ON nt.tag_id = t.id
		 GROUP BY t.id
		 ORDER BY cnt DESC, t.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("note.SQLStore.ListTags: %w", err)
	}
	defer rows.Close()

	var tags []TagCount
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Name, &tc.Count); err != nil {
			return nil, fmt.Errorf("note.SQLStore.ListTags: scan: %w", err)
		}
		tags = append(tags, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("note.SQLStore.ListTags: rows: %w", err)
	}
	return tags, nil
}

// scanNote scans a single note row from a *sql.Row.
func (s *SQLStore) scanNote(row *sql.Row) (*Note, error) {
	n := &Note{}
	var projectID, sourceURL sql.NullString
	var transcriptSource int
	var createdAt, updatedAt string

	err := row.Scan(&n.ID, &n.Title, &projectID, &n.FilePath, &n.Body,
		&n.ContentHash, &sourceURL, &transcriptSource, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	n.ProjectID = projectID.String
	n.SourceURL = sourceURL.String
	n.TranscriptSource = transcriptSource != 0

	var parseErr error
	n.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		slog.Warn("note.SQLStore.scanNote: malformed created_at, using zero time",
			"note_id", n.ID, "raw", createdAt, "error", parseErr)
	}
	n.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
	if parseErr != nil {
		slog.Warn("note.SQLStore.scanNote: malformed updated_at, using zero time",
			"note_id", n.ID, "raw", updatedAt, "error", parseErr)
	}
	return n, nil
}

// scanNoteRow scans a single note from *sql.Rows.
func (s *SQLStore) scanNoteRow(rows *sql.Rows) (*Note, error) {
	n := &Note{}
	var projectID, sourceURL sql.NullString
	var transcriptSource int
	var createdAt, updatedAt string

	err := rows.Scan(&n.ID, &n.Title, &projectID, &n.FilePath, &n.Body,
		&n.ContentHash, &sourceURL, &transcriptSource, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	n.ProjectID = projectID.String
	n.SourceURL = sourceURL.String
	n.TranscriptSource = transcriptSource != 0

	var parseErr error
	n.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		slog.Warn("note.SQLStore.scanNoteRow: malformed created_at, using zero time",
			"note_id", n.ID, "raw", createdAt, "error", parseErr)
	}
	n.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
	if parseErr != nil {
		slog.Warn("note.SQLStore.scanNoteRow: malformed updated_at, using zero time",
			"note_id", n.ID, "raw", updatedAt, "error", parseErr)
	}
	return n, nil
}

// loadAllTags batch-loads tags for a slice of notes in a single query,
// replacing the N+1 per-note loadTags pattern.
func (s *SQLStore) loadAllTags(ctx context.Context, db DBTX, notes []*Note) error {
	if len(notes) == 0 {
		return nil
	}

	// Build note ID index and IN-clause placeholders.
	placeholders := make([]string, len(notes))
	args := make([]interface{}, len(notes))
	noteIndex := make(map[string]int, len(notes))
	for i, n := range notes {
		placeholders[i] = "?"
		args[i] = n.ID
		noteIndex[n.ID] = i
	}

	query := fmt.Sprintf(
		`SELECT nt.note_id, t.name
		 FROM note_tags nt
		 JOIN tags t ON t.id = nt.tag_id
		 WHERE nt.note_id IN (%s)
		 ORDER BY t.name`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("note.SQLStore.loadAllTags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var noteID, tagName string
		if err := rows.Scan(&noteID, &tagName); err != nil {
			return fmt.Errorf("note.SQLStore.loadAllTags: scan: %w", err)
		}
		if idx, ok := noteIndex[noteID]; ok {
			notes[idx].Tags = append(notes[idx].Tags, tagName)
		}
	}
	return rows.Err()
}

// loadTags loads all tag names for a note.
func (s *SQLStore) loadTags(ctx context.Context, db DBTX, noteID string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.name FROM tags t
		 JOIN note_tags nt ON nt.tag_id = t.id
		 WHERE nt.note_id = ?
		 ORDER BY t.name`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// nullString converts a Go string to sql.NullString (empty -> NULL).
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// boolToInt converts a boolean to an integer for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
