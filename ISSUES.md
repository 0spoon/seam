# Implementation Audit Issues

Historical tracker of code audits. All previously logged issues are resolved.
This file is a compacted summary of prior passes -- see git history for full
per-issue detail.

## Audit Passes

| # | Date       | Scope                                              | Issues | All fixed |
|---|------------|----------------------------------------------------|--------|-----------|
| 1 | 2026-03-15 | Original audit: Temporal RAG, Tasks, Webhooks      | 27     | yes       |
| 2 | 2026-03-15 | Post-fix review of edits from pass 1               | 7      | yes       |
| 3 | 2026-03-15 | Test failures from interface changes               | 4      | yes       |
| 4 | 2026-03-15 | Pre-existing issues (auth, note, ai, ws)           | 16     | yes (1 by design) |
| 5 | 2026-03-15 | First full source scan                             | 13     | yes       |
| 6 | 2026-03-15 | Second full source scan                            | 39     | yes       |
| 7 | 2026-03-15 | Third full source scan + SCAN2 verification        | 17     | yes       |
| 8 | 2026-03-15 | LLM provider review (OpenAI + Anthropic)           | 10     | yes       |
| 9 | 2026-03-15 | Fourth full scan (backend + frontend)              | 38     | yes       |
| 10| 2026-03-15 | Migration flattening + single-DB consolidation     | 6      | yes       |
| 11| 2026-03-15 | Fifth full source scan                             | 35     | yes       |
| 12| 2026-03-16 | Phase 1: agentic assistant implementation          | 18     | yes (1 by design) |
| 13| 2026-03-16 | Phase 2: user profile + long-term memory           | 9      | yes       |
| 14| 2026-04-06 | Phase 3: scheduler/briefing + recent-code audit    | 16     | yes       |
|   |            | **Total**                                          | **255**| **all resolved** |

## Recurring Issue Patterns

These themes showed up across multiple passes. Use them as a checklist when
writing new code or auditing.

### Panics from `ulid.MustNew`
Replaced everywhere with `ulid.New` + error handling. Sites included
`note/service.go`, `note/version_store.go`, `task/service.go`,
`webhook/service.go`, `auth/service.go`, `auth/store.go`, `ai/queue.go`,
`agent/service.go`, `chat/service.go`, `mcp/logging.go`, `project/service.go`,
and `cmd/seed/main.go`. `make lint` now enforces this. Always use the
fallible variant in any non-startup path.

### UTF-8 byte slicing (rune safety)
`s[:N]` splits multi-byte characters. Fixed across `ai/embedder.go`,
`ai/suggest.go`, `ai/linker.go`, `ai/chat.go`, `ai/openai.go`,
`ai/anthropic.go`, `agent/service.go`, `agent/briefing.go`, `capture/service.go`,
`note/handler.go`, `mcp/logging.go`, `cmd/seam/search.go`, `cmd/seam/main_screen.go`,
`cmd/seam/ai_assist.go`, `cmd/seam/voice_capture.go`. Use `[]rune(s)[:N]` or
`utf8.RuneCountInString` whenever truncating user-visible or LLM-bound text.
Watch for byte indices from `strings.Index`/`strings.LastIndex` being compared
against rune-based limits.

### Atomic file writes
Markdown notes are the source of truth. `os.WriteFile` is non-atomic --
crashes mid-write corrupt the file. Fixed in `note.AtomicWriteFile` (now
exported, includes `tmp.Sync` before rename) and applied to `note/service.go`
Create/Update/Reindex/rollback paths, `template/service.go`, and the
`cmd/seamd/main.go` frontmatter updater closure.

### File-system / DB ordering and atomicity
Several handlers performed file I/O before committing the DB transaction,
leaving orphans on commit failure. Fixed in `note.Service.Delete`,
`note.BulkAction` (move + delete), `note.Service.Update` (cross-project move
rollback), `project.Service.Update` (directory rename), `task.Service.ToggleDone`.
Pattern: collect pending FS operations, run them only after `tx.Commit()`
succeeds, and on commit failure clean up any new files written before the
commit attempt.

### Path traversal and symlink safety
Validate every user-controlled path with `validate.PathWithinDir` before
file I/O. Skip symlinks in the watcher (`os.Lstat` + `IsDir()`, see comments
in `internal/watcher/watcher.go`). Sanitize multipart upload extensions via
`filepath.Base`. Validate frontmatter titles in `Reindex` and `restoreVersion`.

### SSRF and webhook delivery
Webhook delivery uses bounded fan-out (semaphore worker pool), validates
event types, has panic recovery in dispatch goroutines, and routes private-IP
deliveries through a Debug log rather than a hard block (local-first design).
`isPrivateIP` + `isDangerousIP` check all DNS-resolved addresses, not just
the first. Webhook secrets are returned only on create and redacted from
audit-log result truncation.

### Error sanitation
Never expose raw provider/DB error text to clients. Use `sanitizeError` in
MCP, `writeProviderError` in AI handlers, sentinels (`ErrRateLimited`,
`ErrAuthFailed`, `ErrModelNotFound`, `ErrInvalidArguments`, etc.) and
`errors.Is` for matching. Avoid `strings.Contains(err.Error(), ...)` --
fixed in `ai/handler.go`, `auth/handler.go`. Generic messages like
"invalid action", "model not found", "synthesis stream failed" are the
default for handler responses.

### `errors.Is` over direct equality
Replaced direct `err == ErrFoo` and `err == sql.ErrNoRows` checks across
`assistant/service.go`, `assistant/memory.go`, `chat/store.go`,
`assistant/handler.go`. Future-proofs against wrapping.

### Sentinel construction
Use `errors.New(...)` for package-level sentinels, not `fmt.Errorf(...)`.
Fixed in `webhook/service.go`, `ws/hub.go`.

### `RowsAffected` and `rows.Err()`
Every store-level UPDATE/DELETE checks `RowsAffected` and returns
`ErrNotFound` when zero. Every `rows.Next()` loop is followed by
`rows.Err()`. Missing in `task.Store.Delete`, `webhook.Store.UpdateSecret`,
`chat.Store.DeleteConversation`, `ai/chat.go`, `ai/linker.go`,
`agent.TouchMemory`, `cmd/seed/main.go`. All fixed.

### Concurrency and lifecycle
Replaced unsynchronized setter fields with `sync.RWMutex`-guarded getters
in `capture.Service`, `note.Handler` (template applier), `task.Service`
(noteSvc), `project.Service` (frontmatterUpdater), `assistant.ConfirmationManager`.
`Close()` methods on `ai.Handler`, `auth.Handler`, `mcp.Server`,
`assistant.Service` use `sync.Once` to prevent double-close panics.
`ai.Queue.LoadPending` no longer holds the mutex during DB operations and
respects `maxQueueSize` backpressure. The scheduler goroutine is now joined
during shutdown via a done channel (matches `aiQueueDone`).

### LIKE / FTS query safety
Escape `_`, `%`, and `\` (in that order, with `ESCAPE '\'`) in LIKE patterns.
Sanitize FTS5 queries by stripping operators and quoting terms (see
`sanitizeMemoryFTSQuery`). Fixed in `chat/store.go SearchMessages`,
`agent/store.go ReconcileChildren`, `assistant/memory.go SearchMemories`,
`note/store.go ResolveLink` (the inverse: don't apply `escapeLIKE` to `=`).

### Tool-use / agentic-loop correctness
The agentic loop exits only when `len(resp.ToolCalls) == 0` (some providers
return tool calls alongside `finish_reason: stop`). Recent-history slicing
walks backwards to avoid orphaning a tool result whose parent assistant
message is outside the window (`safeRecentBoundary`). Confirmation workflow
is real (DB-backed pending actions, approve/reject endpoints).
`ChatStream` runs the loop inline and emits events as they happen, not
post-hoc. Anthropic tool results use the `content` field, not `text`.
OpenAI/Anthropic both surface sanitized sentinels, never raw API error text.

### Memory / decay scoring
`TouchMemories` only fires on relevance-path hits, not on the recency
fallback in `loadContext`. The decay metric in `relevanceScore` depends on
this distinction.

### Briefing / scheduler
Briefings dedupe by today's title (lookup + Update vs Create). Default
schedule provisioning is keyed on a deterministic ID
(`default_daily_briefing`), not the user-mutable name. Timestamps in
briefings render in a single zone (UTC). Scheduler `Update` validates
`cron_expr` + `run_at` mutual exclusion the same way `Create` does.

### Other recurring fixes
- `MaxBytesReader` on every body-decoding handler (`template/handler.go`,
  `capture/handler.go` voice upload).
- `filepath.Join` instead of string concatenation for paths.
- `context.WithoutCancel` for background work spawned from request handlers
  (`capture.Service` summarization callback).
- `slog.Warn` instead of silently swallowing `time.Parse`, `json.Unmarshal`,
  scan errors in store layers.
- Defense-in-depth limit caps in service layers (not just handlers): graph
  service, semantic search.
- `marshalResult` (was `mustMarshal`) propagates marshal errors instead of
  returning a fake `{"error":"..."}` blob to the LLM.
- `recordAction` only sets `ExecutedAt` on success, not on failed actions.

## Architectural Notes

- **Migration flattening + single-DB**: 5 user migrations + 1 server
  migration were collapsed into a single `migrations/001_initial.sql`. The
  separate `server.db` was eliminated -- auth and domain data now share one
  `seam.db` opened once and passed to `auth.NewSQLStore` +
  `userdb.NewSQLManagerWithDB`. `users` was renamed to `owner`, `user_id`
  to `owner_id`. `userdb.Manager` is now a thin shim around the shared DB
  with flat data paths (`{data_dir}/notes/`).
- **External LLM providers**: OpenAI and Anthropic clients live alongside
  Ollama. API keys must come from env vars; the YAML config logs a warning
  if it contains an `api_key` and is group/world-readable.
  `models.chat`/`models.background` are validated whenever any provider is
  active; `models.embeddings` is only validated when Ollama is configured
  (embeddings are always local).
- **Design trade-offs accepted, not bugs**: ToggleDone holds a DB tx during
  file I/O; SyncNote matches duplicate-content tasks by order; deep
  pagination + recency returns empty past `limit*3`; path-traversal checks
  don't resolve symlinks (FilePath comes from internal DB only); no
  file-level locking in `toggleCheckboxInFile` (mitigated by single-user
  design).
