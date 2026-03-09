# Seam — Test Plan

Reference: [PLAN.md](./PLAN.md) for architecture, [IMP_PLAN.md](./IMP_PLAN.md) for implementation details.

TDD workflow: write the test first, watch it fail, implement the minimum code to pass, refactor. Every test listed here should exist as a Go test function (or Vitest test for frontend) before the corresponding implementation is written.

---

## Table of Contents

1. [Test Infrastructure](#1-test-infrastructure)
2. [internal/config](#2-internalconfig)
3. [internal/auth](#3-internalauth)
4. [internal/userdb](#4-internaluserdb)
5. [internal/note/frontmatter](#5-internalnotefrontmatter)
6. [internal/note/wikilink](#6-internalnotewikilink)
7. [internal/note/tag](#7-internalnotetag)
8. [internal/project](#8-internalproject)
9. [internal/note (store + service)](#9-internalnote-store--service)
10. [internal/note (handler)](#10-internalnote-handler)
11. [internal/search/fts](#11-internalsearchfts)
12. [internal/watcher](#12-internalwatcher)
13. [internal/ws](#13-internalws)
14. [internal/ai/ollama](#14-internalaiollama)
15. [internal/ai/chroma](#15-internalaichroma)
16. [internal/ai/queue](#16-internalaiqueue)
17. [internal/ai/embedder](#17-internalaiembedder)
18. [internal/search/semantic](#18-internalsearchsemantic)
19. [internal/ai/chat](#19-internalaichat)
20. [internal/ai/synthesizer](#20-internalaissynthesizer)
21. [internal/ai/linker](#21-internalailinker)
22. [internal/capture](#22-internalcapture)
23. [internal/template](#23-internaltemplate)
24. [internal/graph](#24-internalgraph)
25. [Security Tests](#25-security-tests)
26. [Concurrency Tests](#26-concurrency-tests)
27. [Integration Tests](#27-integration-tests)
28. [Performance Tests](#28-performance-tests)
29. [Frontend Tests](#29-frontend-tests)

---

## 1. Test Infrastructure

### Test helpers (shared across all packages)

These are created first, before any test. They live in `internal/testutil/`.

```go
// testutil.go - shared test setup functions

// TestServerDB creates an in-memory server.db with migrations applied.
// Returns *sql.DB and auto-closes via t.Cleanup.
// Uses a unique DB name per test to ensure isolation in parallel runs.
func TestServerDB(t *testing.T) *sql.DB

// TestUserDB creates an in-memory per-user seam.db with migrations applied.
// Returns *sql.DB and auto-closes via t.Cleanup.
// Uses a unique DB name per test to ensure isolation in parallel runs.
func TestUserDB(t *testing.T) *sql.DB

// TestDataDir creates a temp directory with the full seam storage layout
// (users/, templates/, etc.) and removes it on cleanup.
// Returns the path.
func TestDataDir(t *testing.T) string

// TestUserDir creates a user directory within a test data dir,
// including notes/inbox/. Returns the user notes path.
func TestUserDir(t *testing.T, dataDir, userID string) string

// TestNoteFile writes a .md file with the given frontmatter and body
// to the specified path. Returns the full file path.
func TestNoteFile(t *testing.T, dir, filename, frontmatter, body string) string

// TestConfig returns a Config struct populated with test-appropriate
// defaults pointing at the given data dir.
func TestConfig(t *testing.T, dataDir string) *config.Config
```

### Build tags

- Default (`go test ./...`): unit tests only. No filesystem, no external services.
- `//go:build integration`: tests requiring real filesystem, real SQLite on disk.
- `//go:build external`: tests requiring running Ollama and/or ChromaDB instances.

### Conventions

- Every test function name follows `Test{Unit}_{Scenario}_{Expected}` pattern.
- Table-driven tests for functions with many input variations.
- Use `require` (not `assert`) — fail fast on first assertion failure.
- Each test is independent. No shared state between test functions.
- No `time.Sleep` for synchronization — use channels, `sync.WaitGroup`, or `testing.T` deadlines.

---

## 2. internal/config

File: `internal/config/config_test.go`

### Config loading

| Test | Input | Expected |
|---|---|---|
| `TestLoad_ValidConfig` | Complete YAML with all fields | Config struct fully populated, no error |
| `TestLoad_MinimalConfig` | Only required fields (listen, data_dir, jwt_secret, ollama_base_url, models.embeddings/background/chat) | Config populated, optional fields have zero values |
| `TestLoad_FileNotFound` | Path to non-existent file | Descriptive error mentioning the file path |
| `TestLoad_InvalidYAML` | Malformed YAML (bad indentation, unclosed quotes) | Parse error, not a panic |
| `TestLoad_EmptyFile` | Empty file | Error: missing required fields |

### Required field validation

| Test | Input | Expected |
|---|---|---|
| `TestValidate_MissingListen` | Config with empty `listen` | Error: "listen address is required" |
| `TestValidate_MissingDataDir` | Config with empty `data_dir` | Error: "data_dir is required" |
| `TestValidate_MissingOllamaURL` | Config with empty `ollama_base_url` | Error: "ollama_base_url is required" |
| `TestValidate_MissingEmbeddingsModel` | Config with empty `models.embeddings` | Error: "models.embeddings is required" |
| `TestValidate_MissingChatModel` | Config with empty `models.chat` | Error: "models.chat is required" |
| `TestValidate_MissingBackgroundModel` | Config with empty `models.background` | Error: "models.background is required" |
| `TestValidate_InvalidListenAddress` | `listen: "not a valid address"` | Error about invalid address format |
| `TestValidate_MissingJWTSecret` | Config with empty `jwt_secret` | Error: "jwt_secret is required" |
| `TestValidate_InvalidOllamaURL` | `ollama_base_url: "not-a-url"` | Error about invalid URL format |
| `TestValidate_OptionalChromaDBURL` | Config without `chromadb_url` | No error (optional for Phase 1, warn in logs) |
| `TestValidate_OptionalTranscriptionModel` | Config without `models.transcription` | No error (optional, needed for voice capture) |

### Environment variable overrides

| Test | Input | Expected |
|---|---|---|
| `TestLoad_EnvOverrideListen` | YAML has `listen: :8080`, env `SEAM_LISTEN=:9090` | Config.Listen == ":9090" |
| `TestLoad_EnvOverrideDataDir` | YAML has `data_dir: /a`, env `SEAM_DATA_DIR=/b` | Config.DataDir == "/b" |
| `TestLoad_EnvOverrideOllamaURL` | YAML has URL A, env `SEAM_OLLAMA_URL=B` | Config.OllamaBaseURL == "B" |
| `TestLoad_EnvDoesNotOverrideWhenEmpty` | YAML has values, env vars are empty strings | YAML values used (empty string env var does not override) |
| `TestLoad_EnvOverridesPrecedence` | YAML + env both set | Env wins |

### Edge cases

| Test | Input | Expected |
|---|---|---|
| `TestLoad_ExtraFieldsIgnored` | YAML with unknown fields (e.g., `foo: bar`) | No error, unknown fields silently ignored |
| `TestLoad_DataDirTrailingSlash` | `data_dir: /var/seam/` | Trailing slash stripped (normalize to `/var/seam`) |
| `TestLoad_OllamaURLTrailingSlash` | `ollama_base_url: http://localhost:11434/` | Trailing slash stripped |

---

## 3. internal/auth

### 3a. auth/store (user persistence)

File: `internal/auth/store_test.go`

Uses `testutil.TestServerDB()` for each test.

| Test | Input | Expected |
|---|---|---|
| `TestStore_CreateUser_Success` | Valid username, email, hashed password | User created, returned with ID and timestamps |
| `TestStore_CreateUser_DuplicateUsername` | Two users with same username | Second create returns `ErrUserExists` |
| `TestStore_CreateUser_DuplicateEmail` | Two users with same email | Second create returns `ErrUserExists` |
| `TestStore_CreateUser_EmptyUsername` | Empty username string | Error (NOT a DB constraint error — validate in application) |
| `TestStore_CreateUser_EmptyEmail` | Empty email string | Error |
| `TestStore_GetUserByUsername_Exists` | Create user, then get by username | Returns correct user |
| `TestStore_GetUserByUsername_NotFound` | Get non-existent username | Returns `ErrNotFound` |
| `TestStore_GetUserByUsername_CaseSensitive` | Create "Alice", get "alice" | Returns `ErrNotFound` (usernames are case-sensitive) |
| `TestStore_GetUserByID_Exists` | Create user, then get by ID | Returns correct user |
| `TestStore_GetUserByID_NotFound` | Get non-existent ULID | Returns `ErrNotFound` |
| `TestStore_GetUserByID_InvalidID` | Get with empty string | Returns error |
| `TestStore_CreateRefreshToken` | Valid user ID, token hash, expiry | Token stored |
| `TestStore_GetRefreshToken_Valid` | Create then get by token hash | Returns token with correct user_id and expiry |
| `TestStore_GetRefreshToken_Expired` | Create token with past expiry, then get | Returns `ErrTokenExpired` |
| `TestStore_GetRefreshToken_NotFound` | Get with random hash | Returns `ErrNotFound` |
| `TestStore_DeleteRefreshToken` | Create then delete | Subsequent get returns `ErrNotFound` |
| `TestStore_DeleteUserCascadesTokens` | Create user + tokens, delete user | All refresh tokens for that user gone |

### 3b. auth/jwt

File: `internal/auth/jwt_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestJWT_Sign_ValidClaims` | User ID, username, secret, TTL | Returns a signed JWT string |
| `TestJWT_Verify_ValidToken` | Sign then verify with same secret | Returns correct claims (user_id, username, exp) |
| `TestJWT_Verify_ExpiredToken` | Token with TTL=-1s | Returns `ErrTokenExpired` |
| `TestJWT_Verify_WrongSecret` | Sign with secret A, verify with secret B | Returns `ErrInvalidToken` |
| `TestJWT_Verify_MalformedToken` | Random string "not.a.jwt" | Returns `ErrInvalidToken` |
| `TestJWT_Verify_EmptyToken` | Empty string | Returns `ErrInvalidToken` |
| `TestJWT_Verify_TamperedPayload` | Valid JWT with manually modified payload | Returns `ErrInvalidToken` (signature mismatch) |
| `TestJWT_Verify_MissingUserID` | JWT signed without user_id claim | Returns error about missing required claim |
| `TestJWT_Verify_TokenNotYetValid` | Token with `nbf` in the future | Returns error |
| `TestJWT_RefreshToken_Generate` | Call generate | Returns a random token string (>= 32 bytes, base64 encoded) |
| `TestJWT_RefreshToken_Uniqueness` | Generate 1000 tokens | All unique |

### 3c. auth/handler

File: `internal/auth/handler_test.go`

Uses `httptest.NewRecorder()` + chi router. Auth store is the real store backed by in-memory SQLite (not mocked — the store is simple enough to test through).

**Registration:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Register_Success` | `{"username":"alice","email":"alice@test.com","password":"securepass123"}` | 201, response has user info (id, username, email) + tokens (access_token, refresh_token) |
| `TestHandler_Register_DuplicateUsername` | Register twice with same username | Second: 409 Conflict |
| `TestHandler_Register_DuplicateEmail` | Register twice with same email | Second: 409 Conflict |
| `TestHandler_Register_MissingUsername` | `{"email":"a@b.com","password":"pass"}` | 400, error: "username is required" |
| `TestHandler_Register_MissingEmail` | `{"username":"alice","password":"pass"}` | 400, error: "email is required" |
| `TestHandler_Register_MissingPassword` | `{"username":"alice","email":"a@b.com"}` | 400, error: "password is required" |
| `TestHandler_Register_ShortPassword` | Password "ab" (< 8 chars) | 400, error about minimum password length |
| `TestHandler_Register_InvalidEmail` | `email: "notanemail"` | 400, error about invalid email format |
| `TestHandler_Register_UsernameWithSpaces` | `username: "alice bob"` | 400, error: username must be alphanumeric/underscore/hyphen |
| `TestHandler_Register_UsernameTooLong` | 256-char username | 400, error about max length |
| `TestHandler_Register_InvalidJSON` | `{malformed` | 400, error about invalid JSON |
| `TestHandler_Register_EmptyBody` | Empty request body | 400 |
| `TestHandler_Register_CreatesUserDir` | Successful registration | User directory and notes/inbox/ exist on filesystem |

**Login:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Login_Success` | Valid credentials | 200, user info + tokens (access_token + refresh_token) |
| `TestHandler_Login_WrongPassword` | Correct username, wrong password | 401, "invalid credentials" |
| `TestHandler_Login_NonExistentUser` | Username that doesn't exist | 401, "invalid credentials" (same message as wrong password — no user enumeration) |
| `TestHandler_Login_MissingUsername` | `{"password":"pass"}` | 400 |
| `TestHandler_Login_MissingPassword` | `{"username":"alice"}` | 400 |
| `TestHandler_Login_EmptyBody` | Empty request body | 400 |

**Token refresh:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Refresh_Success` | Valid refresh token | 200, new access_token (refresh token stays valid) |
| `TestHandler_Refresh_ExpiredToken` | Expired refresh token | 401 |
| `TestHandler_Refresh_InvalidToken` | Random string | 401 |
| `TestHandler_Refresh_MissingToken` | Empty body | 400 |
| `TestHandler_Refresh_RevokedToken` | Token that was deleted from DB | 401 |

**Logout:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Logout_Success` | Valid refresh token in body | 204, refresh token revoked |
| `TestHandler_Logout_InvalidToken` | Random string as refresh token | 204 (idempotent, don't reveal whether token existed) |
| `TestHandler_Logout_MissingToken` | Empty body | 400 |
| `TestHandler_Logout_AlreadyLoggedOut` | Use same refresh token twice | First: 204, second: 204 (idempotent) |

**Auth middleware:**

| Test | Input | Expected |
|---|---|---|
| `TestMiddleware_ValidToken` | Request with valid `Authorization: Bearer {token}` | Handler receives user_id in context, 200 |
| `TestMiddleware_MissingHeader` | No Authorization header | 401, "missing authorization header" |
| `TestMiddleware_MalformedHeader` | `Authorization: NotBearer {token}` | 401, "invalid authorization format" |
| `TestMiddleware_ExpiredToken` | Expired JWT in header | 401, "token expired" |
| `TestMiddleware_InvalidToken` | Garbage in Bearer value | 401 |
| `TestMiddleware_UserIDInContext` | Valid token for user "abc" | `auth.UserIDFromContext(ctx)` returns "abc" |

---

## 4. internal/userdb

File: `internal/userdb/manager_test.go`

### Manager lifecycle

| Test | Input | Expected |
|---|---|---|
| `TestManager_Open_CreatesDB` | Open for new user ID | seam.db file created at `{data_dir}/users/{id}/seam.db` |
| `TestManager_Open_CreatesDirStructure` | Open for new user ID | `notes/` and `notes/inbox/` directories created |
| `TestManager_Open_RunsMigrations` | Open new DB, query for tables | All expected tables exist (notes, projects, tags, note_tags, links, notes_fts, ai_tasks) |
| `TestManager_Open_ReturnsCached` | Open same user twice | Returns same `*sql.DB` pointer (not a new connection) |
| `TestManager_Open_DifferentUsersGetDifferentDBs` | Open user A and user B | Different `*sql.DB` instances, different file paths |
| `TestManager_Close_RemovesFromCache` | Open user, close, open again | Second open creates new `*sql.DB` (not cached) |
| `TestManager_Close_NonExistentUser` | Close user that was never opened | No error (idempotent) |
| `TestManager_CloseAll` | Open 3 users, CloseAll | All 3 DB handles closed, cache empty |
| `TestManager_Open_InvalidUserID` | Empty string user ID | Error |
| `TestManager_Open_PathTraversalUserID` | User ID "../etc" | Error (must be a valid ULID or safe string) |

### Migration tests

File: `internal/userdb/migrate_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestMigrate_FreshDB` | Empty in-memory DB | All tables created, migration version recorded |
| `TestMigrate_Idempotent` | Run migrations twice | No error, no duplicate tables |
| `TestMigrate_SchemaCorrect_Notes` | Fresh DB | `notes` table has columns: id, title, project_id, file_path, body, content_hash, source_url, transcript_source, created_at, updated_at |
| `TestMigrate_SchemaCorrect_Links` | Fresh DB | `links` table has: source_note_id, target_note_id, link_text. PK is (source_note_id, link_text) |
| `TestMigrate_SchemaCorrect_FTS` | Fresh DB | `notes_fts` virtual table exists, FTS5 |
| `TestMigrate_SchemaCorrect_AITasks` | Fresh DB | `ai_tasks` table has all expected columns |
| `TestMigrate_ForeignKeys_Enabled` | Fresh DB, insert violating FK | Error (foreign key constraint) |
| `TestMigrate_WALMode` | Fresh DB | `PRAGMA journal_mode` returns "wal" |
| `TestMigrate_FTSDeleteTrigger` | Insert a note, then delete it | FTS entry also removed (verify by searching) |

### Eviction (integration test)

| Test | Input | Expected |
|---|---|---|
| `TestManager_Eviction_InactiveDB` | Open user, wait past eviction timeout | DB handle closed, removed from cache |
| `TestManager_Eviction_ActiveDBNotEvicted` | Open user, keep accessing within timeout | DB stays cached |
| `TestManager_Eviction_ReOpenAfterEviction` | Open, let evict, open again | New DB handle created, works correctly |

---

## 5. internal/note/frontmatter

File: `internal/note/frontmatter_test.go`

### Parsing

| Test | Input | Expected |
|---|---|---|
| `TestParseFrontmatter_Valid` | Standard note with all frontmatter fields | Struct populated: id, title, project, tags, created, modified |
| `TestParseFrontmatter_MinimalFields` | Only `id` and `title` in frontmatter | Other fields are zero values, no error |
| `TestParseFrontmatter_NoFrontmatter` | Markdown with no `---` delimiters | Empty frontmatter struct, body is entire content, no error |
| `TestParseFrontmatter_EmptyFrontmatter` | `---\n---\nBody text` | Empty frontmatter struct, body is "Body text" |
| `TestParseFrontmatter_OnlyFrontmatter` | `---\ntitle: Test\n---` (no body) | Frontmatter parsed, body is empty string |
| `TestParseFrontmatter_BodySeparation` | Full note | Body returned WITHOUT frontmatter delimiters or YAML |
| `TestParseFrontmatter_TagsAsList` | `tags: [a, b, c]` | Tags slice = ["a", "b", "c"] |
| `TestParseFrontmatter_TagsAsFlowSequence` | `tags:\n  - a\n  - b` | Tags slice = ["a", "b"] |
| `TestParseFrontmatter_EmptyTagsList` | `tags: []` | Tags slice is empty (not nil) |
| `TestParseFrontmatter_TimestampParsing` | `created: 2026-03-08T10:00:00Z` | Parsed as time.Time in UTC |
| `TestParseFrontmatter_TimestampWithTimezone` | `created: 2026-03-08T10:00:00+09:00` | Parsed correctly with timezone |
| `TestParseFrontmatter_SourceURL` | `source_url: https://example.com` | SourceURL field populated |
| `TestParseFrontmatter_TranscriptSource` | `transcript_source: true` | TranscriptSource == true |
| `TestParseFrontmatter_UnknownFieldsPreserved` | Frontmatter with `custom_field: value` | Unknown fields stored in an extras map (not lost on round-trip) |
| `TestParseFrontmatter_MalformedYAML` | `---\ntitle: [unclosed\n---` | Error with descriptive message |
| `TestParseFrontmatter_TripleDashInBody` | Body contains `---` (e.g., horizontal rule) | Only first `---` pair is frontmatter; body `---` is part of body |
| `TestParseFrontmatter_WindowsLineEndings` | File with `\r\n` line endings | Parsed correctly |
| `TestParseFrontmatter_UTF8Content` | Title with Japanese/emoji characters | Parsed correctly, no encoding issues |
| `TestParseFrontmatter_TitleWithQuotes` | `title: "He said \"hello\""` | Escaped quotes handled |
| `TestParseFrontmatter_TitleWithColon` | `title: "API: Design Patterns"` | Colon in quoted value works |
| `TestParseFrontmatter_ProjectAsSlug` | `project: my-project` | Project field is the slug string |

### Serialization

| Test | Input | Expected |
|---|---|---|
| `TestSerializeFrontmatter_RoundTrip` | Parse valid frontmatter, serialize back | Output matches input (field order may differ, but content identical) |
| `TestSerializeFrontmatter_WithBody` | Frontmatter struct + body string | Output is `---\n{yaml}\n---\n{body}` |
| `TestSerializeFrontmatter_EmptyBody` | Frontmatter struct, empty body | Output is `---\n{yaml}\n---\n` |
| `TestSerializeFrontmatter_PreservesUnknownFields` | Frontmatter with extras | Unknown fields appear in serialized output |
| `TestSerializeFrontmatter_OmitsZeroValues` | Frontmatter with no source_url | `source_url` key does not appear in output |
| `TestSerializeFrontmatter_TimestampFormat` | Frontmatter with timestamps | Timestamps serialized as RFC3339 |

---

## 6. internal/note/wikilink

File: `internal/note/wikilink_test.go`

All tests are table-driven.

### Extraction

| Test | Input Body | Expected Links |
|---|---|---|
| `TestExtractWikilinks_SingleLink` | `Check [[API Design]] for details` | `[{Target: "API Design", Display: ""}]` |
| `TestExtractWikilinks_MultipleLinks` | `See [[Note A]] and [[Note B]]` | 2 links, order preserved |
| `TestExtractWikilinks_AliasedLink` | `See [[api-design\|API Design Patterns]]` | `[{Target: "api-design", Display: "API Design Patterns"}]` |
| `TestExtractWikilinks_DuplicateLinks` | `[[A]] and [[A]] again` | 2 entries (both extracted, dedup is caller's responsibility) |
| `TestExtractWikilinks_AdjacentLinks` | `[[A]][[B]]` | 2 links |
| `TestExtractWikilinks_LinkAtStartOfLine` | `[[Start]] of line` | 1 link |
| `TestExtractWikilinks_LinkAtEndOfLine` | `End of [[Line]]` | 1 link |
| `TestExtractWikilinks_LinkOnOwnLine` | `\n[[Standalone]]\n` | 1 link |
| `TestExtractWikilinks_NoLinks` | `Plain text with no links` | Empty slice |
| `TestExtractWikilinks_EmptyInput` | `` | Empty slice |

### Code block exclusion

| Test | Input Body | Expected Links |
|---|---|---|
| `TestExtractWikilinks_IgnoreFencedCodeBlock` | ````\n[[inside code]]\n```` | Empty slice |
| `TestExtractWikilinks_IgnoreInlineCode` | `` `[[inside inline]]` `` | Empty slice |
| `TestExtractWikilinks_MixedCodeAndLinks` | `[[real link]] and `[[code link]]`` | 1 link: "real link" only |
| `TestExtractWikilinks_MultipleFencedBlocks` | Link, then fenced block with link, then another link | 2 real links, code link excluded |
| `TestExtractWikilinks_IndentedCodeBlock` | 4-space indented `[[link]]` | This is ambiguous — document the chosen behavior |
| `TestExtractWikilinks_FencedBlockWithLanguage` | ````go\n[[link]]\n```` | Excluded |
| `TestExtractWikilinks_NestedBackticks` | ```` `` [[inside]] `` ```` | Excluded |

### Edge cases

| Test | Input Body | Expected |
|---|---|---|
| `TestExtractWikilinks_EmptyBrackets` | `[[]]` | Excluded (empty target is not a valid link) |
| `TestExtractWikilinks_SingleBrackets` | `[not a link]` | Empty slice |
| `TestExtractWikilinks_UnclosedBrackets` | `[[unclosed` | Empty slice |
| `TestExtractWikilinks_NestedBrackets` | `[[ [[inner]] ]]` | Behavior: extracts "inner" (greedy match on first `]]`) |
| `TestExtractWikilinks_LinkWithNewline` | `[[broken\nlink]]` | Empty (wikilinks don't span lines) |
| `TestExtractWikilinks_SpecialCharsInTarget` | `[[API (v2) - Design]]` | Target: "API (v2) - Design" |
| `TestExtractWikilinks_UnicodeTarget` | `[[Japanese text here]]` | Extracted correctly |
| `TestExtractWikilinks_AliasWithPipe` | `[[target\|display\|extra]]` | Target: "target", Display: "display|extra" (only first pipe splits) |
| `TestExtractWikilinks_WhitespaceAroundTarget` | `[[ spaced ]]` | Target: "spaced" (trimmed) or " spaced " (document the decision) |

---

## 7. internal/note/tag

File: `internal/note/tag_test.go`

### Inline tag extraction

| Test | Input Body | Expected Tags |
|---|---|---|
| `TestExtractTags_SingleTag` | `This is #important` | `["important"]` |
| `TestExtractTags_MultipleTags` | `#tag1 text #tag2` | `["tag1", "tag2"]` |
| `TestExtractTags_TagAtStart` | `#first word` | `["first"]` |
| `TestExtractTags_TagAtEnd` | `word #last` | `["last"]` |
| `TestExtractTags_TagWithHyphens` | `#my-tag` | `["my-tag"]` |
| `TestExtractTags_TagWithUnderscores` | `#my_tag` | `["my_tag"]` |
| `TestExtractTags_TagWithNumbers` | `#v2` | `["v2"]` |
| `TestExtractTags_DuplicateTags` | `#tag and #tag again` | `["tag"]` (deduplicated) |
| `TestExtractTags_NoTags` | `Plain text` | Empty slice |
| `TestExtractTags_EmptyInput` | `` | Empty slice |

### False positive exclusion

| Test | Input Body | Expected Tags |
|---|---|---|
| `TestExtractTags_IgnoreHeadings` | `## Heading` | Empty slice |
| `TestExtractTags_IgnoreH1` | `# Title` | Empty slice |
| `TestExtractTags_IgnoreH3` | `### Sub-heading` | Empty slice |
| `TestExtractTags_IgnoreInFencedCode` | ````\n#tag\n```` | Empty slice |
| `TestExtractTags_IgnoreInInlineCode` | `` `#tag` `` | Empty slice |
| `TestExtractTags_IgnoreInURL` | `http://example.com/#section` | Empty slice |
| `TestExtractTags_IgnoreNumberOnly` | `Issue #123` | Empty slice (pure numeric "tags" are treated as issue references) |
| `TestExtractTags_IgnoreHashWithoutWord` | `# ` (hash followed by space, not a heading marker) | Empty slice |
| `TestExtractTags_IgnoreHexColors` | `color: #ff0000` | Empty slice (pure hex is ignored) |
| `TestExtractTags_MixedHeadingAndTag` | `## Section\nText with #tag` | `["tag"]` (heading excluded, inline tag captured) |
| `TestExtractTags_TagAfterHeading` | `## Heading #tag` | Document behavior: is this a tag or part of the heading? Define and test. |

### Frontmatter + body merge

| Test | Input | Expected |
|---|---|---|
| `TestMergeTags_OnlyFrontmatter` | Frontmatter: `["a", "b"]`, Body: no tags | `["a", "b"]` |
| `TestMergeTags_OnlyBody` | Frontmatter: empty, Body: `#c #d` | `["c", "d"]` |
| `TestMergeTags_Both_NoDuplicates` | Frontmatter: `["a"]`, Body: `#b` | `["a", "b"]` |
| `TestMergeTags_Both_WithDuplicates` | Frontmatter: `["a", "b"]`, Body: `#b #c` | `["a", "b", "c"]` (dedup) |
| `TestMergeTags_CaseHandling` | Frontmatter: `["API"]`, Body: `#api` | Document: are tags case-sensitive? Test the decision. |
| `TestMergeTags_EmptyBoth` | Frontmatter: nil, Body: no tags | Empty slice (not nil) |

---

## 8. internal/project

### 8a. project/store

File: `internal/project/store_test.go`

Uses `testutil.TestUserDB()`.

| Test | Input | Expected |
|---|---|---|
| `TestStore_Create_Success` | Valid project (name, slug, description) | Project created, returned with ID and timestamps |
| `TestStore_Create_DuplicateSlug` | Two projects with same slug | Error (UNIQUE constraint) |
| `TestStore_Create_EmptyName` | Empty name | Error |
| `TestStore_Get_Exists` | Create then get by ID | Correct project returned |
| `TestStore_Get_NotFound` | Get with random ULID | `ErrNotFound` |
| `TestStore_GetBySlug_Exists` | Create project with slug "my-project", get by slug | Correct project returned |
| `TestStore_GetBySlug_NotFound` | Get by non-existent slug | `ErrNotFound` |
| `TestStore_List_Empty` | No projects | Empty slice (not nil) |
| `TestStore_List_Multiple` | Create 3 projects | All 3 returned, ordered by created_at desc |
| `TestStore_Update_Success` | Create, then update name | Updated project has new name, updated_at changed |
| `TestStore_Update_NotFound` | Update non-existent ID | `ErrNotFound` |
| `TestStore_Update_DuplicateSlug` | Update slug to collide with existing | Error |
| `TestStore_Delete_Success` | Create then delete | Subsequent get returns `ErrNotFound` |
| `TestStore_Delete_NotFound` | Delete non-existent | `ErrNotFound` |
| `TestStore_Delete_CascadesNotesProjectID` | Create project, create note in project, delete project | Note's project_id is SET NULL (note still exists in inbox) |

### 8b. project/service

File: `internal/project/service_test.go`

Uses real filesystem (`t.TempDir()`).

| Test | Input | Expected |
|---|---|---|
| `TestService_Create_CreatesDirectory` | Create project "My Project" | Directory `my-project/` created under user's notes dir |
| `TestService_Create_SlugFromName` | Name "API Design Patterns" | Slug: "api-design-patterns" |
| `TestService_Create_SlugDedup` | Create "Test" twice (second should get unique slug) | Error on duplicate, or slug "test-2" — define behavior |
| `TestService_Create_SlugSpecialChars` | Name "C++ / Rust (2026)" | Slug: "c-rust-2026" (strip unsafe chars) |
| `TestService_Create_SlugUnicode` | Name with unicode chars | Slug is ASCII-safe |
| `TestService_Delete_RemovesDirectory` | Create then delete project | Directory removed from filesystem |
| `TestService_Delete_DirWithFiles` | Project dir has files, delete with `cascade=delete` | Files deleted, note DB rows deleted |
| `TestService_Delete_MoveToInbox` | Project dir has files, delete with `cascade=inbox` | Files moved to `inbox/`, notes updated |

### 8c. project/handler

File: `internal/project/handler_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Create_Success` | `POST /api/projects {"name":"Test"}` | 201, project JSON with id, slug |
| `TestHandler_Create_MissingName` | `POST /api/projects {}` | 400 |
| `TestHandler_Create_Unauthorized` | No auth header | 401 |
| `TestHandler_List_Success` | Create 2 projects, GET /api/projects | 200, array of 2 |
| `TestHandler_List_Empty` | GET /api/projects (none created) | 200, `[]` |
| `TestHandler_Get_Success` | `GET /api/projects/{id}` | 200, project JSON |
| `TestHandler_Get_NotFound` | `GET /api/projects/{bad-id}` | 404 |
| `TestHandler_Get_WrongUser` | User A creates project, User B requests it | 404 (not 403 — don't reveal existence) |
| `TestHandler_Update_Success` | `PUT /api/projects/{id} {"name":"New Name"}` | 200, updated project |
| `TestHandler_Delete_Success` | `DELETE /api/projects/{id}` | 204 |
| `TestHandler_Delete_NotFound` | `DELETE /api/projects/{bad-id}` | 404 |

---

## 9. internal/note (store + service)

### 9a. note/store

File: `internal/note/store_test.go`

Uses `testutil.TestUserDB()`. Each test gets a fresh DB.

**CRUD:**

| Test | Input | Expected |
|---|---|---|
| `TestStore_Create_Success` | Full note struct | Note inserted, all fields persisted |
| `TestStore_Create_DuplicateID` | Insert same ID twice | Error (PK constraint) |
| `TestStore_Create_DuplicateFilePath` | Two notes with same file_path | Error (UNIQUE constraint) |
| `TestStore_Create_NullProjectID` | Note with empty project_id (inbox note) | Created with NULL project_id |
| `TestStore_Get_Exists` | Create then get | All fields match |
| `TestStore_Get_NotFound` | Get random ID | `ErrNotFound` |
| `TestStore_Update_Success` | Create, update title | Title changed, updated_at changed |
| `TestStore_Update_NotFound` | Update random ID | `ErrNotFound` |
| `TestStore_Delete_Success` | Create then delete | Gone from DB |
| `TestStore_Delete_NotFound` | Delete random ID | `ErrNotFound` |
| `TestStore_Delete_CascadesLinks` | Note A links to note B, delete A | Links from A removed |
| `TestStore_Delete_CascadesTags` | Note with tags, delete note | note_tags rows removed |

**List with filters:**

| Test | Input | Expected |
|---|---|---|
| `TestStore_List_All` | 5 notes, no filter | All 5 returned |
| `TestStore_List_ByProject` | 3 notes in project A, 2 in B, filter by A | 3 returned |
| `TestStore_List_ByTag` | Notes with tags [a,b], [b,c], [c,d], filter by "b" | First 2 returned |
| `TestStore_List_BySince` | Notes from Jan, Feb, Mar, filter since=Feb | Feb and Mar returned |
| `TestStore_List_ByUntil` | Notes from Jan, Feb, Mar, filter until=Feb | Jan and Feb returned |
| `TestStore_List_ByDateRange` | Notes from Jan-Dec, filter since=Mar until=May | Mar, Apr, May returned |
| `TestStore_List_SortByCreated` | 3 notes, sort=created | Ordered by created_at desc |
| `TestStore_List_SortByModified` | 3 notes with different mod times, sort=modified | Ordered by updated_at desc |
| `TestStore_List_CombinedFilters` | Filter by project AND tag | Only notes matching both |
| `TestStore_List_Empty` | No notes | Empty slice (not nil) |
| `TestStore_List_InboxOnly` | Filter with InboxOnly=true | Only notes without project_id |
| `TestStore_List_SortAscending` | 3 notes, sort=created, sortDir=asc | Ordered by created_at ascending |
| `TestStore_List_DefaultSortDesc` | 3 notes, no sortDir specified | Ordered descending (default) |

**Links:**

| Test | Input | Expected |
|---|---|---|
| `TestStore_UpdateLinks_Insert` | Note with 3 wikilinks | 3 rows in links table |
| `TestStore_UpdateLinks_Replace` | Update links from [A,B] to [B,C] | Link A removed, C added, B kept |
| `TestStore_UpdateLinks_Empty` | Update with empty links | All links for note removed |
| `TestStore_UpdateLinks_DanglingLink` | Link to non-existent note | target_note_id is NULL, link_text preserved |
| `TestStore_UpdateLinks_ResolveDangling` | Create dangling link, then create target note, re-update links | target_note_id now points to real note |
| `TestStore_GetBacklinks` | A->B, C->B, get backlinks for B | Returns A and C |
| `TestStore_GetBacklinks_None` | Note with no incoming links | Empty slice |
| `TestStore_GetBacklinks_SelfLink` | Note links to itself | Returned in backlinks |

**Tags:**

| Test | Input | Expected |
|---|---|---|
| `TestStore_UpdateTags_Insert` | Note with tags ["a", "b"] | Tags created, note_tags rows created |
| `TestStore_UpdateTags_Replace` | Change from ["a","b"] to ["b","c"] | "a" association removed, "c" added |
| `TestStore_UpdateTags_ReuseExistingTag` | Two notes both tagged "common" | Only one row in tags table, two rows in note_tags |
| `TestStore_UpdateTags_Empty` | Update to empty tags | All note_tags rows removed for this note |
| `TestStore_UpdateTags_OrphanTagCleanup` | Remove last note using tag "old" | Define: do we clean up orphan tags or leave them? Test the decision. |

**FTS:**

| Test | Input | Expected |
|---|---|---|
| `TestStore_UpdateFTS_Insert` | Insert title + content | Searchable via FTS MATCH |
| `TestStore_UpdateFTS_Update` | Insert, then update content | Old content not searchable, new content is |
| `TestStore_UpdateFTS_Delete` | Insert, then delete note | FTS entry removed (via trigger), search returns nothing |

### 9b. note/service

File: `internal/note/service_test.go`

Service tests use real filesystem + real in-memory SQLite. The service coordinates file I/O, store operations, and (mocked) AI queue and WebSocket hub.

| Test | Input | Expected |
|---|---|---|
| `TestService_Create_WritesFile` | Create note with title + body | `.md` file written to correct path with frontmatter |
| `TestService_Create_InInbox` | Create note without project | File written to `inbox/` |
| `TestService_Create_InProject` | Create note in project "foo" | File written to `foo/` |
| `TestService_Create_GeneratesULID` | Create note without ID | ID is a valid ULID |
| `TestService_Create_SetsTimestamps` | Create note | created_at and updated_at set to ~now |
| `TestService_Create_IndexesWikilinks` | Create note with `[[A]] and [[B]]` in body | links table has 2 entries |
| `TestService_Create_IndexesTags` | Create note with `#tag1` in body and `tags: [tag2]` in request | Both tags indexed |
| `TestService_Create_IndexesFTS` | Create note with body "caching strategies" | FTS search for "caching" returns this note |
| `TestService_Create_ComputesContentHash` | Create note | content_hash in DB matches SHA-256 of body |
| `TestService_Create_EnqueuesEmbedding` | Create note (with mocked queue) | Embed task enqueued with note ID |
| `TestService_Get_ReturnsFileContent` | Create, then get | Body content read from file, not stored in DB |
| `TestService_Get_NotFound` | Get non-existent ID | `ErrNotFound` |
| `TestService_Update_OverwritesFile` | Update body | File on disk has new content |
| `TestService_Update_UpdatesFrontmatter` | Change title | Frontmatter in file reflects new title |
| `TestService_Update_ReindexesLinks` | Change body to have different wikilinks | Links table updated |
| `TestService_Update_ReindexesTags` | Change tags | Tags table updated |
| `TestService_Update_ReindexesFTS` | Change body content | FTS updated (new content searchable, old is not) |
| `TestService_Update_UpdatesContentHash` | Change body | content_hash updated |
| `TestService_Update_SkipsReindexIfHashUnchanged` | Update with same body content | Wikilink/tag/FTS re-index NOT called (verified via mock counts) |
| `TestService_Update_EnqueuesReembedding` | Update body (with mocked queue) | Embed task enqueued |
| `TestService_Delete_RemovesFile` | Delete note | File gone from disk |
| `TestService_Delete_RemovesFromDB` | Delete note | Note, links, tags, FTS all cleaned up |
| `TestService_Delete_RemovesEmbeddings` | Delete (with mocked ChromaDB) | Delete embedding task enqueued |
| `TestService_Delete_FileAlreadyGone` | Delete note whose file was already removed from disk | No error (idempotent) |
| `TestService_Reindex_FromDisk` | Manually edit file on disk, call Reindex | DB metadata, links, tags, FTS all updated from file |
| `TestService_Reindex_NewFile` | File exists on disk but not in DB | Note created in DB from file |
| `TestService_Reindex_SkipsIfHashUnchanged` | Call Reindex when file hasn't changed | No writes to DB |
| `TestService_Reindex_ResolvesProjectSlug` | File has `project: my-project` in frontmatter | project_id resolved to ULID via project.Store.GetBySlug |
| `TestService_Reindex_UnknownProjectSlug` | File has `project: nonexistent` in frontmatter | project_id set to NULL (inbox), warning logged |
| `TestService_MoveNote_BetweenProjects` | Move note from project A to project B | File moved on disk, project_id updated in DB, file_path updated |
| `TestService_MoveNote_ToInbox` | Move note to inbox (project=nil) | File moved to inbox/ |

---

## 10. internal/note (handler)

File: `internal/note/handler_test.go`

HTTP-level tests. Service layer mocked via interface.

**Create:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Create_Success` | `POST /api/notes {"title":"T","body":"B"}` | 201, note JSON with id |
| `TestHandler_Create_WithProject` | `POST /api/notes {"title":"T","body":"B","project_id":"..."}` | 201, project_id set |
| `TestHandler_Create_MissingTitle` | `{"body":"B"}` | 400 |
| `TestHandler_Create_EmptyBody` | `{"title":"T","body":""}` | 201 (empty body is valid) |
| `TestHandler_Create_Unauthorized` | No auth header | 401 |
| `TestHandler_Create_InvalidJSON` | `{bad` | 400 |
| `TestHandler_Create_TitleTooLong` | 1000-char title | 400, max length error |

**Read:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Get_Success` | `GET /api/notes/{id}` | 200, full note JSON including body |
| `TestHandler_Get_NotFound` | Bad ID | 404 |
| `TestHandler_Get_InvalidIDFormat` | `GET /api/notes/not-a-ulid` | 400 or 404 |

**List:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_List_NoFilters` | `GET /api/notes` | 200, array, X-Total-Count header present |
| `TestHandler_List_FilterByProject` | `GET /api/notes?project={id}` | 200, filtered |
| `TestHandler_List_FilterByTag` | `GET /api/notes?tag=arch` | 200, filtered |
| `TestHandler_List_FilterByDateRange` | `GET /api/notes?since=...&until=...` | 200, filtered |
| `TestHandler_List_InvalidDateFormat` | `?since=not-a-date` | 400 |
| `TestHandler_List_InvalidSort` | `?sort=invalid` | 400 |
| `TestHandler_List_Pagination` | `GET /api/notes?limit=2&offset=1` | 200, 2 notes returned, X-Total-Count reflects all |
| `TestHandler_List_LimitExceedsMax` | `GET /api/notes?limit=1000` | 200, capped to 500 |
| `TestHandler_List_DefaultLimit` | `GET /api/notes` (no limit) | Default 100 applied |
| `TestHandler_List_InboxFilter` | `GET /api/notes?project=inbox` | 200, only inbox notes |

**Update:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Update_Success` | `PUT /api/notes/{id} {"title":"New","body":"New"}` | 200, updated note |
| `TestHandler_Update_PartialUpdate` | `PUT /api/notes/{id} {"title":"New"}` | 200, only title changed |
| `TestHandler_Update_NotFound` | PUT to bad ID | 404 |

**Delete:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Delete_Success` | `DELETE /api/notes/{id}` | 204 |
| `TestHandler_Delete_NotFound` | DELETE bad ID | 404 |

**Backlinks:**

| Test | Input | Expected |
|---|---|---|
| `TestHandler_Backlinks_Success` | `GET /api/notes/{id}/backlinks` | 200, array of linking notes |
| `TestHandler_Backlinks_NoneExist` | Note with no backlinks | 200, `[]` |
| `TestHandler_Backlinks_NoteNotFound` | Bad note ID | 404 |

---

## 11. internal/search/fts

File: `internal/search/fts_test.go`

Uses `testutil.TestUserDB()` with notes pre-inserted.

| Test | Input | Expected |
|---|---|---|
| `TestFTS_Search_SingleWord` | Search "caching" (note has "caching strategies") | 1 result |
| `TestFTS_Search_MultipleWords` | Search "caching strategies" | Results matching both words ranked higher |
| `TestFTS_Search_Prefix` | Search "cach*" | Matches "caching", "cache", "cached" |
| `TestFTS_Search_NoResults` | Search "nonexistentterm" | Empty slice |
| `TestFTS_Search_MatchesTitle` | Search note's title text | Result found |
| `TestFTS_Search_MatchesBody` | Search text only in body | Result found |
| `TestFTS_Search_Ranking` | 3 notes, one mentions "API" 10 times, one 2 times, one 1 time | Ranked by relevance (bm25) |
| `TestFTS_Search_Stemming` | Search "running" when note has "runs" | Match found (Porter stemming) |
| `TestFTS_Search_EmptyQuery` | Search "" | Error or empty results (define behavior) |
| `TestFTS_Search_SpecialChars` | Search `"foo AND bar"` | Treated as literal search, not FTS operators (escape user input) |
| `TestFTS_Search_SQLInjection` | Search `"); DROP TABLE notes; --` | No error, no damage, treated as literal text |
| `TestFTS_Search_VeryLongQuery` | 10000-char search string | No crash, returns results or empty (define max query length) |
| `TestFTS_Search_Unicode` | Index and search Japanese text | Works correctly |
| `TestFTS_Search_PhraseQuery` | Notes "red fast car" and "fast red car", search "red fast" | Both match, but phrase-adjacent match ranks higher |
| `TestFTS_Index_Update` | Index note, update body, search old text | Old text NOT found |
| `TestFTS_Index_Delete` | Index note, delete note, search | Not found |
| `TestFTS_Handler_Success` | `GET /api/search?q=test` | 200, results with note metadata |
| `TestFTS_Handler_MissingQuery` | `GET /api/search` (no q param) | 400 |
| `TestFTS_Handler_Unauthorized` | No auth header | 401 |

---

## 12. internal/watcher

File: `internal/watcher/watcher_test.go`

All watcher tests use `//go:build integration` since they need real filesystem events.

### File events

| Test | Input | Expected |
|---|---|---|
| `TestWatcher_FileCreated` | Watch dir, create new `.md` file | Reindex called with file path |
| `TestWatcher_FileModified` | Watch dir, modify existing `.md` file | Reindex called |
| `TestWatcher_FileDeleted` | Watch dir, delete `.md` file | Delete called with note ID |
| `TestWatcher_FileRenamed` | Watch dir, rename `.md` file | Old path deleted, new path indexed |
| `TestWatcher_NonMDFile` | Create `.txt` file | Reindex NOT called (only watches .md) |
| `TestWatcher_HiddenFile` | Create `.hidden.md` | Reindex NOT called (ignore dotfiles) |
| `TestWatcher_Debounce` | Rapidly write to same file 10 times in 100ms | Reindex called only once (after debounce) |
| `TestWatcher_SubdirectoryCreated` | Create new directory under notes/ | New directory is now being watched |
| `TestWatcher_SubdirectoryDeleted` | Remove a project directory | Watcher handles gracefully (no crash) |
| `TestWatcher_SymlinkCreated` | Create symlink in notes/ | Define behavior: follow or ignore? Test decision. |
| `TestWatcher_IgnoreNext_Suppresses` | Call IgnoreNext(path), then write to that path | Reindex NOT called (event suppressed) |
| `TestWatcher_IgnoreNext_ExpiresAfterTTL` | Call IgnoreNext(path), wait 3s, then write to path | Reindex IS called (suppression expired) |
| `TestWatcher_IgnoreNext_OnlyOnce` | Call IgnoreNext(path), write twice | First write suppressed, second write triggers reindex |

### Multi-user isolation

| Test | Input | Expected |
|---|---|---|
| `TestWatcher_MultipleUsers` | Watch dirs for user A and B, modify file in A's dir | Only A's reindex triggered, not B's |
| `TestWatcher_UnwatchUser` | Watch user A, then Unwatch | File changes in A's dir no longer trigger events |

### Reconciliation

File: `internal/watcher/reconcile_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestReconcile_NewFileOnDisk` | File exists on disk, not in DB | Note created in DB from file's frontmatter |
| `TestReconcile_NewFileNoFrontmatter` | File on disk without `---` frontmatter | ULID assigned, title from filename, indexed |
| `TestReconcile_DeletedFromDisk` | Note in DB, file gone from disk | Note removed from DB |
| `TestReconcile_ModifiedOnDisk` | File mtime > DB updated_at | Note re-indexed |
| `TestReconcile_UnchangedOnDisk` | File mtime <= DB updated_at | No re-index (skip, mtime fast-path) |
| `TestReconcile_MtimeChangedContentSame` | File mtime > DB updated_at but content_hash matches | No re-index (hash confirms no real change) |
| `TestReconcile_EmptyNotesDir` | No files in notes/ | DB cleared of notes (if any stale ones exist) |
| `TestReconcile_LargeReconciliation` | 500 files on disk, empty DB | All 500 indexed without error, reasonable time (< 10s) |
| `TestReconcile_MultipleProjects` | Files in inbox/, project-a/, project-b/ | All indexed with correct project associations |
| `TestReconcile_ConflictingIDs` | Two files on disk with same `id:` in frontmatter | Error or second file gets new ULID. Define and test. |
| `TestReconcile_CorruptedFile` | File with invalid frontmatter YAML | Logged as warning, skipped (not crash) |

---

## 13. internal/ws

File: `internal/ws/hub_test.go`

### Connection management

| Test | Input | Expected |
|---|---|---|
| `TestHub_Register` | Register connection for user A | Hub tracks connection |
| `TestHub_Register_MultipleConnections` | Register 3 connections for user A | All 3 tracked |
| `TestHub_Unregister` | Register then unregister | Connection removed |
| `TestHub_Unregister_NonExistent` | Unregister connection that was never registered | No error (idempotent) |
| `TestHub_Unregister_OneOfMany` | 3 connections, unregister 1 | Other 2 still tracked |

### Message delivery

| Test | Input | Expected |
|---|---|---|
| `TestHub_Send_SingleConnection` | Send to user with 1 connection | Message received by connection |
| `TestHub_Send_MultipleConnections` | Send to user with 3 connections | All 3 receive the message |
| `TestHub_Send_NoConnections` | Send to user with no connections | No error (message dropped) |
| `TestHub_Send_DifferentUser` | Send to user A, user B connected | User B does NOT receive message |
| `TestHub_Broadcast` | Broadcast, users A and B connected | Both receive |
| `TestHub_Send_ClosedConnection` | Connection closed but not unregistered | Detect closed conn, auto-unregister, no crash |

### Protocol

File: `internal/ws/protocol_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestProtocol_Serialize_NoteChanged` | NoteChangedEvent{ID, Type: "modified"} | `{"type":"note.changed","payload":{"note_id":"...","change":"modified"}}` |
| `TestProtocol_Serialize_TaskProgress` | TaskProgressEvent | Valid JSON with type "task.progress" |
| `TestProtocol_Serialize_TaskComplete` | TaskCompleteEvent | Valid JSON with type "task.complete" |
| `TestProtocol_Serialize_ChatStream` | ChatStreamEvent{Token: "hello"} | `{"type":"chat.stream","payload":{"token":"hello"}}` |
| `TestProtocol_Deserialize_ChatAsk` | `{"type":"chat.ask","payload":{"query":"test"}}` | Parsed correctly |
| `TestProtocol_Deserialize_UnknownType` | `{"type":"unknown","payload":{}}` | Error: unknown message type |
| `TestProtocol_Deserialize_InvalidJSON` | `{bad json` | Error |
| `TestProtocol_Deserialize_MissingType` | `{"payload":{}}` | Error: missing type field |

### WebSocket auth

| Test | Input | Expected |
|---|---|---|
| `TestWSAuth_ValidToken` | Connect, send valid JWT as first message | Connection accepted, registered with correct user ID |
| `TestWSAuth_InvalidToken` | Connect, send invalid JWT | Connection closed with error |
| `TestWSAuth_ExpiredToken` | Connect, send expired JWT | Connection closed |
| `TestWSAuth_NoTokenSent` | Connect, send a non-auth message first | Connection closed with error |
| `TestWSAuth_TokenTimeout` | Connect, wait >5s without sending token | Connection closed (auth timeout) |

---

## 14. internal/ai/ollama

File: `internal/ai/ollama_test.go`

All tests use `httptest.NewServer` to mock Ollama's HTTP API.

### Embedding

| Test | Input | Expected |
|---|---|---|
| `TestOllama_Embedding_Success` | Mock returns `{"embedding": [0.1, 0.2, ...]}` | Returns `[]float64{0.1, 0.2, ...}` |
| `TestOllama_Embedding_EmptyText` | Empty input string | Error: "text is required" (validate before HTTP call) |
| `TestOllama_Embedding_ModelNotFound` | Mock returns 404 | Error wrapping "model not found" |
| `TestOllama_Embedding_ServerDown` | No mock server running | Error wrapping connection refused |
| `TestOllama_Embedding_Timeout` | Mock server delays 60s, client timeout 1s | Error wrapping context deadline |
| `TestOllama_Embedding_LargeText` | 50,000 character input | No error (Ollama handles chunking internally) |
| `TestOllama_Embedding_ServerError` | Mock returns 500 | Error with status code |

### Chat completion (non-streaming)

| Test | Input | Expected |
|---|---|---|
| `TestOllama_Chat_Success` | Mock returns chat response | Parsed response with content |
| `TestOllama_Chat_EmptyMessages` | No messages | Error |
| `TestOllama_Chat_ModelNotFound` | Mock 404 | Error |
| `TestOllama_Chat_Timeout` | Mock delays, short timeout | Error |

### Chat completion (streaming)

| Test | Input | Expected |
|---|---|---|
| `TestOllama_ChatStream_Success` | Mock returns NDJSON stream of tokens | Channel receives each token in order, then closes |
| `TestOllama_ChatStream_ServerError` | Mock returns 500 | Error on first read |
| `TestOllama_ChatStream_MidStreamError` | Mock sends 3 tokens then closes connection | 3 tokens received, then error |
| `TestOllama_ChatStream_ContextCancelled` | Cancel context during stream | Stream stops, no goroutine leak |

### Request construction

| Test | Input | Expected |
|---|---|---|
| `TestOllama_EmbeddingRequest_Format` | Call embedding with model "qwen3-embedding:8b", text "hello" | HTTP POST to `/api/embed`, body has `{"model":"qwen3-embedding:8b","input":"hello"}` |
| `TestOllama_ChatRequest_Format` | Call chat with model, messages, stream=false | HTTP POST to `/api/chat`, body has correct format |
| `TestOllama_ChatRequest_StreamFormat` | Call chat with stream=true | Request body has `"stream": true` |

---

## 15. internal/ai/chroma

File: `internal/ai/chroma_test.go`

All tests use `httptest.NewServer` to mock ChromaDB's HTTP API.

| Test | Input | Expected |
|---|---|---|
| `TestChroma_CreateCollection_Success` | Mock returns 200 | No error, collection ID returned |
| `TestChroma_CreateCollection_AlreadyExists` | Mock returns 409 | No error (idempotent — get existing) |
| `TestChroma_AddDocuments_Success` | Add 3 docs with IDs, embeddings, metadata | Correct POST body sent to ChromaDB |
| `TestChroma_AddDocuments_EmptyList` | Add 0 docs | No HTTP call made (noop) |
| `TestChroma_Query_Success` | Mock returns 5 nearest neighbors | Returns sorted list with IDs, distances, metadata |
| `TestChroma_Query_NoResults` | Mock returns empty results | Empty slice |
| `TestChroma_Query_NResults` | Query with nResults=3 | Request body has `"n_results": 3` |
| `TestChroma_UpdateDocuments_Success` | Update 2 docs | Correct POST body |
| `TestChroma_DeleteDocuments_Success` | Delete by IDs | Correct POST body |
| `TestChroma_DeleteDocuments_ByNotePrefix` | Delete all chunks for a note (IDs with prefix) | Where clause filters by ID prefix |
| `TestChroma_ServerDown` | No mock server | Connection error |
| `TestChroma_ServerError` | Mock returns 500 | Error with status |
| `TestChroma_CollectionNaming` | User ID "abc123" | Collection name: "user_abc123_notes" |

---

## 16. internal/ai/queue

File: `internal/ai/queue_test.go`

### Priority ordering

| Test | Input | Expected |
|---|---|---|
| `TestQueue_PriorityOrder` | Enqueue: background, user-triggered, interactive | Executed in order: interactive, user-triggered, background |
| `TestQueue_SamePriority_FIFO` | Enqueue 3 background tasks | Executed in insertion order |
| `TestQueue_InteractivePreemptsBackground` | Background task running, interactive enqueued | Interactive runs next (after current finishes) |
| `TestQueue_InteractivePreemptsUserTriggered` | User-triggered waiting, interactive enqueued | Interactive runs first |

### Fair scheduling

| Test | Input | Expected |
|---|---|---|
| `TestQueue_FairScheduling_SamePriority` | User A enqueues 5 background tasks, User B enqueues 5 background tasks | Execution alternates: A, B, A, B, ... |
| `TestQueue_FairScheduling_ThreeUsers` | 3 users, 3 tasks each, same priority | Round-robin: A, B, C, A, B, C, ... |
| `TestQueue_FairScheduling_UnequalLoads` | User A: 10 tasks, User B: 2 tasks | After B is done, A continues alone |
| `TestQueue_FairScheduling_MixedPriorities` | User A: interactive + background, User B: background | A's interactive first, then round-robin on backgrounds |

### Task lifecycle

| Test | Input | Expected |
|---|---|---|
| `TestQueue_TaskStatus_Pending` | Enqueue task, check status before processing | Status: "pending" |
| `TestQueue_TaskStatus_Running` | Start processing, check during | Status: "running", started_at set |
| `TestQueue_TaskStatus_Done` | Task completes | Status: "done", finished_at set, result populated |
| `TestQueue_TaskStatus_Failed` | Task handler returns error | Status: "failed", error field populated |
| `TestQueue_TaskPersistence` | Enqueue task, stop queue, restart, load from DB | Task re-queued from DB, eventually processed |
| `TestQueue_RunningTaskOnRestart` | Task status "running" in DB on startup | Reset to "pending" and re-queued (crashed mid-execution) |

### Subscription

| Test | Input | Expected |
|---|---|---|
| `TestQueue_Subscribe_ReceivesOwnEvents` | User A subscribes, A's task completes | A receives TaskEvent |
| `TestQueue_Subscribe_NoOtherUserEvents` | User A subscribes, B's task completes | A does NOT receive event |
| `TestQueue_Subscribe_MultipleSubscribers` | User A subscribes twice | Both channels receive events |
| `TestQueue_Subscribe_UnsubscribeOnCancel` | Subscribe with context, cancel context | Channel closed, no goroutine leak |

### Context cancellation

| Test | Input | Expected |
|---|---|---|
| `TestQueue_Run_ContextCancel` | Start Run, cancel context | Run returns, no goroutine leak |
| `TestQueue_Enqueue_ContextCancel` | Cancel context, then enqueue | Error: context cancelled |
| `TestQueue_DrainOnShutdown` | 5 tasks queued, cancel context | Currently running task finishes, pending tasks remain in DB |

---

## 17. internal/ai/embedder

File: `internal/ai/embedder_test.go`

### Chunking

| Test | Input | Expected |
|---|---|---|
| `TestChunk_ShortText` | 100-word text | 1 chunk (below threshold, no splitting) |
| `TestChunk_LongText` | 2000-word text | Multiple chunks, ~512 tokens each |
| `TestChunk_Overlap` | Long text | Consecutive chunks share overlap region |
| `TestChunk_EmptyText` | "" | 0 chunks |
| `TestChunk_ExactBoundary` | Text exactly at chunk size | 1 chunk (no split needed) |
| `TestChunk_SplitOnParagraph` | Long text with paragraphs | Chunks split at paragraph boundaries when possible (not mid-sentence) |
| `TestChunk_PreservesOrder` | Long text | Chunk[0] is the beginning, Chunk[N] is the end |

### Embedding generation

| Test | Input | Expected |
|---|---|---|
| `TestEmbedder_ProcessTask_ShortNote` | Note with short body | 1 embedding stored in ChromaDB with ID: "{note_id}_0" |
| `TestEmbedder_ProcessTask_LongNote` | Note with long body (3 chunks) | 3 embeddings stored: "{id}_0", "{id}_1", "{id}_2" |
| `TestEmbedder_ProcessTask_DeleteOldChunks` | Note already had 5 chunks, now has 3 | Old 5 deleted, new 3 inserted |
| `TestEmbedder_ProcessTask_NoteNotFound` | Task references deleted note | Task fails gracefully, marked as failed |
| `TestEmbedder_ProcessTask_OllamaDown` | Mock returns error | Task fails, retryable error (stays in queue for retry) |
| `TestEmbedder_ProcessTask_ChromaDown` | Mock returns error | Task fails, retryable error |
| `TestEmbedder_ProcessTask_Metadata` | Embed a note | ChromaDB documents include metadata: {note_id, chunk_index, title} |
| `TestEmbedder_DeleteTask` | Process delete task for note | All chunks removed from ChromaDB |

---

## 18. internal/search/semantic

File: `internal/search/semantic_test.go`

Uses mocked Ollama and ChromaDB clients.

| Test | Input | Expected |
|---|---|---|
| `TestSemantic_Search_Success` | Query "caching strategies", mock returns 5 chunks from 3 notes | 3 results (deduplicated by note), ranked by best chunk score |
| `TestSemantic_Search_Deduplication` | 3 chunks from same note match | 1 result for that note, score is best of 3 |
| `TestSemantic_Search_NoResults` | Mock returns empty | Empty slice |
| `TestSemantic_Search_EmptyQuery` | "" | Error: "query is required" |
| `TestSemantic_Search_OllamaError` | Embedding call fails | Error propagated |
| `TestSemantic_Search_ChromaError` | ChromaDB query fails | Error propagated |
| `TestSemantic_Search_ResultsIncludeMetadata` | Successful search | Each result has: note_id, title, score, content_snippet |
| `TestSemantic_Handler_Success` | `GET /api/search/semantic?q=test` | 200, results JSON |
| `TestSemantic_Handler_MissingQuery` | `GET /api/search/semantic` | 400 |
| `TestSemantic_Handler_Unauthorized` | No auth header | 401 |

---

## 19. internal/ai/chat

File: `internal/ai/chat_test.go`

### RAG pipeline

| Test | Input | Expected |
|---|---|---|
| `TestChat_ProcessQuery_RetrievesContext` | Query "API design" | Embeds query, queries ChromaDB, gets relevant chunks |
| `TestChat_ProcessQuery_PromptConstruction` | 3 chunks retrieved | System prompt + chunk context + user query assembled correctly |
| `TestChat_ProcessQuery_PromptIncludesCitations` | Chunks from notes A, B | Prompt includes note titles as source references |
| `TestChat_ProcessQuery_StreamsResponse` | Mock Ollama streams 10 tokens | 10 `chat.stream` events sent, then `chat.done` |
| `TestChat_ProcessQuery_Citations` | Response cites notes A, B | `chat.done` payload includes citation IDs |
| `TestChat_ProcessQuery_EmptyQuery` | "" | Error |
| `TestChat_ProcessQuery_NoRelevantContext` | ChromaDB returns 0 results | Response indicates "no relevant notes found" |

### Conversation memory

| Test | Input | Expected |
|---|---|---|
| `TestChat_ConversationMemory_FollowUp` | Ask Q1, then Q2 | Q2's prompt includes Q1 and A1 in message history |
| `TestChat_ConversationMemory_Limit` | Ask 10 questions (memory limit 5) | Only last 5 turns in prompt |
| `TestChat_ConversationMemory_PerUser` | User A asks Q, User B asks Q | Separate memory (A's history not in B's prompt) |
| `TestChat_ConversationMemory_Reset` | Send reset command | Memory cleared, next query has no history |

### System prompt

| Test | Input | Expected |
|---|---|---|
| `TestChat_SystemPrompt_Content` | Inspect constructed prompt | System message tells LLM to only answer from provided notes, not use general knowledge |
| `TestChat_SystemPrompt_ChunkFormat` | 3 chunks in context | Each chunk prefixed with source note title and ID |

---

## 20. internal/ai/synthesizer

File: `internal/ai/synthesizer_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestSynthesize_ProjectScope` | Scope: project, project_id: X | Retrieves all notes in project X, constructs synthesis prompt |
| `TestSynthesize_TagScope` | Scope: tag, tag: "architecture" | Retrieves all notes with tag "architecture" |
| `TestSynthesize_CustomPrompt` | Prompt: "what are the key decisions?" | User's prompt included in LLM request |
| `TestSynthesize_NoNotesInScope` | Project with 0 notes | Error: "no notes found in scope" |
| `TestSynthesize_StreamsResponse` | Mock Ollama streams | Tokens pushed via WebSocket |
| `TestSynthesize_LargeScope` | 50 notes in project | Notes truncated/summarized to fit context window (define strategy and test) |
| `TestSynthesize_InvalidScope` | Scope: "invalid" | Error: "invalid scope" |
| `TestSynthesize_MissingProjectID` | Scope: "project", no project_id | Error |
| `TestSynthesize_MissingTag` | Scope: "tag", no tag | Error |

---

## 21. internal/ai/linker

File: `internal/ai/linker_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestLinker_Suggest_Success` | Note A saved, similar notes B, C, D exist | Returns suggestions to link to B, C, D with reasons |
| `TestLinker_Suggest_NoSimilarNotes` | Note with no related content | Empty suggestions |
| `TestLinker_Suggest_AlreadyLinked` | Note A already links to B | B NOT suggested (filter existing links) |
| `TestLinker_Suggest_PushToWebSocket` | Suggestions generated | `note.link_suggestions` event pushed via WebSocket |
| `TestLinker_Suggest_LLMFormat` | Inspect prompt to LLM | Prompt includes the saved note's content and candidates |
| `TestLinker_Suggest_ParseLLMResponse` | LLM returns structured suggestions | Correctly parsed into list of {target_id, title, reason} |
| `TestLinker_Suggest_MalformedLLMResponse` | LLM returns garbage | Graceful failure, empty suggestions (not crash) |

---

## 22. internal/capture

### URL capture

File: `internal/capture/url_test.go`

Uses `httptest.NewServer` to mock target URLs.

| Test | Input | Expected |
|---|---|---|
| `TestURLCapture_ExtractTitle` | Page with `<title>My Page</title>` | Title: "My Page" |
| `TestURLCapture_ExtractTitleFromOG` | Page with `<meta property="og:title" content="OG Title">` | Falls back to og:title if no `<title>` |
| `TestURLCapture_NoTitle` | Page with no title tag | Title: URL hostname or "Untitled" |
| `TestURLCapture_ExtractContent_Article` | Page with `<article>` | Content from article tag |
| `TestURLCapture_ExtractContent_Main` | Page with `<main>` but no `<article>` | Content from main tag |
| `TestURLCapture_ExtractContent_Body` | Page with only `<body>` | Content from body, tags stripped |
| `TestURLCapture_StripHTML` | Content with `<b>bold</b>` | Returns "bold" (HTML stripped) |
| `TestURLCapture_CreatesNote` | Valid URL | Note created in Inbox with source_url in frontmatter |
| `TestURLCapture_InvalidURL` | "not a url" | Error: "invalid URL" |
| `TestURLCapture_ServerDown` | URL to non-existent server | Error: connection failed |
| `TestURLCapture_Timeout` | Mock server delays 30s | Error: timeout |
| `TestURLCapture_404Page` | Mock returns 404 | Error: "page not found" |
| `TestURLCapture_LargePage` | Mock returns 10MB page | Content truncated to reasonable limit |
| `TestURLCapture_EncodingUTF8` | UTF-8 page | Content parsed correctly |
| `TestURLCapture_EncodingLatin1` | Latin-1 encoded page | Content converted to UTF-8 or handled gracefully |
| `TestURLCapture_RedirectFollowed` | Mock returns 301 -> 200 | Final page captured |
| `TestURLCapture_PrivateIP` | URL to 127.0.0.1 or 192.168.x.x | Error: "private IP not allowed" (SSRF prevention) |
| `TestURLCapture_Handler_Success` | `POST /api/capture {"type":"url","url":"..."}` | 201 |
| `TestURLCapture_Handler_MissingURL` | `POST /api/capture {"type":"url"}` | 400 |

### Voice capture

File: `internal/capture/voice_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestVoiceCapture_Success` | Mock Whisper returns transcription | Note created with transcription body, `transcript_source: true` |
| `TestVoiceCapture_EmptyAudio` | Empty audio data | Error: "audio data is required" |
| `TestVoiceCapture_TranscriptionError` | Mock Whisper returns error | Error propagated |
| `TestVoiceCapture_SummarizationEnqueued` | Successful transcription | Background summarize task enqueued |
| `TestVoiceCapture_Handler_Multipart` | `POST /api/capture` with multipart audio | 201 |
| `TestVoiceCapture_Handler_TooLargeFile` | Audio > max size (e.g., 50MB) | 413 or 400, error about file size |

---

## 23. internal/template

File: `internal/template/service_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestTemplate_List_DefaultTemplates` | No per-user templates | Returns list of built-in templates |
| `TestTemplate_List_UserOverride` | User has template with same name as default | User's version returned (override) |
| `TestTemplate_List_UserCustom` | User has custom template "my-template" | Included alongside defaults |
| `TestTemplate_Apply_Basic` | Template with `{{date}}` | Date substituted with current date |
| `TestTemplate_Apply_ProjectVar` | Template with `{{project}}` applied in project | Project name substituted |
| `TestTemplate_Apply_NoVars` | Template with no `{{...}}` variables | Returned as-is |
| `TestTemplate_Apply_UnknownVar` | Template with `{{unknown}}` | Left as literal `{{unknown}}` (not crash) |
| `TestTemplate_Apply_MultipleVars` | Template with `{{date}}` and `{{project}}` | Both substituted |
| `TestTemplate_Get_NotFound` | Request non-existent template | `ErrNotFound` |
| `TestTemplate_Handler_List` | `GET /api/templates` | 200, array of template names |
| `TestTemplate_Handler_CreateWithTemplate` | `POST /api/notes {"template":"meeting-notes","project_id":"..."}` | Note created from template with vars substituted |

---

## 24. internal/graph

File: `internal/graph/service_test.go`

| Test | Input | Expected |
|---|---|---|
| `TestGraph_AllNodesAndEdges` | 5 notes, A->B, A->C, B->C links | 5 nodes, 3 edges |
| `TestGraph_NodeMetadata` | Get graph | Each node has: id, title, project, tags, created_at |
| `TestGraph_EdgeFromLinks` | A->B via `[[B]]` | Edge: {source: A, target: B} |
| `TestGraph_DanglingLink` | A links to non-existent note | Edge with target: null (or exclude — define behavior) |
| `TestGraph_FilterByProject` | 5 notes across 2 projects, filter by project A | Only nodes in project A and their edges |
| `TestGraph_FilterByTag` | Notes with various tags, filter by "arch" | Only notes with tag "arch" |
| `TestGraph_FilterByDateRange` | Notes from Jan-Dec, filter Mar-May | Only notes from that range |
| `TestGraph_CombinedFilters` | Filter by project AND tag | Intersection |
| `TestGraph_EmptyGraph` | No notes | Empty nodes and edges arrays |
| `TestGraph_Limit` | 1000 notes, limit=100 | 100 nodes returned (most recent or most connected — define) |
| `TestGraph_SelfLink` | Note links to itself | Edge with source == target |
| `TestGraph_Bidirectional` | A->B and B->A | 2 separate edges |
| `TestGraph_Handler_Success` | `GET /api/graph` | 200, `{"nodes":[...],"edges":[...]}` |
| `TestGraph_Handler_WithFilters` | `GET /api/graph?project=X&tag=Y` | 200, filtered |
| `TestGraph_Handler_Unauthorized` | No auth | 401 |

---

## 25. Security Tests

These are cross-cutting tests that don't live in a single package. Put them in `internal/security_test.go` (or a `security` package with `//go:build integration`).

### Path traversal

| Test | Input | Expected |
|---|---|---|
| `TestSecurity_PathTraversal_NoteFilePath` | Create note with file_path `../../etc/passwd` | Rejected at service layer |
| `TestSecurity_PathTraversal_ProjectSlug` | Create project with slug `../secrets` | Rejected |
| `TestSecurity_PathTraversal_NoteTitle` | Title containing `/` or `\` | Sanitized or rejected |
| `TestSecurity_PathTraversal_AbsolutePath` | File path `/etc/passwd` | Rejected |
| `TestSecurity_PathTraversal_NullByte` | File path with `\x00` | Rejected |
| `TestSecurity_PathTraversal_EncodedDots` | Path with `%2e%2e%2f` | Rejected (no URL-decoding tricks) |
| `TestSecurity_PathTraversal_UnicodeNormalization` | Path with unicode equivalents of `..` | Rejected |

### User isolation

| Test | Input | Expected |
|---|---|---|
| `TestSecurity_UserIsolation_NoteAccess` | User A creates note, User B requests it by ID | 404 (not 403) |
| `TestSecurity_UserIsolation_ProjectAccess` | User A's project, User B requests | 404 |
| `TestSecurity_UserIsolation_SearchResults` | User A has note "secret", User B searches "secret" | B gets no results |
| `TestSecurity_UserIsolation_SemanticSearch` | User A has embedded note, User B does semantic search | B gets no results from A's collection |
| `TestSecurity_UserIsolation_Backlinks` | Note A (user 1) links to name matching note B (user 2) | User 2's backlinks don't show user 1's note |
| `TestSecurity_UserIsolation_GraphData` | User A requests graph | Only A's notes and links, never B's |
| `TestSecurity_UserIsolation_AITasks` | User A's task status | User B cannot see A's task events |
| `TestSecurity_UserIsolation_WebSocket` | User A connected via WS | A does NOT receive B's events |
| `TestSecurity_UserIsolation_FilesystemSeparation` | After creating notes for A and B | Files exist in separate directories, no crossover |
| `TestSecurity_UserIsolation_DirectDBQuery` | Open User A's seam.db, check for any User B data | No cross-contamination |

### Input validation

| Test | Input | Expected |
|---|---|---|
| `TestSecurity_InputValidation_XSSInTitle` | Title: `<script>alert('xss')</script>` | Stored as-is (no server-side sanitization needed for markdown files, but verified not executing in API JSON response) |
| `TestSecurity_InputValidation_SQLInjectionInSearch` | Search query: `'; DROP TABLE notes; --` | No error, no damage, query treated as literal |
| `TestSecurity_InputValidation_SQLInjectionInTag` | Tag filter: `architecture' OR '1'='1` | No error, no extra results |
| `TestSecurity_InputValidation_VeryLongTitle` | 100,000 char title | 400, max length enforced |
| `TestSecurity_InputValidation_VeryLongBody` | 10MB note body | Define max, enforce it |
| `TestSecurity_InputValidation_BinaryDataInBody` | Random binary bytes | Rejected or handled gracefully |
| `TestSecurity_InputValidation_ControlCharsInTitle` | Title with null bytes, tabs, newlines | Sanitized or rejected |
| `TestSecurity_InputValidation_EmptyStrings` | All fields empty | Proper validation errors |

### JWT security

| Test | Input | Expected |
|---|---|---|
| `TestSecurity_JWT_AlgorithmNone` | JWT with `alg: none` | Rejected |
| `TestSecurity_JWT_AlgorithmSwitch` | JWT signed with HS256, verified expecting RS256 (or vice versa) | Rejected (if supporting multiple algorithms) |
| `TestSecurity_JWT_TokenReuse` | Use same access token after it's expired then refreshed | Old token rejected |
| `TestSecurity_JWT_SecretRotation` | Change JWT secret, use old token | Rejected |

### SSRF prevention

| Test | Input | Expected |
|---|---|---|
| `TestSecurity_SSRF_PrivateIPv4` | URL capture: `http://192.168.1.1/admin` | Rejected |
| `TestSecurity_SSRF_Localhost` | URL capture: `http://127.0.0.1:11434/api` | Rejected |
| `TestSecurity_SSRF_IPv6Loopback` | URL capture: `http://[::1]/` | Rejected |
| `TestSecurity_SSRF_MetadataEndpoint` | URL capture: `http://169.254.169.254/` | Rejected |
| `TestSecurity_SSRF_FileProtocol` | URL capture: `file:///etc/passwd` | Rejected |
| `TestSecurity_SSRF_DNSRebinding` | URL resolves to private IP after DNS lookup | Define mitigation and test |

---

## 26. Concurrency Tests

### Database concurrency

| Test | Input | Expected |
|---|---|---|
| `TestConcurrency_SimultaneousNoteCreation` | 10 goroutines each create a note for same user | All 10 created, no corruption, no deadlock |
| `TestConcurrency_ReadWhileWrite` | 1 goroutine writes notes, 5 goroutines read | Reads succeed, return consistent data (WAL mode) |
| `TestConcurrency_SimultaneousFTSUpdate` | 10 goroutines update FTS for different notes | All succeed, FTS is consistent |
| `TestConcurrency_MultipleUsersSimultaneous` | 5 users each performing CRUD concurrently | All operations succeed, no cross-user data leakage |
| `TestConcurrency_UserDBManager_ConcurrentOpen` | 10 goroutines call Open for same user | All get same `*sql.DB`, no race condition |
| `TestConcurrency_ServerDB_ConcurrentAuth` | 10 goroutines register different users simultaneously | All succeed, no duplicate IDs |

### WebSocket concurrency

| Test | Input | Expected |
|---|---|---|
| `TestConcurrency_Hub_SimultaneousRegister` | 100 connections register at once | All tracked correctly |
| `TestConcurrency_Hub_SendDuringRegister` | Send message while new connections register | No race, all registered connections receive |
| `TestConcurrency_Hub_SimultaneousUnregister` | 50 connections unregister at once | All removed, no dangling references |

### AI queue concurrency

| Test | Input | Expected |
|---|---|---|
| `TestConcurrency_Queue_MultipleEnqueues` | 3 users enqueue 10 tasks each simultaneously | All 30 tasks enqueued, none lost |
| `TestConcurrency_Queue_SubscribeWhileProcessing` | Subscribe while tasks are being processed | Subscriber receives events for tasks completing after subscription |

### File watcher concurrency

| Test | Input | Expected |
|---|---|---|
| `TestConcurrency_Watcher_RapidFileChanges` | Write 50 files in quick succession | All eventually indexed, debounce prevents overwhelming |
| `TestConcurrency_Watcher_FileChangesDuringReconciliation` | Files change while reconciliation is running | No data loss, no duplicate entries |

---

## 27. Integration Tests

Build tag: `//go:build integration`

These test full workflows across multiple packages.

### User journey: Note lifecycle

| Test | Steps | Expected |
|---|---|---|
| `TestIntegration_NoteLifecycle` | 1. Register user, 2. Create project via API, 3. Create note in project, 4. Verify file on disk, 5. Edit note, 6. Verify file updated, 7. Search for note, 8. Check backlinks, 9. Delete note, 10. Verify file gone | All steps pass |

### User journey: External edit

| Test | Steps | Expected |
|---|---|---|
| `TestIntegration_ExternalEdit` | 1. Register user, create note via API, 2. Directly edit the .md file on disk, 3. Wait for watcher, 4. GET note via API | API returns updated content from the file edit |

### User journey: Reconciliation

| Test | Steps | Expected |
|---|---|---|
| `TestIntegration_Reconciliation` | 1. Register user, create 3 notes, 2. Stop server, 3. Add 2 new .md files to disk, delete 1, modify 1, 4. Start server, 5. List notes via API | Correct: original 2 + new 2 = 4 notes, all with correct content |

### User journey: Multi-user isolation

| Test | Steps | Expected |
|---|---|---|
| `TestIntegration_MultiUserIsolation` | 1. Register Alice and Bob, 2. Alice creates project + notes, 3. Bob creates different project + notes, 4. Alice searches — sees only her notes, 5. Bob searches — sees only his, 6. Verify filesystem directories are separate | Full isolation |

### User journey: Auth flow

| Test | Steps | Expected |
|---|---|---|
| `TestIntegration_AuthFlow` | 1. Register, 2. Login, get tokens, 3. Access protected endpoint with access token, 4. Wait for access token to expire, 5. Use refresh token to get new access token, 6. Access endpoint with new token | Works end to end |

### WebSocket integration

| Test | Steps | Expected |
|---|---|---|
| `TestIntegration_WebSocket_FileChange` | 1. Connect WebSocket, 2. Create note via API, 3. Edit file on disk, 4. Verify WebSocket receives `note.changed` event | Event received with correct note ID |

---

## 28. Performance Tests

Build tag: `//go:build performance`

These are not run in CI. Run manually to verify performance characteristics.

| Test | Scenario | Target |
|---|---|---|
| `TestPerf_NoteCreation_1000Notes` | Create 1000 notes for one user | < 30 seconds total |
| `TestPerf_Search_1000Notes` | FTS search across 1000 notes | < 100ms per query |
| `TestPerf_List_1000Notes` | List all 1000 notes with filters | < 200ms |
| `TestPerf_Reconciliation_1000Files` | Reconcile 1000 files on startup | < 10 seconds |
| `TestPerf_Reconciliation_500Files_NoChanges` | Reconcile when nothing changed | < 1 second (hash comparison only) |
| `TestPerf_ConcurrentUsers_3Users` | 3 users performing mixed CRUD simultaneously | No request > 500ms, no errors |
| `TestPerf_FTS_Index_1000Notes` | Index 1000 notes into FTS | < 15 seconds |
| `TestPerf_Backlinks_HighlyLinked` | Note linked by 200 other notes | Backlinks query < 50ms |
| `TestPerf_Graph_1000Nodes` | Graph endpoint with 1000 notes, 3000 links | < 300ms |
| `TestPerf_WebSocket_1000Messages` | Push 1000 messages to one connection | < 2 seconds, no dropped messages |
| `TestPerf_SQLiteMemory_1000Notes` | After creating 1000 notes | Per-user DB < 5MB, FTS index < 10MB |

---

## 29. Frontend Tests

Framework: Vitest + React Testing Library

### API client

File: `web/src/api/__tests__/client.test.ts`

| Test | Input | Expected |
|---|---|---|
| `test_apiClient_attachesToken` | Make request with stored token | Authorization header present |
| `test_apiClient_handles401` | API returns 401 | Redirects to login, clears token |
| `test_apiClient_refreshesToken` | Access token expired, refresh token valid | Auto-refreshes and retries request |
| `test_apiClient_refreshFails` | Both tokens expired | Redirects to login |

### Auth store (Zustand)

File: `web/src/stores/__tests__/auth.test.ts`

| Test | Input | Expected |
|---|---|---|
| `test_authStore_login` | Valid credentials | Token stored, isAuthenticated: true |
| `test_authStore_logout` | Call logout | Token cleared, isAuthenticated: false |
| `test_authStore_persistsToken` | Login, reload (mock) | Token survives reload (localStorage) |

### Note editor

File: `web/src/components/__tests__/NoteEditor.test.tsx`

| Test | Input | Expected |
|---|---|---|
| `test_noteEditor_loadsContent` | Render with note ID | API called, content displayed |
| `test_noteEditor_autoSaves` | Type in editor, wait 1.1s | PUT request sent with new content |
| `test_noteEditor_debouncesSave` | Type rapidly for 500ms | Only 1 save request (not per keystroke) |
| `test_noteEditor_showsBacklinks` | Note has 2 backlinks | Backlinks panel shows 2 items |
| `test_noteEditor_wikilinkAutocomplete` | Type `[[` | Autocomplete dropdown appears |

### Project list

File: `web/src/components/__tests__/ProjectList.test.tsx`

| Test | Input | Expected |
|---|---|---|
| `test_projectList_renders` | 3 projects | 3 items + Inbox shown |
| `test_projectList_clickNavigates` | Click project | Navigates to `/projects/{id}` |
| `test_projectList_empty` | 0 projects | Shows only Inbox |

### Quick capture

File: `web/src/components/__tests__/QuickCapture.test.tsx`

| Test | Input | Expected |
|---|---|---|
| `test_quickCapture_opensOnShortcut` | Simulate Ctrl+N | Modal appears |
| `test_quickCapture_savesToInbox` | Enter title + content, click save | POST /api/notes called with no project_id |
| `test_quickCapture_dismissesAfterSave` | Save | Modal closes |
| `test_quickCapture_validatesTitle` | Click save with empty title | Error shown, no request made |

### WebSocket client

File: `web/src/ws/__tests__/client.test.ts`

| Test | Input | Expected |
|---|---|---|
| `test_wsClient_connects` | Valid server URL | Connection established |
| `test_wsClient_authenticates` | Connect with token | First message is JWT |
| `test_wsClient_reconnects` | Connection drops | Auto-reconnects with backoff |
| `test_wsClient_dispatchesEvents` | Receive `note.changed` event | Zustand store updated |
| `test_wsClient_chatStream` | Receive multiple `chat.stream` events | Tokens accumulated in order |

### Search

File: `web/src/components/__tests__/Search.test.tsx`

| Test | Input | Expected |
|---|---|---|
| `test_search_fullText` | Type query, press Enter | GET /api/search?q=... called, results displayed |
| `test_search_semantic` | Toggle to semantic, type query | GET /api/search/semantic?q=... called |
| `test_search_clickResult` | Click a search result | Navigates to `/notes/{id}` |
| `test_search_noResults` | Search returns empty | "No results found" message |

---

## Appendix: Test Count Summary

| Package | Unit Tests | Integration | Security | Performance |
|---|---|---|---|---|
| config | ~16 | - | - | - |
| auth/store | ~16 | - | - | - |
| auth/jwt | ~11 | - | 4 | - |
| auth/handler | ~21 | 1 | - | - |
| userdb | ~12 | 3 | - | - |
| note/frontmatter | ~22 | - | - | - |
| note/wikilink | ~20 | - | - | - |
| note/tag | ~17 | - | - | - |
| project | ~25 | - | - | - |
| note (store) | ~33 | - | - | - |
| note (service) | ~27 | - | - | - |
| note (handler) | ~21 | - | - | - |
| search/fts | ~18 | - | 2 | 2 |
| watcher | ~15 | 10 | - | 2 |
| ws | ~18 | 1 | - | 1 |
| ai/ollama | ~13 | - | - | - |
| ai/chroma | ~13 | - | - | - |
| ai/queue | ~18 | - | - | - |
| ai/embedder | ~10 | - | - | - |
| search/semantic | ~10 | - | - | - |
| ai/chat | ~10 | - | - | - |
| ai/synthesizer | ~9 | - | - | - |
| ai/linker | ~7 | - | - | - |
| capture | ~18 | - | 6 | - |
| template | ~10 | - | - | - |
| graph | ~14 | - | - | 1 |
| security (cross-cutting) | - | - | ~25 | - |
| concurrency | - | ~12 | - | - |
| integration | - | 6 | - | - |
| performance | - | - | - | 11 |
| **frontend** | ~25 | - | - | - |
| **Total** | **~425** | **~33** | **~37** | **~17** |

**Grand total: ~512 tests**

---

*Last updated: 2026-03-08*
