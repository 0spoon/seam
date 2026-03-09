# Seam — Implementation Plan

Reference: [PLAN.md](./PLAN.md) for architecture decisions and feature scope. [FE_DESIGN.md](./FE_DESIGN.md) for frontend design system, component specs, and screen layouts.

---

## 1. Project Structure

```
seam/
  cmd/
    seamd/                      # server binary
      main.go
    seam/                       # TUI binary
      main.go
  internal/
    config/                     # config loading and validation
      config.go
      config_test.go
    server/                     # HTTP server setup, middleware, router
      server.go
      middleware.go
      middleware_test.go
    auth/                       # user accounts, JWT, bcrypt
      handler.go                # HTTP handlers: register, login, refresh
      handler_test.go
      service.go                # business logic: register, login, token lifecycle
      service_test.go
      jwt.go
      jwt_test.go
      store.go                  # user + refresh token CRUD against server.db
      store_test.go
    userdb/                     # per-user SQLite database manager
      manager.go                # open/close/cache per-user DBs
      manager_test.go
      migrate.go                # schema migrations
      migrate_test.go
    note/                       # note domain logic
      handler.go                # HTTP handlers: CRUD, backlinks
      handler_test.go
      service.go                # business logic: create, update, delete, list
      service_test.go
      store.go                  # SQLite queries against per-user DB
      store_test.go
      frontmatter.go            # YAML frontmatter parse/serialize
      frontmatter_test.go
      wikilink.go               # [[wikilink]] regex parsing
      wikilink_test.go
      tag.go                    # #tag parsing from body text
      tag_test.go
    project/                    # project domain logic
      handler.go
      handler_test.go
      service.go
      service_test.go
      store.go
      store_test.go
    search/                     # full-text and semantic search
      handler.go
      handler_test.go
      service.go                # coordinates FTS + semantic search
      service_test.go
      fts.go                    # SQLite FTS5 queries (store-level)
      fts_test.go
      semantic.go               # ChromaDB client, semantic search
      semantic_test.go
    ai/                         # Ollama client and task queue
      ollama.go                 # HTTP client for Ollama API
      ollama_test.go
      chroma.go                 # ChromaDB REST API client
      chroma_test.go
      queue.go                  # priority task queue
      queue_test.go
      embedder.go               # embedding generation pipeline
      embedder_test.go
      chat.go                   # Ask Seam: RAG chat
      chat_test.go
      synthesizer.go            # synthesis / summarization
      synthesizer_test.go
      linker.go                 # auto-link suggestion
      linker_test.go
      writer.go                 # AI writing assist (expand, summarize, extract actions)
      writer_test.go
    capture/                    # quick capture, URL fetch, voice
      handler.go
      handler_test.go
      url.go                    # URL fetch + title extraction
      url_test.go
      voice.go                  # Whisper transcription via Ollama
      voice_test.go
    template/                   # note templates
      handler.go
      handler_test.go
      service.go
      service_test.go
    watcher/                    # fsnotify file watcher
      watcher.go
      watcher_test.go
      reconcile.go              # startup reconciliation scan
      reconcile_test.go
    ws/                         # WebSocket hub
      hub.go                    # connection registry, broadcast
      hub_test.go
      client.go                 # per-connection read/write pumps
      protocol.go               # message types and serialization
      protocol_test.go
    testutil/                    # shared test helpers
      testutil.go               # TestServerDB, TestUserDB, TestDataDir, etc.
    graph/                      # graph data endpoint
      handler.go
      handler_test.go
      service.go
      service_test.go
  web/                          # React frontend (separate npm project)
    package.json
    src/
      ...
  migrations/
    server/                     # server.db migrations (SQL files)
      001_users.sql
    user/                       # per-user seam.db migrations (SQL files)
      001_initial.sql
  Makefile
  go.mod
  go.sum
  seam-server.yaml.example      # example config
```

### Package dependency rules

Strict layering to prevent circular imports:

```
cmd/ --> internal/server --> internal/{auth,note,project,search,capture,template,graph,ws}
                         --> internal/config
                         --> internal/userdb
                         --> internal/watcher
                         --> internal/ai

internal/{note,project,search,...} --> internal/userdb (to get DB handles)
                                   --> internal/ws (to push events)
                                   --> internal/ai (to enqueue tasks)

internal/note --> internal/project (to resolve project slug <-> ULID via project.Store.GetBySlug)

internal/capture --> internal/note (to create notes from captured content)
                 --> internal/ai (to enqueue transcription/summarization)

internal/ai --> internal/userdb (to read note content for RAG)

internal/watcher --> internal/ws (to push file change events)
                 --> internal/ai (to queue embedding regen)
```

No package imports `internal/server`. No circular dependencies. Each domain package exposes a `Service` struct that owns business logic, and a `Handler` struct that owns HTTP routing. The server wires them together at startup.

**Breaking the `note` <-> `watcher` cycle:** The watcher needs to trigger note re-indexing, and the note service needs to tell the watcher to suppress self-write events. If `watcher` imported `note` (for `Reindex`) and `note` imported `watcher` (for `IgnoreNext`), that would be a circular import. Instead:

- The `watcher` package defines a callback interface (`FileEventHandler`) that the watcher calls on file events. It does NOT import `note`.
- The `note` package defines a `WriteSuppressor` interface with an `IgnoreNext(filePath string)` method. The note service accepts this interface as a dependency (injected at startup), so it does NOT import `watcher`.
- At startup, `internal/server` wires them together: it creates the watcher, creates the note service with the watcher as its `WriteSuppressor`, and registers `note.Service.Reindex` as the watcher's `FileEventHandler`.

```
internal/watcher  -- defines FileEventHandler interface
                  -- accepts FileEventHandler at construction (no import of note)
internal/note     -- defines WriteSuppressor interface
                  -- accepts WriteSuppressor at construction (no import of watcher)
internal/server   -- wires: watcher.New(noteService.Reindex), noteService.New(..., watcher)
```

**Cross-package type usage:** The `search` package defines its own result types (`FTSResult`, `SemanticResult`) rather than importing `note.Note`. This avoids a dependency from `search` to `note`. If a search result needs full note data, the handler (in `search/handler.go`) can call `note.Service.Get()` to hydrate results — the server wires both services into the handler at startup. Similarly, `graph` defines its own `Node` and `Edge` types.

---

## 2. Database Schemas

### server.db (shared, one per server)

```sql
-- 001_users.sql
CREATE TABLE users (
    id          TEXT PRIMARY KEY,   -- ULID
    username    TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,       -- bcrypt hash
    created_at  TEXT NOT NULL,       -- RFC3339
    updated_at  TEXT NOT NULL
);

CREATE TABLE refresh_tokens (
    id          TEXT PRIMARY KEY,   -- ULID
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE, -- SHA-256 of refresh token (unique to prevent replay)
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens(expires_at);
```

Always open with `PRAGMA journal_mode=WAL;` and `PRAGMA foreign_keys=ON;`. WAL mode allows concurrent reads while a write is in progress — critical since auth checks happen on every request.

### seam.db (per user)

```sql
-- 001_initial.sql
-- Note: PRAGMA journal_mode=WAL and PRAGMA foreign_keys=ON are set by the
-- Go code on every connection open (they are connection-level, not schema-level).
-- Do NOT put PRAGMAs in migration files.

CREATE TABLE projects (
    id          TEXT PRIMARY KEY,   -- ULID
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE, -- filesystem directory name
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE notes (
    id              TEXT PRIMARY KEY,  -- ULID
    title           TEXT NOT NULL,
    project_id      TEXT REFERENCES projects(id) ON DELETE SET NULL,
    file_path       TEXT NOT NULL UNIQUE, -- relative to user's notes/ dir
    body            TEXT NOT NULL DEFAULT '', -- note content (denormalized from .md file for FTS sync)
    content_hash    TEXT NOT NULL,      -- SHA-256 of full file (frontmatter + body), for change detection
    source_url      TEXT,
    transcript_source INTEGER NOT NULL DEFAULT 0, -- 0=false, 1=true (SQLite has no bool)
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX idx_notes_project ON notes(project_id);
CREATE INDEX idx_notes_updated ON notes(updated_at);

-- Tags: many-to-many via join table.
-- Tags can come from frontmatter or inline #tag in body.
-- Both sources are merged at index time.
CREATE TABLE tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE note_tags (
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, tag_id)
);

CREATE INDEX idx_note_tags_tag ON note_tags(tag_id);

-- Link graph: directed edges parsed from [[wikilinks]] in note body.
-- target_note_id is nullable: the link may reference a note that
-- does not exist yet (a "dangling" link). We store the raw link
-- text so we can resolve it later if the target is created.
CREATE TABLE links (
    source_note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    target_note_id  TEXT REFERENCES notes(id) ON DELETE SET NULL,
    link_text       TEXT NOT NULL,  -- raw text inside [[ ]]
    PRIMARY KEY (source_note_id, link_text)
);

CREATE INDEX idx_links_target ON links(target_note_id);

-- Full-text search.
-- External content FTS5 table synced with the notes table via triggers.
-- body column in notes is a denormalized copy of the .md file content
-- (stripped of frontmatter). The .md file on disk remains source of truth;
-- body in notes is kept in sync by application code on every write/reindex.
CREATE VIRTUAL TABLE notes_fts USING fts5(
    title,
    body,
    content='notes',          -- external content: reads from notes table
    content_rowid='rowid',    -- uses notes' implicit rowid
    tokenize='porter'         -- Porter stemming for English
);

-- Triggers to keep FTS in sync with the notes table.
-- All three operations (insert, update, delete) are handled automatically.
CREATE TRIGGER notes_fts_insert AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END;

CREATE TRIGGER notes_fts_delete AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, body)
    VALUES('delete', old.rowid, old.title, old.body);
END;

CREATE TRIGGER notes_fts_update AFTER UPDATE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, body)
    VALUES('delete', old.rowid, old.title, old.body);
    INSERT INTO notes_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END;

-- AI task queue: persisted so tasks survive server restarts.
CREATE TABLE ai_tasks (
    id          TEXT PRIMARY KEY,  -- ULID
    type        TEXT NOT NULL,     -- 'embed', 'delete_embed', 'synthesize', 'autolink', 'chat', 'transcribe', 'assist'
    priority    INTEGER NOT NULL,  -- 0=interactive, 1=user-triggered, 2=background
    status      TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'running', 'done', 'failed'
    payload     TEXT NOT NULL,     -- JSON: task-specific data (note_id, query, etc.)
    result      TEXT,              -- JSON: task result (nullable until done)
    error       TEXT,              -- error message if failed
    created_at  TEXT NOT NULL,
    started_at  TEXT,
    finished_at TEXT
);

CREATE INDEX idx_ai_tasks_status ON ai_tasks(status, priority, created_at);
```

### Schema design notes

**Why external-content FTS5 with `body` in notes table:** The `.md` files on disk are the source of truth for note content. We store a denormalized copy of the body in the `notes.body` column so that SQLite triggers can automatically keep the FTS index in sync on insert, update, and delete. This avoids the critical flaw of contentless FTS5 tables: contentless tables require the *exact original values* to delete index entries, which is impossible when the body is not stored in the database. The trade-off (storing body text in both the file and SQLite) is negligible for 2-5 users and gives us correct FTS sync via triggers plus working `highlight()` and `snippet()` functions for search result display. On conflict, disk files always win: reconciliation overwrites `notes.body` from the file. `Service.Get` reads content from the filesystem, not from `notes.body`.

**Why `content_hash`:** The file watcher receives a "file changed" event, but we need to know if the content actually changed (editors sometimes write the same content). The hash covers the **full file content** (frontmatter + body) so that any change -- title, tags, project assignment, or body text -- triggers re-indexing. If the hash is unchanged, we skip all DB writes entirely.

**Why `link_text` as part of the primary key:** A note can link to the same target multiple times with different link text (e.g., `[[API Design]]` and `[[api-design]]` might both resolve to the same note). We store each unique link text to support accurate reconstruction and to resolve links by fuzzy matching against note titles and filenames.

**Why AI tasks are in per-user DB:** Tasks are scoped to a user's data. If we put them in server.db, every task operation would require joining against user context. Per-user keeps it simple. The in-memory queue in the `ai` package reads from all user DBs and merges them into a unified priority queue.

---

## 3. Core Types and Key Interfaces

Define these first, implement second. Types are listed before the interfaces that use them.

### 3a. Domain types

```go
// auth types

type User struct {
    ID        string
    Username  string
    Email     string
    Password  string    // bcrypt hash (never returned in API responses)
    CreatedAt time.Time
    UpdatedAt time.Time
}

type RegisterReq struct {
    Username string `json:"username"`
    Email    string `json:"email"`
    Password string `json:"password"` // plaintext, hashed by service
}

type LoginReq struct {
    Username string `json:"username"`
    Password string `json:"password"`
}

type TokenPair struct {
    AccessToken  string `json:"access_token"`
    RefreshToken string `json:"refresh_token"`
}

type AuthResponse struct {
    User      UserInfo  `json:"user"`
    TokenPair TokenPair `json:"tokens"`
}

type UserInfo struct {
    ID       string `json:"id"`
    Username string `json:"username"`
    Email    string `json:"email"`
}
```

```go
// note types

type Note struct {
    ID            string    `json:"id"`
    Title         string    `json:"title"`
    ProjectID     string    `json:"project_id,omitempty"` // empty for inbox notes
    FilePath      string    `json:"file_path"`            // relative to user's notes/ dir
    Body          string    `json:"body"`                 // content from .md file (read from disk on Get)
    ContentHash   string    `json:"-"`                    // SHA-256 of full file, not exposed in API
    SourceURL     string    `json:"source_url,omitempty"`
    TranscriptSource bool      `json:"transcript_source,omitempty"`
    Tags          []string  `json:"tags"`
    CreatedAt     time.Time `json:"created_at"`
    UpdatedAt     time.Time `json:"updated_at"`
}

type CreateNoteReq struct {
    Title     string   `json:"title"`
    Body      string   `json:"body"`
    ProjectID string   `json:"project_id,omitempty"` // empty = inbox
    Tags      []string `json:"tags,omitempty"`
    SourceURL string   `json:"source_url,omitempty"`
    Template  string   `json:"template,omitempty"`   // optional template name
}

// Note filenames: derived from the title, slugified (lowercase, hyphens,
// unsafe chars stripped), with ".md" extension. Example: title "API Design
// Patterns" -> filename "api-design-patterns.md". If a file with that name
// already exists in the target directory, append a numeric suffix:
// "api-design-patterns-2.md". The ULID is stored in frontmatter, NOT used
// as the filename, so files remain human-readable on disk.

type UpdateNoteReq struct {
    Title     *string  `json:"title,omitempty"`      // nil = no change
    Body      *string  `json:"body,omitempty"`
    ProjectID *string  `json:"project_id,omitempty"` // nil = no change, "" = move to inbox
    Tags      *[]string `json:"tags,omitempty"`       // nil = no change, &[]string{} = clear all, &[]string{"a","b"} = replace
}
// Note: Tags uses *[]string (pointer to slice) so JSON decoding can distinguish
// between "field absent" (nil pointer = no change) and "field present as empty
// array" (non-nil pointer to empty slice = clear all tags). A plain []string
// cannot reliably distinguish these cases across JSON boundaries.

type NoteFilter struct {
    ProjectID string // filter by project ULID; empty = all projects
    InboxOnly bool   // if true, return only notes with no project (inbox)
    Tag       string // filter by tag name
    Since     time.Time
    Until     time.Time
    Sort      string // "created" or "modified" (default: "modified")
    SortDir   string // "asc" or "desc" (default: "desc")
    Limit     int    // default 100, max 500; 0 = use default
    Offset    int    // for pagination
}
// Handler maps query params: ?project=inbox -> InboxOnly=true,
// ?project={ulid} -> ProjectID={ulid}. This avoids magic strings in
// the service/store layer.

type Link struct {
    TargetNoteID string // empty if dangling (target note does not exist)
    LinkText     string // raw text inside [[ ]], used for resolution
    Display      string // display alias from [[target|display]], empty if no alias
}

// Frontmatter represents the YAML frontmatter as it exists on disk.
// This is an intermediate type between the .md file and the Note domain type.
// Key difference: Frontmatter uses `Project` (slug string) while Note uses
// `ProjectID` (ULID). The note.Service translates between them.
type Frontmatter struct {
    ID               string    `yaml:"id"`
    Title            string    `yaml:"title"`
    Project          string    `yaml:"project,omitempty"`          // slug, NOT ULID
    Tags             []string  `yaml:"tags,omitempty"`
    Created          time.Time `yaml:"created"`
    Modified         time.Time `yaml:"modified"`
    SourceURL        string    `yaml:"source_url,omitempty"`
    TranscriptSource bool      `yaml:"transcript_source,omitempty"`
    Extra            map[string]interface{} `yaml:"-"` // unknown fields preserved on round-trip
}
```

```go
// project types

type Project struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Slug        string    `json:"slug"`
    Description string    `json:"description"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

```go
// ai types

type Task struct {
    ID         string
    UserID     string          // which user's DB this task belongs to
                               // NOT stored in ai_tasks table (implicit from per-user DB);
                               // populated by the queue loader when merging tasks from all user DBs
    Type       string          // "embed", "delete_embed", "synthesize", "autolink", "chat", "transcribe", "assist"
    Priority   int             // 0=interactive, 1=user-triggered, 2=background
    Status     string          // "pending", "running", "done", "failed"
    Payload    json.RawMessage // task-specific data (note_id, query, etc.)
    Result     json.RawMessage // task result (nil until done)
    Error      string          // error message if failed
    CreatedAt  time.Time
    StartedAt  time.Time
    FinishedAt time.Time
}

type TaskEvent struct {
    TaskID  string          `json:"task_id"`
    UserID  string          `json:"-"`       // not sent to client
    Type    string          `json:"type"`    // "progress", "complete", "failed"
    Payload json.RawMessage `json:"payload"`
}
```

```go
// ws types

type Message struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}
```

### 3b. Interfaces

```go
// config.Config - server configuration (see Section 3c for full struct)
```

```go
// userdb.Manager - manages per-user SQLite database lifecycle
type Manager interface {
    // Open returns a *sql.DB for the given user, creating the DB
    // and running migrations if it does not exist. Caches open handles.
    Open(ctx context.Context, userID string) (*sql.DB, error)

    // Close closes the DB for a user (e.g., on logout or eviction).
    Close(userID string) error

    // CloseAll closes all open databases (graceful shutdown).
    CloseAll() error

    // UserNotesDir returns the absolute path to a user's notes/ directory.
    UserNotesDir(userID string) string

    // ListUsers returns the IDs of all users who have a data directory.
    // Used by startup reconciliation (scan all users' notes) and the AI
    // task queue loader (reload pending tasks from all user DBs).
    // Scans {data_dir}/users/ for subdirectories.
    ListUsers(ctx context.Context) ([]string, error)
}
```

```go
// auth.Store - user and token persistence against server.db
type Store interface {
    CreateUser(ctx context.Context, u *User) error
    GetUserByUsername(ctx context.Context, username string) (*User, error)
    GetUserByID(ctx context.Context, id string) (*User, error)

    CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
    GetRefreshToken(ctx context.Context, tokenHash string) (userID string, expiresAt time.Time, err error)
    DeleteRefreshToken(ctx context.Context, tokenHash string) error
    DeleteRefreshTokensByUser(ctx context.Context, userID string) error
}
```

```go
// auth.Service - registration, login, token lifecycle
type Service interface {
    Register(ctx context.Context, req RegisterReq) (*AuthResponse, error)
    Login(ctx context.Context, req LoginReq) (*AuthResponse, error)
    Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
    // Refresh issues a new access token only. The refresh token itself is NOT
    // rotated (same refresh token remains valid until expiry or logout).
    // This simplifies the client: no need to update stored refresh tokens.
    // If token rotation is desired later, change the return type and delete
    // the old refresh token hash in the store.
    Logout(ctx context.Context, refreshToken string) error
}
```

```go
// project.Store - data access for projects in a user's SQLite DB.
type Store interface {
    Create(ctx context.Context, db *sql.DB, p *Project) error
    Get(ctx context.Context, db *sql.DB, id string) (*Project, error)
    GetBySlug(ctx context.Context, db *sql.DB, slug string) (*Project, error)
    List(ctx context.Context, db *sql.DB) ([]*Project, error)
    Update(ctx context.Context, db *sql.DB, p *Project) error
    Delete(ctx context.Context, db *sql.DB, id string) error
}
```

```go
// project.Service - business logic: coordinates store + filesystem directory management
type Service interface {
    Create(ctx context.Context, userID string, name, description string) (*Project, error)
    Get(ctx context.Context, userID, projectID string) (*Project, error)
    List(ctx context.Context, userID string) ([]*Project, error)
    Update(ctx context.Context, userID, projectID string, name, description *string) (*Project, error)
    Delete(ctx context.Context, userID, projectID string, cascade string) error
    // cascade controls what happens to notes in the deleted project:
    //   "inbox"  - move notes to inbox (update file_path, set project_id to NULL)
    //   "delete" - delete all notes in the project (files + DB rows)
    // Any other value returns an error.
}
```

```go
// note.Store - data access for notes in a user's SQLite DB.
// Every method receives *sql.DB because the database is per-user;
// the service resolves the correct DB handle via userdb.Manager.
type Store interface {
    Create(ctx context.Context, db *sql.DB, n *Note) error
    Get(ctx context.Context, db *sql.DB, id string) (*Note, error)
    GetByFilePath(ctx context.Context, db *sql.DB, filePath string) (*Note, error)
    List(ctx context.Context, db *sql.DB, filter NoteFilter) ([]*Note, int, error) // returns notes + total count for pagination
    Update(ctx context.Context, db *sql.DB, n *Note) error
    Delete(ctx context.Context, db *sql.DB, id string) error
    GetBacklinks(ctx context.Context, db *sql.DB, noteID string) ([]*Note, error)
    UpdateLinks(ctx context.Context, db *sql.DB, noteID string, links []Link) error
    // UpdateLinks replaces all links for the given note. For each link, it calls
    // ResolveLink to populate target_note_id. Links that cannot be resolved
    // are stored as dangling (target_note_id = NULL).
    ResolveLink(ctx context.Context, db *sql.DB, linkText string) (noteID string, err error)
    // ResolveLink attempts to find a note matching the link text:
    //   1. Exact match on title (case-insensitive)
    //   2. Exact match on filename without .md extension (case-insensitive)
    //   3. No match -> returns empty string and nil error (dangling link)
    ResolveDanglingLinks(ctx context.Context, db *sql.DB, noteID, title, filePath string) error
    // ResolveDanglingLinks is called after creating a new note. It queries all
    // links with target_note_id IS NULL and attempts to match their link_text
    // against the new note's title and filename. Updates matched links.
    UpdateTags(ctx context.Context, db *sql.DB, noteID string, tags []string) error
    // Note: FTS is maintained automatically by SQLite triggers on the notes table.
    // No explicit FTS update method is needed. Ensure notes.body is set correctly
    // on Create and Update, and the triggers handle the rest.
}
```

```go
// note.Service - business logic: coordinates file I/O, store, watcher, AI
type Service interface {
    Create(ctx context.Context, userID string, req CreateNoteReq) (*Note, error)
    Get(ctx context.Context, userID, noteID string) (*Note, error)
    List(ctx context.Context, userID string, filter NoteFilter) ([]*Note, int, error)
    Update(ctx context.Context, userID, noteID string, req UpdateNoteReq) (*Note, error)
    Delete(ctx context.Context, userID, noteID string) error
    Backlinks(ctx context.Context, userID, noteID string) ([]*Note, error)
    // Reindex re-indexes a note from its file on disk. Called by the file watcher
    // and reconciliation. If the file is new (not in DB), creates the note.
    // If the file was deleted, removes the note from DB.
    Reindex(ctx context.Context, userID, filePath string) error
}
// Note: changing a note's project (via Update with ProjectID set) moves the file
// on disk to the new project directory and updates file_path in the DB.
// Moving to inbox: set ProjectID to empty string.
//
// Frontmatter project resolution: The .md file stores `project: <slug>` in
// frontmatter (human-readable). The DB stores `project_id` (ULID). The service
// layer resolves between them:
//   - On file write (Create/Update): look up project ULID by slug, store ULID
//     in DB. Write slug to frontmatter.
//   - On file read (Reindex from disk): read slug from frontmatter, look up
//     ULID via project.Store.GetBySlug(). If the project slug does not exist,
//     set project_id to NULL (treat as inbox) and log a warning.
//   - On Get: return project_id (ULID) in the API response. The client uses
//     the ULID for API calls, not the slug.
```

```go
// ai.Queue - priority task queue for Ollama operations
type Queue interface {
    Enqueue(ctx context.Context, task Task) error
    // Run starts the queue processor (blocking, call in a goroutine).
    Run(ctx context.Context) error
    // Subscribe returns a channel that receives task status updates for a user.
    // The channel is closed when the context is cancelled (unsubscribe).
    Subscribe(ctx context.Context, userID string) <-chan TaskEvent
}
```

```go
// ai.OllamaClient - HTTP client for Ollama API
type OllamaClient interface {
    GenerateEmbedding(ctx context.Context, model, text string) ([]float64, error)
    ChatCompletion(ctx context.Context, model string, messages []ChatMessage) (*ChatResponse, error)
    ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage) (<-chan string, error)
    // ChatCompletion returns a complete response (stream=false in Ollama API).
    // ChatCompletionStream returns a channel of tokens (stream=true in Ollama API).
    // Use ChatCompletion for short operations (AI assist). Use ChatCompletionStream
    // for interactive responses (Ask Seam, synthesis).
}

type ChatMessage struct {
    Role    string `json:"role"`    // "system", "user", "assistant"
    Content string `json:"content"`
}

type ChatResponse struct {
    Content string `json:"content"`
}
```

```go
// ai.ChromaClient - HTTP client for ChromaDB API
type ChromaClient interface {
    CreateCollection(ctx context.Context, name string) (string, error)
    AddDocuments(ctx context.Context, collection string, ids []string, embeddings [][]float64, metadatas []map[string]string) error
    Query(ctx context.Context, collection string, embedding []float64, nResults int) ([]QueryResult, error)
    UpdateDocuments(ctx context.Context, collection string, ids []string, embeddings [][]float64, metadatas []map[string]string) error
    DeleteDocuments(ctx context.Context, collection string, ids []string) error
}

type QueryResult struct {
    ID       string            `json:"id"`
    Distance float64           `json:"distance"`
    Metadata map[string]string `json:"metadata"`
}
```

```go
// ai.Writer - AI writing assist (expand, summarize, extract actions)
type Writer interface {
    Assist(ctx context.Context, userID, noteID string, action string, selection string) (string, error)
    // action is one of: "expand", "summarize", "extract-actions"
    // If selection is non-empty, the LLM operates on the selected text.
    // If selection is empty, the LLM operates on the full note body.
}
```

```go
// search.Store - FTS5 query layer (store-level, receives *sql.DB)
type Store interface {
    Search(ctx context.Context, db *sql.DB, query string, limit, offset int) ([]FTSResult, int, error) // results + total count
}

// search.Service - coordinates FTS and semantic search (service-level, resolves DB via userdb.Manager)
type Service interface {
    SearchFTS(ctx context.Context, userID, query string, limit, offset int) ([]FTSResult, int, error)
    SearchSemantic(ctx context.Context, userID, query string, limit int) ([]SemanticResult, error)
}

type FTSResult struct {
    NoteID    string  `json:"note_id"`
    Title     string  `json:"title"`
    Snippet   string  `json:"snippet"`   // highlighted match from snippet()
    Rank      float64 `json:"rank"`      // bm25 score
}
```

```go
// search.SemanticSearcher - semantic search over embeddings (store-level).
// Used by search.Service internally. Implemented in search/semantic.go
// using ai.OllamaClient + ai.ChromaClient.
type SemanticSearcher interface {
    Search(ctx context.Context, userID, query string, limit int) ([]SemanticResult, error)
}

type SemanticResult struct {
    NoteID  string  `json:"note_id"`
    Title   string  `json:"title"`
    Score   float64 `json:"score"`
    Snippet string  `json:"snippet"`
}
```

```go
// ws.Hub - WebSocket connection registry
type Hub interface {
    Register(userID string, conn *websocket.Conn)
    Unregister(userID string, conn *websocket.Conn)
    Send(userID string, msg Message) error
    Broadcast(msg Message) error
    Run(ctx context.Context)
}
```

```go
// watcher.FileEventHandler - callback interface for file change events.
// Defined in the watcher package so it does NOT import note.
// The note.Service.Reindex method satisfies this interface.
type FileEventHandler func(ctx context.Context, userID, filePath string) error

// watcher.Watcher - filesystem watcher
type Watcher interface {
    // Watch starts watching a user's notes directory.
    Watch(userID string, notesDir string) error
    // Unwatch stops watching.
    Unwatch(userID string) error
    // Run starts processing events (blocking, call in a goroutine).
    Run(ctx context.Context) error
    // IgnoreNext tells the watcher to suppress the next event for the given
    // file path. Called by note.Service before writing a file to disk, so
    // the watcher does not trigger a redundant Reindex for API-initiated writes.
    // The suppression expires after a short TTL (e.g., 2 seconds) in case the
    // write never triggers an event (edge case).
    IgnoreNext(filePath string)
}
```

```go
// note.WriteSuppressor - interface accepted by note.Service to suppress
// watcher events for files the service writes itself. Defined in the note
// package so it does NOT import watcher. The watcher satisfies this interface.
type WriteSuppressor interface {
    IgnoreNext(filePath string)
}
```

```go
// capture.Service - quick capture (URL fetch, voice transcription)
// Note: capture returns a *note.Note because it delegates to note.Service.Create
// internally. The capture package imports note, which is allowed by the
// dependency graph (capture is a domain package like note, not imported by note).
type Service interface {
    CaptureURL(ctx context.Context, userID, rawURL string) (*note.Note, error)
    CaptureVoice(ctx context.Context, userID string, audio io.Reader) (*note.Note, error)
    // audio is the raw audio data stream. The handler reads multipart form
    // data and passes the file part as an io.Reader.
}
```

```go
// template.Service - note templates
type Service interface {
    List(ctx context.Context, userID string) ([]TemplateMeta, error)
    Get(ctx context.Context, userID, name string) (*Template, error)
    Apply(ctx context.Context, userID, name string, vars map[string]string) (string, error) // returns rendered body
    // Apply loads the template (checking per-user overrides first, then shared
    // defaults), substitutes variables, and returns the rendered body text.
}

type TemplateMeta struct {
    Name        string `json:"name"`
    Description string `json:"description"`
}

type Template struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Body        string `json:"body"` // raw template content with {{var}} placeholders
}
```

```go
// graph.Service - knowledge graph data
type Service interface {
    GetGraph(ctx context.Context, userID string, filter GraphFilter) (*Graph, error)
}

type GraphFilter struct {
    ProjectID string
    Tag       string
    Since     time.Time
    Until     time.Time
    Limit     int // default 500
}

type Graph struct {
    Nodes []Node `json:"nodes"`
    Edges []Edge `json:"edges"`
}

type Node struct {
    ID        string   `json:"id"`
    Title     string   `json:"title"`
    ProjectID string   `json:"project_id,omitempty"`
    Tags      []string `json:"tags"`
    CreatedAt time.Time `json:"created_at"`
}

type Edge struct {
    Source string `json:"source"` // source note ID
    Target string `json:"target"` // target note ID (empty if dangling)
}
```

### 3c. Configuration

Complete config struct with all configurable values. Loaded from `seam-server.yaml`, overridable by env vars where noted.

```yaml
# seam-server.yaml — full reference
listen: ":8080"                          # env: SEAM_LISTEN
data_dir: "/var/seam"                    # env: SEAM_DATA_DIR

jwt_secret: ""                           # env: SEAM_JWT_SECRET (required, no default)

ollama_base_url: "http://localhost:11434" # env: SEAM_OLLAMA_URL
chromadb_url: "http://localhost:8000"     # env: SEAM_CHROMADB_URL (optional, required for Phase 2+)

models:
  embeddings: "qwen3-embedding:8b"
  background: "qwen3:32b"
  chat: "qwen3:32b"
  transcription: "whisper"

auth:
  access_token_ttl: "15m"                # duration string
  refresh_token_ttl: "168h"              # 7 days
  bcrypt_cost: 12

ai:
  queue_workers: 1                       # concurrent Ollama requests
  embedding_timeout: "30s"
  chat_timeout: "120s"

userdb:
  eviction_timeout: "30m"               # close idle user DBs after this

watcher:
  debounce_interval: "200ms"
```

```go
type Config struct {
    Listen        string       `yaml:"listen"`
    DataDir       string       `yaml:"data_dir"`
    JWTSecret     string       `yaml:"jwt_secret"`
    OllamaBaseURL string       `yaml:"ollama_base_url"`
    ChromaDBURL   string       `yaml:"chromadb_url"`   // optional; required when AI features are used
    Models        ModelsConfig `yaml:"models"`
    Auth          AuthConfig   `yaml:"auth"`
    AI            AIConfig     `yaml:"ai"`
    UserDB        UserDBConfig `yaml:"userdb"`
    Watcher       WatcherConfig `yaml:"watcher"`
}

type ModelsConfig struct {
    Embeddings    string `yaml:"embeddings"`    // required
    Background    string `yaml:"background"`    // required
    Chat          string `yaml:"chat"`          // required
    Transcription string `yaml:"transcription"` // optional; required for voice capture
}

type AuthConfig struct {
    AccessTokenTTL  time.Duration `yaml:"access_token_ttl"`
    RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl"`
    BcryptCost      int           `yaml:"bcrypt_cost"`
}

type AIConfig struct {
    QueueWorkers     int           `yaml:"queue_workers"`
    EmbeddingTimeout time.Duration `yaml:"embedding_timeout"`
    ChatTimeout      time.Duration `yaml:"chat_timeout"`
}

type UserDBConfig struct {
    EvictionTimeout time.Duration `yaml:"eviction_timeout"`
}

type WatcherConfig struct {
    DebounceInterval time.Duration `yaml:"debounce_interval"`
}
```

Env var override precedence: env > YAML > defaults. Empty env vars do not override (only non-empty values take effect).

**Per-user config:** PLAN.md mentions `{user_id}/config.yaml` for per-user model overrides. This is deferred — not implemented in any phase. All users share the server-level config. When needed, add a `UserConfig` struct and a loader that merges user overrides on top of the server config.

---

## 4. Go Dependencies

```
# Core
github.com/go-chi/chi/v5          # HTTP router
github.com/go-chi/cors             # CORS middleware
modernc.org/sqlite                 # pure-Go SQLite driver
github.com/oklog/ulid/v2           # ULID generation
github.com/golang-jwt/jwt/v5       # JWT signing/verification
golang.org/x/crypto                # bcrypt
golang.org/x/net                   # HTML parsing for URL capture (net/html)
gopkg.in/yaml.v3                   # YAML config + frontmatter parsing

# File watching
github.com/fsnotify/fsnotify       # filesystem events

# WebSocket
github.com/coder/websocket         # nhooyr/websocket successor, maintained

# TUI
github.com/charmbracelet/bubbletea # TUI framework
github.com/charmbracelet/bubbles   # TUI components (text input, list, viewport)
github.com/charmbracelet/lipgloss  # TUI styling

# Testing
github.com/stretchr/testify        # assertions (assert + require)

# Logging
log/slog                           # stdlib structured logging (Go 1.21+)
```

No CGO. No external services other than Ollama and ChromaDB (both run separately).

### ChromaDB client

There is no mature Go client for ChromaDB. We write a thin HTTP client in `internal/ai/chroma.go` that wraps the ChromaDB REST API. The API surface we need is small:

```
POST   /api/v2/tenants/{tenant}/databases/{db}/collections              -- create collection
POST   /api/v2/tenants/{tenant}/databases/{db}/collections/{id}/add     -- add embeddings
POST   /api/v2/tenants/{tenant}/databases/{db}/collections/{id}/query   -- query by vector
POST   /api/v2/tenants/{tenant}/databases/{db}/collections/{id}/update  -- update embeddings
POST   /api/v2/tenants/{tenant}/databases/{db}/collections/{id}/delete  -- delete embeddings
```

This is roughly 200 lines of Go. No need for a third-party library.

---

## 5. React Frontend Dependencies

See [FE_DESIGN.md](./FE_DESIGN.md) for the full design system (colors, typography, components, screen layouts).

```json
{
  "dependencies": {
    "react": "^19",
    "react-dom": "^19",
    "react-router-dom": "^7",
    "@codemirror/lang-markdown": "^6",
    "@codemirror/state": "^6",
    "@codemirror/view": "^6",
    "codemirror": "^6",
    "@uiw/react-codemirror": "^4",
    "zustand": "^5",
    "cytoscape": "^3",
    "lucide-react": "^0.500",
    "motion": "^12",
    "markdown-it": "^14",
    "date-fns": "^4"
  },
  "devDependencies": {
    "vite": "^6",
    "typescript": "^5",
    "@types/react": "^19",
    "vitest": "^3",
    "@testing-library/react": "^16"
  }
}
```

| Package | Purpose |
|---|---|
| `lucide-react` | Icon library (Lucide, MIT, tree-shakeable 24px stroke icons) |
| `motion` | Animation orchestration (staggered lists, layout transitions, modal enter/exit) |
| `markdown-it` | Markdown rendering in preview pane, extensible for wikilinks and task lists |
| `date-fns` | Date formatting ("3 hours ago", "Mar 8, 2026"), tree-shakeable |

State management: Zustand. Lightweight, no boilerplate, plays well with WebSocket-driven state updates.

---

## 6. Implementation Phases — Detailed Task Breakdown

Each task has: what to build, what it depends on, and acceptance criteria. Tasks within a week can be parallelized across team members where noted.

---

### Phase 1 — Core (Weeks 1-3)

#### Week 1: Foundation

**1.1 — Project scaffolding**
- Init Go module, create directory structure from Section 1
- Create `Makefile` with targets: `build`, `test`, `lint`, `run`
- Create `seam-server.yaml.example` with all config fields documented
- **Depends on:** nothing
- **Done when:** `make build` produces two binaries (`seamd`, `seam`), `make test` passes (no tests yet, but the harness works)

**1.2 — Config loading** (`internal/config`)
- Parse `seam-server.yaml` with `gopkg.in/yaml.v3` into `Config` struct (see Section 3c for full definition)
- Validate required fields: `listen`, `data_dir`, `jwt_secret`, `ollama_base_url`, `models.embeddings`, `models.background`, `models.chat`
- Validate optional fields (warn if missing, needed for Phase 2+): `chromadb_url`, `models.transcription`
- Apply defaults for optional fields: `auth.access_token_ttl=15m`, `auth.refresh_token_ttl=168h`, `auth.bcrypt_cost=12`, `ai.queue_workers=1`, `ai.embedding_timeout=30s`, `ai.chat_timeout=120s`, `userdb.eviction_timeout=30m`, `watcher.debounce_interval=200ms`
- Support env var overrides: `SEAM_LISTEN`, `SEAM_DATA_DIR`, `SEAM_JWT_SECRET`, `SEAM_OLLAMA_URL`, `SEAM_CHROMADB_URL`
- Normalize paths: strip trailing slashes from `data_dir` and URLs
- Write tests: valid config, missing required fields, env override precedence, defaults applied, path normalization
- **Depends on:** 1.1
- **Done when:** tests pass, config struct is fully populated from YAML + env + defaults

**1.3 — Server.db setup** (`internal/auth/store.go`, `migrations/server/`)
- Create `server.db` at `{data_dir}/server.db` on first run
- Run migrations (embed SQL files with `go:embed`)
- Open with WAL mode, foreign keys on
- Write `auth.Store` implementing the full interface (see Section 3b): `CreateUser`, `GetUserByUsername`, `GetUserByID`, `CreateRefreshToken`, `GetRefreshToken`, `DeleteRefreshToken`, `DeleteRefreshTokensByUser`
- Write tests against an in-memory SQLite DB
- **Depends on:** 1.2
- **Done when:** can create a user, retrieve it, manage refresh tokens, migrations run idempotently

**1.4 — Per-user DB manager** (`internal/userdb`)
- Implement `Manager`: open/cache/close per-user `seam.db`
- On `Open`: create `{data_dir}/users/{user_id}/` directory tree if missing, run per-user migrations
- Cache open `*sql.DB` handles in a `sync.Map` — no need to open/close per request
- Eviction: close DBs for users who haven't made a request in 30 minutes (configurable). Timer-based cleanup goroutine.
- Write tests: open creates DB, re-open returns cached handle, close works
- **Depends on:** 1.2
- **Done when:** tests pass, can open multiple user DBs concurrently

**1.5 — HTTP server + auth middleware** (`internal/server`, `internal/auth`)
- Set up chi router with: request ID middleware, structured logging (slog), CORS, panic recovery
- `auth.Service`: business logic layer (see Section 3b for interface). Coordinates `auth.Store`, `userdb.Manager`, bcrypt hashing, JWT generation. On register: validate input, hash password, create user in `server.db`, create user data dir via `userdb.Manager`, return AuthResponse (user info + token pair). On login: verify credentials, return AuthResponse. On refresh: verify refresh token hash, issue new access token. On logout: delete the refresh token (revoke session).
- `auth.Handler`: HTTP handlers that delegate to `auth.Service`
  - `POST /api/auth/register`: validate input, call service, return 201 with AuthResponse
  - `POST /api/auth/login`: call service, return 200 with AuthResponse
  - `POST /api/auth/refresh`: call service, return 200 with new token pair
  - `POST /api/auth/logout`: revoke refresh token, return 204
- JWT middleware: extract user ID from token, inject into `context.Context`, reject expired tokens
- Access token TTL and refresh token TTL are read from config (see Section 3c). Defaults: 15 minutes / 7 days.
- Write tests: register, login, invalid credentials, expired token, refresh flow, directory creation
- **Depends on:** 1.3, 1.4
- **Done when:** can register a user, log in, and make an authenticated request

#### Week 2: Note CRUD + Projects

**2.1 — Frontmatter parser** (`internal/note/frontmatter.go`)
- Parse YAML frontmatter delimited by `---` markers
- Serialize frontmatter back to YAML (for writes)
- Handle edge cases: no frontmatter, empty frontmatter, frontmatter with unknown fields (preserve them)
- Write tests: round-trip parse/serialize, missing fields, malformed YAML
- **Depends on:** 1.1
- **Done when:** tests pass, frontmatter parse/serialize is lossless for known fields

**2.2 — Wikilink parser + resolution** (`internal/note/wikilink.go`)
- Regex: `\[\[([^\]]+)\]\]` — extract all wikilinks from note body
- Handle display aliases: `[[target|display text]]` — extract both parts
- Ignore wikilinks inside code blocks (fenced ``` and inline `)
- **Resolution logic** (used by `note.Store.UpdateLinks` to populate `target_note_id`):
  1. Exact match on note title (case-insensitive)
  2. Exact match on filename without `.md` extension (case-insensitive)
  3. Exact match on slug (lowercase, hyphenated form of the title)
  4. If no match: dangling link (`target_note_id = NULL`, `link_text` preserved for future resolution)
- **Dangling link resolution**: when a new note is created, query `links WHERE target_note_id IS NULL` and attempt to resolve each against the new note's title/filename. Update matched links to point to the new note.
- Write tests: basic link, aliased link, multiple links, links in code blocks (should be ignored), resolution by title, resolution by filename, dangling link creation, dangling link resolution on new note
- **Depends on:** 1.1
- **Done when:** tests pass, wikilinks are parsed and resolved correctly

**2.3 — Tag parser** (`internal/note/tag.go`)
- Parse inline `#tag` from note body (regex: `(?:^|\s)#([a-zA-Z0-9_-]+)`)
- Merge with tags from YAML frontmatter `tags: [...]` field
- Ignore `#` inside code blocks, URLs, and headings (`## Heading` is not a tag)
- Write tests: inline tags, frontmatter tags, merged result, false positives
- **Depends on:** 2.1
- **Done when:** tests pass

**2.4 — Project CRUD** (`internal/project`)
- `project.Store`: Create, Get, List, Update, Delete against per-user `seam.db`
- `project.Service`: business logic + create/delete project directory on filesystem
- `project.Handler`: REST handlers for `/api/projects` endpoints
- Slug generation from project name (lowercase, hyphens, dedup)
- On delete: option to move notes to Inbox or delete them (query param)
- Write tests: CRUD operations, slug generation, filesystem directory creation
- **Depends on:** 1.4, 1.5
- **Done when:** full CRUD via REST API works, directory created/deleted on filesystem

**2.5 — Note CRUD** (`internal/note`)
- `note.Store`: Create, Get, GetByFilePath, List, Update, Delete in `seam.db` (see interfaces in Section 3)
- `note.Service`: coordinates file I/O + SQLite. Accepts an optional `WriteSuppressor` interface (nil until the watcher is wired in task 3.1; when non-nil, calls `IgnoreNext(filePath)` before every file write to suppress redundant watcher events). On create: generate ULID, write `.md` file with frontmatter, insert into SQLite (including `body` column for FTS trigger sync), parse wikilinks + tags, update links table, resolve dangling links pointing to the new note's title. On update: same but overwrite file; if `project_id` changes, move the file to the new project directory. On delete: remove file + all DB rows.
- FTS index is maintained automatically by SQLite triggers on the `notes` table. The service ensures `notes.body` is set correctly on create/update, and the triggers handle FTS sync.
- `note.Handler`: REST handlers for `/api/notes` endpoints
- List endpoint supports filters: `?project=`, `?tag=`, `?since=`, `?until=`, `?sort=created|modified`, `?limit=` (default 100, max 500), `?offset=` (for pagination). Response includes `X-Total-Count` header for total matching notes.
- Backlinks endpoint: `GET /api/notes/{id}/backlinks` — query links table for `target_note_id = {id}`
- Write tests: full CRUD lifecycle, filter combinations, pagination, backlinks, wikilink/tag indexing, project move
- **Depends on:** 1.4, 1.5, 2.1, 2.2, 2.3, 2.4
- **Done when:** can create a note via API, read it back with parsed metadata, search by tag, see backlinks, paginate results

**2.6 — Full-text search** (`internal/search/fts.go`)
- FTS index is maintained automatically by triggers on the `notes` table (insert/update/delete). No explicit FTS write code needed in the search package.
- `GET /api/search?q=...`: query FTS5 with `MATCH`, return results ranked by `bm25()`. Use `highlight()` and `snippet()` for result display (available because FTS is external-content, not contentless).
- Sanitize user input: escape FTS5 operators (`AND`, `OR`, `NOT`, `*`, `"`, parentheses) to prevent query syntax errors. Treat all user input as literal search terms by default.
- Support prefix queries (e.g., `cach*` matches "caching")
- Write tests: search by word, ranking order, prefix search, FTS auto-sync on note create/update/delete, special character handling, SQL injection resistance
- **Depends on:** 2.5
- **Done when:** can search notes by content, results are ranked, FTS stays in sync automatically

**2.7 — Tags endpoint** (`internal/note/handler.go`)
- `GET /api/tags` — return all tags for the user with note counts: `[{"name": "architecture", "count": 5}, ...]`
- Query: `SELECT t.name, COUNT(*) FROM tags t JOIN note_tags nt ON t.id = nt.tag_id GROUP BY t.id ORDER BY COUNT(*) DESC`
- Write tests: empty tags, multiple tags with counts, tags from deleted notes are excluded
- **Depends on:** 2.5
- **Done when:** API returns correct tag list with counts

#### Week 3: File Watching, WebSocket, Client Scaffolding

**3.1 — File watcher** (`internal/watcher`)
- Use `fsnotify` to watch each user's `notes/` directory (recursive — watch subdirectories too)
- On file create/modify: debounce (configurable, see `watcher.debounce_interval`), then invoke the `FileEventHandler` callback (see Section 3b). The watcher does NOT import `note` — the callback is injected at startup by `internal/server`, pointing to `note.Service.Reindex`.
- On file delete: the `FileEventHandler` callback detects the file is gone and removes the note from DB (including FTS via trigger cascade)
- On directory create (new project dir added externally): start watching it
- Handle rename events (fsnotify delivers delete + create)
- **Self-write suppression:** maintain a set of recently-written paths (populated by `IgnoreNext()`). When an event arrives for a suppressed path, skip it and remove from the set. Entries expire after 2 seconds to avoid leaks if the filesystem event never fires. The `note.Service` calls watcher via the `WriteSuppressor` interface (see Section 3b) — no circular import.
- Start watcher BEFORE reconciliation to avoid missing changes that occur during the scan
- Write tests: create a temp dir, write a file, verify callback is triggered; test self-write suppression
- **Depends on:** 1.4 (userdb for directory paths), 3.3 (ws for pushing events)
- **Done when:** editing a `.md` file externally triggers the file event callback, verified via test

**3.2 — Startup reconciliation** (`internal/watcher/reconcile.go`)
- On server start, for each user: scan `notes/` directory, reconcile with SQLite
- Two-pass change detection for efficiency:
  1. **Fast pass (mtime):** compare file mtime against `notes.updated_at`. Skip files where mtime has not changed (avoids reading file content).
  2. **Confirm pass (content_hash):** for files where mtime changed, read the file, compute SHA-256, compare against `notes.content_hash`. Only re-index if the hash differs. This handles cases where mtime changed but content did not (e.g., file copy, editor save without changes).
- New files on disk but not in DB: index them (parse frontmatter, assign ULID if missing, insert)
- Files in DB but not on disk: delete from DB
- Write tests: simulate stale DB, verify reconciliation fixes it
- **Depends on:** 3.1
- **Done when:** server can recover from being offline while files were edited

**3.3 — WebSocket hub** (`internal/ws`)
- Hub manages a map of `userID -> []*websocket.Conn`
- `Register`/`Unregister` on connect/disconnect
- `Send(userID, msg)` — send to all connections for a user
- Message protocol: JSON `{"type": "...", "payload": {...}}`
- Auth: client sends JWT as first message after connect, hub validates and registers
- Ping/pong keepalive (30s interval)
- Write tests: mock connections, verify message delivery, auth rejection
- **Depends on:** 1.5
- **Done when:** can connect via WebSocket, authenticate, receive pushed messages

**3.4 — Wire file watcher to WebSocket**
- When watcher detects a change and reindexes, push `note.changed` event to the user's WebSocket connections
- Include note ID, change type (created/modified/deleted) in the event payload
- **Depends on:** 3.1, 3.3
- **Done when:** editing a file externally pushes a WebSocket event to connected clients

**3.5 — TUI scaffold** (`cmd/seam`)
- Bubble Tea app with screen routing
- Login screen: server URL, username, password. Store token in `~/.config/seam/auth.json`
- Main screen: project list (left pane), note list (right pane)
- Navigation: `j`/`k` or arrow keys to navigate, `Enter` to open, `/` to search, `q` to quit
- Quick capture: `c` to open capture input, type note, `Ctrl+S` to save to Inbox
- **Depends on:** 1.5, 2.4, 2.5 (needs working API)
- **Done when:** can log in, see projects, navigate notes, create a quick note

**3.6 — TUI note editor**
- Full-screen text area (Bubble Tea `textarea` component from `bubbles`)
- Load note content from API, edit, save back via `PUT /api/notes/{id}`
- Markdown syntax highlighting (basic: headers, bold, italic, links, code)
- `Ctrl+S` to save, `Esc` to exit editor
- **Depends on:** 3.5
- **Done when:** can open a note, edit it, save, and see the change reflected

**3.7 — TUI search**
- `/` opens search input
- Debounced search-as-you-type against `GET /api/search?q=...`
- Results list with title and snippet
- `Enter` to open a result in the editor
- **Depends on:** 3.5, 2.6
- **Done when:** can search and open results

**3.8 — React app scaffold** (`web/`)
- Vite + React + TypeScript project init
- CSS architecture: `variables.css` (design tokens from FE_DESIGN.md), `reset.css`, `global.css`, `fonts.css`, CSS Modules per component
- Google Fonts: Fraunces (display), Outfit (UI), Lora (content), IBM Plex Mono (code)
- Zustand store for auth state, notes, projects
- React Router: `/login`, `/`, `/notes/{id}`, `/projects/{id}`, `/search`, `/ask`, `/graph`, `/timeline`
- API client module with JWT interceptor (attach token, handle 401 -> redirect to login)
- WebSocket client with auto-reconnect
- Login page: register + login forms (centered card on topographic background per FE_DESIGN.md Section 7.1)
- **Depends on:** 1.5 (needs working auth API)
- **Done when:** can register, log in, token is stored, WebSocket connects, design tokens render correctly

**3.9 — React sidebar + project view**
- Sidebar component per FE_DESIGN.md Section 6.1: wordmark, search, inbox, projects, tags, user row, capture button with pulse animation
- Sidebar collapse/expand at `<1024px` breakpoints
- Command palette (`Cmd+K`/`Ctrl+K`) per FE_DESIGN.md Section 6.7
- Project view per FE_DESIGN.md Section 7.2: header with title/description, sort controls, note card list
- Note cards per FE_DESIGN.md Section 6.2: title, preview, tags as colored pills, timestamp, hover seam-line
- Click note to navigate to `/notes/{id}`
- Empty states with topographic contour background per FE_DESIGN.md Section 6.10
- **Depends on:** 3.8, 2.4, 2.5
- **Done when:** can see projects, click into them, see notes, sidebar collapses on narrow viewports

**3.10 — React note editor**
- CodeMirror 6 with custom dark theme per FE_DESIGN.md Section 7.3a (amber cursor, warm syntax colors)
- Split view per FE_DESIGN.md Section 7.3: editor (left, font-mono) + preview (right, font-content, max 720px)
- Toolbar: formatting buttons (ghost icon buttons), view mode toggle (editor/split/preview), right panel toggle
- Markdown preview rendering via `markdown-it` with wikilink plugin
- Load note content from API on mount
- Auto-save on debounced change (1 second after last keystroke), "Saving..."/"Saved" indicator
- `[[wikilink]]` autocomplete: on typing `[[`, fetch note titles from API, show dropdown
- Wikilink decoration in editor per FE_DESIGN.md Section 6.9 (amber text, dotted underline, dimmed delimiters)
- Right panel per FE_DESIGN.md Section 7.3: backlinks, tags, metadata sections
- **Depends on:** 3.8, 2.5
- **Done when:** can edit a note with live preview, wikilinks autocomplete, see backlinks, custom theme renders

**3.11 — React quick capture**
- Modal overlay per FE_DESIGN.md Section 7.8: backdrop blur, 420px width, enter/exit animations
- Triggered by sidebar capture button, `Cmd+N`/`Ctrl+N`, or command palette
- Title input (optional) + body textarea (auto-focused), optional project dropdown and tag input
- Save to Inbox by default, `Cmd+Enter`/`Ctrl+Enter` to save
- Dismiss after save, confirm on Escape if content entered
- **Depends on:** 3.8, 2.5
- **Done when:** can capture a note quickly without leaving current view

---

### Phase 2 — Intelligence (Weeks 4-6)

#### Week 4: Embeddings + Semantic Search

**4.1 — Ollama HTTP client** (`internal/ai/ollama.go`)
- Thin wrapper around Ollama REST API
- `GenerateEmbedding(model, text) -> []float64`
- `ChatCompletion(model, messages, stream bool) -> stream of tokens or full response`
- Handle Ollama errors: model not found, server down, timeout
- Configurable timeout (embeddings: 30s, chat: 120s)
- Write tests with HTTP mock server
- **Depends on:** 1.2
- **Done when:** can generate an embedding and get a chat response from Ollama

**4.2 — ChromaDB HTTP client** (`internal/ai/chroma.go`)
- Thin wrapper around ChromaDB REST API (see Section 4 in project structure)
- `CreateCollection(name)`, `AddDocuments(collection, ids, embeddings, metadatas)`, `Query(collection, embedding, nResults)`, `UpdateDocuments(...)`, `DeleteDocuments(...)`
- Per-user collection naming: `user_{user_id}_notes`
- Write tests with HTTP mock server
- **Depends on:** 1.1
- **Done when:** can create a collection, add vectors, query nearest neighbors

**4.3 — AI task queue** (`internal/ai/queue.go`)
- In-memory priority queue backed by Go channels
- Three priority levels: interactive (0), user-triggered (1), background (2)
- Fair scheduling: within each priority, round-robin across users
- Workers: configurable concurrency (default: 1 for Ollama, meaning serial execution with priority ordering)
- On startup: load pending/running tasks from all user DBs, re-queue them
- On task complete/fail: update `ai_tasks` row in user's `seam.db`
- Push status updates via WebSocket (`task.progress`, `task.complete`)
- Write tests: enqueue tasks at different priorities, verify execution order, verify fair scheduling
- **Depends on:** 1.4, 3.3
- **Done when:** tasks execute in priority order, round-robin within priority, status pushed via WebSocket

**4.4 — Embedding pipeline** (`internal/ai/embedder.go`)
- On note create/update: enqueue a background `embed` task
- Task handler: read note content, call Ollama embedding API, upsert into ChromaDB
- Chunk long notes: split into ~512 token chunks with overlap. Store one embedding per chunk, all keyed by note ULID with chunk index suffix.
- On note delete: remove all embeddings for that note from ChromaDB
- Batch processing: on user's first login or on `POST /api/admin/reindex-embeddings`, enqueue all notes
- Write tests: mock Ollama + ChromaDB, verify embedding is stored, chunking works
- **Depends on:** 4.1, 4.2, 4.3
- **Done when:** saving a note triggers embedding generation, embeddings land in ChromaDB

**4.5 — Semantic search** (`internal/search/semantic.go`)
- `GET /api/search/semantic?q=...`: embed the query text with Ollama, query ChromaDB for nearest neighbors, return notes ranked by similarity
- Deduplicate: if multiple chunks from the same note match, take the best score
- Return results with similarity score, note metadata, and a content snippet around the matching chunk
- Write tests: mock embedding + ChromaDB, verify ranked results
- **Depends on:** 4.1, 4.2, 4.4
- **Done when:** can ask a natural language question and get relevant notes back

**4.6 — Related notes panel**
- `GET /api/notes/{id}/related`: embed the current note, query ChromaDB excluding itself, return top 5 most similar notes
- Wire into React editor: show related notes panel alongside backlinks
- Wire into TUI: `Tab` to switch to related notes panel
- **Depends on:** 4.5
- **Done when:** related notes appear when viewing a note

#### Week 5: Ask Seam + Synthesis

**4.7 — Ask Seam — RAG chat** (`internal/ai/chat.go`)
- Client sends question via WebSocket: `{"type": "chat.ask", "payload": {"query": "..."}}`
- Backend: embed the query, retrieve top-K relevant chunks from ChromaDB, construct prompt with chunks as context, stream response from Ollama
- Prompt template: system message explaining the assistant only answers from the user's notes, then retrieved chunks with note titles as citations, then user question
- Stream tokens back via WebSocket: `{"type": "chat.stream", "payload": {"token": "..."}}`
- Final message: `{"type": "chat.done", "payload": {"citations": ["note_id_1", "note_id_2"]}}`
- Conversation memory: keep last N turns (configurable, default 5) in the message history for follow-up questions
- Interactive priority in task queue
- Write tests: mock Ollama, verify prompt construction includes retrieved context, verify streaming
- **Depends on:** 4.1, 4.5, 3.3
- **Done when:** can ask a question in the chat, get a streaming answer grounded in notes, with citations

**4.8 — AI synthesis** (`internal/ai/synthesizer.go`)
- `POST /api/synthesize` with body `{"scope": "project", "project_id": "...", "prompt": "summarize"}` or `{"scope": "tag", "tag": "architecture", "prompt": "what are the key decisions?"}`
- Backend: retrieve all notes matching the scope, chunk and prioritize by relevance to the prompt, construct a synthesis prompt, stream response
- User-triggered priority in task queue
- Return streaming response via WebSocket
- Write tests: mock Ollama, verify scoped note retrieval, prompt construction
- **Depends on:** 4.1, 4.3, 2.5
- **Done when:** can synthesize across a project or tag scope with a streaming response

**4.9 — Auto-link suggestions** (`internal/ai/linker.go`)
- On note save: enqueue a background `autolink` task
- Task handler: read the saved note, retrieve semantically similar notes (top 10), prompt the LLM: "Given this note and these related notes, suggest which notes should be linked and why"
- Return suggestions as a list: `[{target_note_id, target_title, reason}]`
- Push suggestions via WebSocket: `{"type": "note.link_suggestions", "payload": {...}}`
- UI shows suggestions; user can accept (inserts `[[wikilink]]`) or dismiss
- Background priority in task queue
- **Depends on:** 4.1, 4.4, 4.3
- **Done when:** saving a note triggers link suggestions pushed to the client

#### Week 6: Client integration for Phase 2 features

**4.10 — React: semantic search UI**
- Add toggle in search bar: "Full-text" / "Semantic"
- Semantic mode calls `GET /api/search/semantic?q=...`
- Results show similarity score and content snippet
- **Depends on:** 4.5, 3.8

**4.11 — React: Ask Seam page**
- Chat interface at `/ask`
- Message input, streaming response display (token by token via WebSocket)
- Citation links: click to open the cited note
- Conversation history displayed as message bubbles
- **Depends on:** 4.7, 3.8

**4.12 — React: synthesis UI**
- Button in project view: "Summarize this project"
- Modal shows streaming synthesis response
- Also available from tag view: "Summarize notes tagged #X"
- **Depends on:** 4.8, 3.9

**4.13 — React: auto-link suggestion UI**
- After saving a note, if suggestions arrive via WebSocket, show a dismissible panel
- Each suggestion: target note title, reason, "Link" button
- "Link" inserts `[[target title]]` at the cursor position or end of note
- **Depends on:** 4.9, 3.10

**4.14 — TUI: semantic search**
- `/` search with prefix `?` for semantic search (e.g., `?caching strategies`)
- Results show similarity score
- **Depends on:** 4.5, 3.7

**4.15 — TUI: Ask Seam**
- New screen: `a` from main screen to open Ask Seam
- Chat-style input/output, streaming display
- **Depends on:** 4.7, 3.5

---

### Phase 3 — Rich Capture (Weeks 7-8)

#### Week 7: Voice + URL Capture

**5.1 — URL capture** (`internal/capture/url.go`)
- `POST /api/capture` with `{"type": "url", "url": "https://..."}`
- Fetch the page, extract `<title>`, extract main content text (use `golang.org/x/net/html` for parsing — take `<article>` or `<main>` or `<body>` text)
- Create a note in Inbox with: title from `<title>`, body is extracted text, `source_url` in frontmatter
- Write tests: mock HTTP server, verify title extraction, note creation
- **Depends on:** 2.5
- **Done when:** pasting a URL creates a properly formatted note with source attribution

**5.2 — Voice capture** (`internal/capture/voice.go`)
- `POST /api/capture` with `{"type": "voice", "audio": <base64 or multipart file>}`
- Send audio to Ollama Whisper endpoint for transcription
- Create a note in Inbox with transcription as body, `transcript_source: true` in frontmatter
- Optionally: enqueue a background task to summarize the transcription and prepend a summary section
- Write tests: mock Whisper API, verify transcription flow
- **Depends on:** 4.1, 2.5
- **Done when:** uploading an audio file creates a transcribed note

**5.3 — React + TUI: capture integration**
- React: URL paste detection in quick-capture modal (if content starts with `http`, trigger URL capture)
- React: audio recording button in quick-capture modal (use MediaRecorder API)
- TUI: `u` for URL capture (prompts for URL), `v` for voice capture (records from mic via system command)
- **Depends on:** 5.1, 5.2, 3.11, 3.5

#### Week 8: Templates + AI Writing Assist

**5.4 — Templates system** (`internal/template`)
- Templates are `.md` files stored in `{data_dir}/templates/` (shared) and `{user_dir}/templates/` (per-user overrides)
- Default templates: project kick-off, meeting notes, research summary, daily log
- `GET /api/templates` — list available templates
- `POST /api/notes` with `{"template": "meeting-notes", ...}` — create note from template, replacing `{{date}}`, `{{project}}` variables
- Write tests: template loading, variable substitution
- **Depends on:** 2.5
- **Done when:** can create a note from a template via API

**5.5 — AI writing assist** (`internal/ai/writer.go`)
- `POST /api/notes/{id}/ai-assist` with `{"action": "expand|summarize|extract-actions", "selection": "..."}`
- `expand`: take selected bullet points, prompt LLM to expand into paragraphs
- `summarize`: take selected text (or full note), prompt LLM to summarize
- `extract-actions`: prompt LLM to extract action items as a checklist
- Return result as text (not streaming — these are short operations)
- User-triggered priority in task queue
- Write tests: mock Ollama, verify prompt construction for each action
- **Depends on:** 4.1, 4.3
- **Done when:** all three actions return useful transformations

**5.6 — React + TUI: templates and AI assist**
- React: template picker in "New Note" flow
- React: right-click or toolbar button for AI assist actions on selected text
- TUI: template selection when creating a new note (`n` -> template picker)
- TUI: AI assist via command palette (`:expand`, `:summarize`, `:actions`)
- **Depends on:** 5.4, 5.5, 3.10, 3.6

---

### Phase 4 — Visualization (Weeks 9-10)

#### Week 9: Knowledge Graph

**6.1 — Graph data endpoint** (`internal/graph`)
- `GET /api/graph` — return all nodes and edges for the user
- Nodes: `{id, title, project, tags, created_at}`
- Edges: from `links` table, `{source, target}`
- Support filters: `?project=`, `?tag=`, `?since=`, `?until=`
- For large graphs: pagination or limit with `?limit=` (default 500 nodes)
- Write tests: verify node/edge generation, filtering
- **Depends on:** 2.5
- **Done when:** API returns correct graph data matching the link graph

**6.2 — React: knowledge graph view**
- Cytoscape.js canvas at `/graph` per FE_DESIGN.md Section 7.6
- Background: `--bg-deep` with subtle dot grid pattern (blueprint paper motif)
- Nodes: pill-shaped, project color at 15% fill / 60% border, sized by link count
- Edges: curved, `--border-default` color, highlight to `--accent-primary` on hover
- Filter panel: floating card (top-left), project checkboxes, tag pills, date range
- Minimap: bottom-right, 120x80px
- Click to select node, double-click to open note
- Zoom, pan, drag nodes
- Layout: `fcose` (fast compound spring embedder), cluster by project
- **Depends on:** 6.1, 3.8
- **Done when:** interactive graph renders, clicking opens notes, filters work, dot grid background visible

#### Week 10: Timeline + Polish

**6.3 — React: timeline view**
- Calendar-style view at `/timeline`
- Group notes by creation date (or modified date, toggleable)
- Click a date to see notes from that day
- Click a note to open it
- **Depends on:** 2.5, 3.8

**6.4 — TUI: timeline view**
- Date-grouped list view
- Navigate by date with `[` / `]` for previous/next day
- **Depends on:** 2.5, 3.5

**6.5 — Backlinks panel refinement**
- Show two-hop backlinks ("notes that link to notes that link to this one")
- Show orphan detection: notes with no incoming or outgoing links
- **Depends on:** 2.5

**6.6 — End-to-end testing and polish**
- Full user journey test: register, create project, create notes with wikilinks, search, ask Seam, view graph
- Performance testing: 1000 notes per user, 3 concurrent users
- Fix any issues found
- **Depends on:** everything

---

## 7. Testing Strategy

### Unit tests

Every package has `_test.go` files. Use `testify/require` for assertions (fail fast, not `assert`).

**SQLite tests:** Use in-memory databases for speed. Each test function gets a fresh, isolated DB with migrations applied. Use a unique DB name per test to avoid shared state when tests run in parallel.

```go
func testDB(t *testing.T) *sql.DB {
    t.Helper()
    // Use a unique name per test to ensure isolation with parallel tests.
    // "file:{name}?mode=memory&cache=shared" creates a named in-memory DB
    // that can have multiple connections (needed for WAL mode) but is unique
    // to this test.
    name := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
    db, err := sql.Open("sqlite", name)
    require.NoError(t, err)
    t.Cleanup(func() { db.Close() })
    runMigrations(db)
    return db
}
```

**Important:** Do NOT use `file::memory:?cache=shared` (unnamed shared-cache) because all tests using that URI share the same database, causing data collisions when tests run in parallel.

**HTTP handler tests:** Use `httptest.NewRecorder()` + `chi.NewRouter()`. Test the handler function directly, not the full server. Mock the service layer with interfaces.

**External service tests (Ollama, ChromaDB):** Use `httptest.NewServer()` to mock their HTTP APIs. Never call real external services in unit tests.

### Integration tests

Build tag `//go:build integration` for tests that require:
- Real filesystem (file watcher tests)
- Real SQLite on disk (not in-memory)
- A running Ollama instance (optional, skip if not available)
- A running ChromaDB instance (optional, skip if not available)

Run with: `make test-integration`

### Frontend tests

- Vitest for React component tests
- React Testing Library for UI behavior
- Mock the API client for unit tests

### Target

Every non-trivial function has at least one test. Code coverage is not a metric we chase, but critical paths (auth, note CRUD, search, file reconciliation) must have thorough coverage.

---

## 8. Development Setup

### Prerequisites

- Go 1.24+
- Node.js 22+ (for React frontend)
- Ollama running locally (for Phase 2+)
- ChromaDB running in server mode (for Phase 2+)

### First run

```bash
git clone <repo>
cd seam

# Build
make build

# Create config
cp seam-server.yaml.example seam-server.yaml
# Edit seam-server.yaml: set data_dir to a local path (e.g., ./data)

# Run server
make run

# In another terminal: run TUI
./bin/seam --server http://localhost:8080

# In another terminal: run React dev server
cd web && npm install && npm run dev
```

### Makefile targets

```makefile
build:            # build seamd + seam binaries to ./bin/
run:              # build and run seamd
test:             # run unit tests
test-integration: # run integration tests
lint:             # run golangci-lint + eslint
fmt:              # run gofmt + prettier
dev-web:          # run React dev server with hot reload
clean:            # remove build artifacts
```

---

## 9. Error Handling and Logging

### Errors

- Domain errors are typed: `note.ErrNotFound`, `auth.ErrInvalidCredentials`, `auth.ErrUserExists`
- Handlers map domain errors to HTTP status codes: `ErrNotFound` -> 404, `ErrInvalidCredentials` -> 401
- Unknown errors -> 500, logged with full context, sanitized response to client
- All errors include context: wrap with `fmt.Errorf("note.Service.Create: %w", err)`

### Logging

- Use `log/slog` (stdlib). Structured JSON in production, text in development.
- Log levels: `DEBUG` for per-request detail, `INFO` for lifecycle events (server start, user login, watcher events), `WARN` for recoverable issues (Ollama timeout, stale file), `ERROR` for failures.
- Every request gets a request ID (middleware), included in all log entries for that request.
- Avoid logging note content or user data beyond IDs.

---

## 10. Graceful Shutdown

On `SIGINT`/`SIGTERM`:

1. Stop accepting new HTTP connections (server.Shutdown with 10s timeout)
2. Close all WebSocket connections (send close frame)
3. Stop file watchers
4. Drain AI task queue (finish running task, discard pending — they are persisted in SQLite and will be reloaded on next start)
5. Close all per-user SQLite databases
6. Close server.db
7. Exit

Implemented in `cmd/seamd/main.go` using `signal.NotifyContext`.

---

## 11. Security Considerations

- **Path traversal:** Note file paths must be validated to stay within the user's `notes/` directory. Reject any path containing `..` or absolute paths.
- **User isolation:** Every API handler resolves the user ID from the JWT and passes it to the service layer. The service layer uses `userdb.Manager.Open(ctx, userID)` to get the correct database. There is no way to pass a different user's ID.
- **Input validation:** All API inputs validated at the handler level before reaching the service. Note titles, project names, tags: sanitize for filesystem safety (no `/`, `\`, `..`, null bytes).
- **JWT secrets:** Stored in config file, not hardcoded. Rotate by changing config and restarting (existing tokens invalidated).
- **bcrypt cost:** 12 (default, configurable). Sufficient for 2-5 users, not a bottleneck.
- **Rate limiting:** Not needed for MVP (2-5 trusted users on local network). Add if exposed to wider network later.

---

*Last updated: 2026-03-08*
