# Implementation Audit Issues

Audit of features #3 (Temporal RAG), #4 (Task Tracking), #7 (Webhooks) implemented on 2026-03-15.
All issues are verified against the actual code. Fix in priority order (Critical > High > Medium > Low).

---

## CRITICAL

### C1. Task: `toggleCheckboxInFile` frontmatter offset is off-by-one

**File:** `internal/task/service.go`, lines 247-255
**Impact:** Toggles the WRONG checkbox line in every note with frontmatter.

The frontmatter end-detection logic is wrong:

```go
endIdx := strings.Index(fileStr[3:], "---")
if endIdx >= 0 {
    bodyStart = endIdx + 3 + 3 // skip opening "---" + offset + closing "---"
    fmLines := strings.Count(fileStr[:bodyStart], "\n")
    lineNumber = lineNumber + fmLines
}
```

For a file like:
```
---\n
title: x\n
---\n
body here\n
```

- `fileStr[3:]` = `\ntitle: x\n---\nbody here\n`
- `endIdx` = 11 (position of second `---` in substring)
- `bodyStart` = 11 + 3 + 3 = 17
- `strings.Count(fileStr[:17], "\n")` = 2

But the frontmatter occupies 3 lines (`---`, `title: x`, `---`). The body starts on line 4, so we need `fmLines = 3`, not 2. The line number will consistently be off by one.

**Additional sub-bug:** `strings.Index(fileStr[3:], "---")` matches any `---` inside frontmatter values (e.g., a title containing `---`), not just the closing delimiter. Should match `\n---\n` or `\n---` at end-of-string instead.

**Fix:** Rewrite the frontmatter detection to properly find the closing `---` at the start of a line and count the newline after the closing delimiter:

```go
// Find closing "---" that starts on its own line.
rest := fileStr[3:] // skip opening "---"
// Skip past the newline after opening "---"
nlIdx := strings.Index(rest, "\n")
if nlIdx < 0 {
    // No frontmatter body, just "---" with no closing
    return // no adjustment needed
}
rest = rest[nlIdx+1:]
endIdx := strings.Index(rest, "\n---\n")
if endIdx < 0 {
    // Try end-of-file: "\n---" at the very end
    if strings.HasSuffix(rest, "\n---") {
        endIdx = len(rest) - 4
    }
}
if endIdx >= 0 {
    // bodyStart = opening "---" (3) + first newline (nlIdx+1) + content up to closing delimiter + "\n---\n" (4)
    bodyStart = 3 + nlIdx + 1 + endIdx + 4
    fmLines := strings.Count(fileStr[:bodyStart], "\n")
    lineNumber = lineNumber + fmLines
}
```

Alternatively, count lines by splitting on `\n` and finding the closing `---` line index, which is simpler and less error-prone.

---

### C2. Webhook: Secret never returned to the user

**File:** `internal/webhook/store.go`, line 22; `internal/webhook/handler.go`, line 77
**Impact:** The HMAC signing feature is completely unusable. Users cannot verify webhook signatures.

The `Secret` field has `json:"-"` tag:
```go
Secret string `json:"-"`
```

When the handler returns the created webhook via `writeJSON(w, http.StatusCreated, wh)`, the secret is omitted from the JSON response. The user has no way to learn the HMAC signing secret.

**Fix:** Create a `CreateResponse` struct that includes the secret, used only in the create handler response:

```go
// In handler.go, after creating the webhook:
type createResponse struct {
    *Webhook
    Secret string `json:"secret"`
}
writeJSON(w, http.StatusCreated, createResponse{Webhook: wh, Secret: wh.Secret})
```

Or use a dedicated response struct in `store.go`:
```go
type WebhookWithSecret struct {
    Webhook
    Secret string `json:"secret"`
}
```

The secret should ONLY be returned in the create response, never in list/get responses (current behavior for those is correct).

---

### C3. Search: Recency re-ranking applied AFTER SQL pagination

**File:** `internal/search/fts.go`, lines 196-237 (`SearchWithRecency`) and lines 243-323 (`SearchScopedWithRecency`)
**Impact:** Recency-adjusted results are drawn only from the BM25-top-N, not the full result set. A note ranked #101 by BM25 but updated 5 minutes ago could be the true #1 after recency adjustment, but it's never fetched.

The SQL query applies `LIMIT ? OFFSET ?` based on pure BM25 rank, then recency adjustment is applied in Go, then results are re-sorted.

**Fix:** Fetch a larger window from SQL (e.g., `limit * 3` or remove pagination from the inner query), apply recency adjustment, sort, then paginate in Go:

```go
// Fetch a larger window for re-ranking.
fetchLimit := limit * 3
if fetchLimit > 500 {
    fetchLimit = 500
}

// SQL query uses fetchLimit, offset=0
rows, err := db.QueryContext(ctx,
    `SELECT ... ORDER BY rank LIMIT ?`, fetchLimit)
// ... scan all results ...
// Apply recency adjustment
// Re-sort
// Slice to [offset:offset+limit] in Go
// Return sliced results with adjusted total
```

The `total` count should still come from the COUNT query (unchanged), but note that it reflects un-adjusted totals. Consider documenting this behavior.

---

## HIGH

### H1. Webhook: No panic recovery in Dispatch goroutines

**File:** `internal/webhook/service.go`, lines 286-336
**Impact:** A panic in `deliver`, `matchesFilter`, `json.Marshal` (cyclic payload), or `ulid.MustNew` crashes the entire server process.

The outer goroutine (line 286) and inner per-webhook goroutines (line 329) have no `recover()`.

**Fix:** Add deferred recover at the top of both goroutines:

```go
// Outer goroutine (line 286):
go func() {
    defer func() {
        if r := recover(); r != nil {
            s.logger.Error("webhook.Service.Dispatch: panic recovered",
                "panic", r, "user_id", userID, "event_type", eventType)
        }
    }()
    // ... existing code ...

// Inner goroutine (line 329):
go func(wh *Webhook) {
    defer wg.Done()
    defer func() {
        if r := recover(); r != nil {
            s.logger.Error("webhook.Service.deliver: panic recovered",
                "panic", r, "webhook_id", wh.ID)
        }
    }()
    s.deliver(bgCtx, db, wh, eventType, payloadJSON)
}(wh)
```

Also replace `ulid.MustNew` with `ulid.New` + error handling in `recordDelivery` (line 389) and `Create` (line 148).

---

### H2. Webhook: Concurrent SQLite writes from Dispatch goroutines

**File:** `internal/webhook/service.go`, lines 329-333
**Impact:** Multiple goroutines call `recordDelivery` concurrently, each doing `INSERT INTO webhook_deliveries`. Can cause `SQLITE_BUSY` errors under load.

Multiple inner goroutines each call `s.deliver()` which calls `s.recordDelivery()` doing an INSERT, all sharing the same `*sql.DB`.

**Fix:** Serialize delivery recording. Options:
1. Collect delivery results from goroutines via a channel, then record them sequentially in the outer goroutine after `wg.Wait()`.
2. Use a mutex around `recordDelivery`.
3. Deliver sequentially (simplest, acceptable given 10s timeout per webhook).

Recommended approach (option 1):
```go
type deliveryResult struct {
    webhookID  string
    eventType  string
    payload    string
    statusCode int
    response   string
    errText    string
    duration   time.Duration
}

results := make(chan deliveryResult, len(webhooks))

for _, wh := range webhooks {
    if !s.matchesFilter(wh.Filter, eventPayload) {
        continue
    }
    wg.Add(1)
    go func(wh *Webhook) {
        defer wg.Done()
        dr := s.deliver(bgCtx, wh, eventType, payloadJSON) // return result instead of recording
        results <- dr
    }(wh)
}
wg.Wait()
close(results)

// Record all deliveries sequentially.
for dr := range results {
    s.recordDelivery(bgCtx, db, dr)
}
```

---

### H3. Task: `SyncNote` regenerates all task IDs on every note save

**File:** `internal/task/service.go`, lines 120-141
**Impact:** Task IDs change on every sync. External references (bookmarks, API consumers tracking tasks by ID) break. `created_at` also resets every time.

The current approach is delete-all + re-insert with new ULIDs:
```go
s.store.DeleteByNote(ctx, tx, noteID)
// ... for each parsed task:
t := &Task{
    ID: ulid.MustNew(ulid.Now(), rand.Reader).String(), // NEW ID every time
    // ...
    CreatedAt: now, // RESETS every time
}
s.store.Upsert(ctx, tx, t)
```

**Fix:** Match existing tasks by `(note_id, line_number, content)` tuple. If a match exists, update its `done` status; if not, insert a new task; delete tasks that no longer exist in the note body:

```go
func (s *Service) SyncNote(ctx context.Context, userID, noteID, body string) error {
    // ... open DB, begin tx ...

    parsed := parseTasks(body)

    // Get existing tasks for this note.
    existing, _, _ := s.store.List(ctx, tx, TaskFilter{NoteID: noteID, Limit: 10000})

    // Build lookup map: content -> existing task (for matching).
    existingMap := make(map[string]*Task)
    for _, t := range existing {
        existingMap[t.Content] = t
    }

    // Track which existing tasks we've matched.
    matched := make(map[string]bool)

    for _, p := range parsed {
        if et, ok := existingMap[p.Content]; ok && !matched[et.ID] {
            // Update existing task's line number and done status.
            matched[et.ID] = true
            // UPDATE line_number, done, updated_at WHERE id = et.ID
        } else {
            // Insert new task with new ULID.
        }
    }

    // Delete tasks that no longer exist in the note.
    for _, et := range existing {
        if !matched[et.ID] {
            // DELETE WHERE id = et.ID
        }
    }

    return tx.Commit()
}
```

This preserves task IDs and `created_at` for unchanged tasks.

---

### H4. Task: No transaction around `ToggleDone` Get+UpdateDone+file write

**File:** `internal/task/service.go`, lines 194-220
**Impact:** Race condition between `ToggleDone` and `SyncNote` (from file watcher). DB and file can diverge. File write error is only logged, not returned.

The sequence is: DB read (`Get`) -> DB write (`UpdateDone`) -> file read-modify-write, all without locking.

If two concurrent requests toggle different tasks in the same note file, the file read-modify-write can lose one update (classic TOCTOU). If the file write fails, the DB is updated but the file is not.

**Fix:**
1. Wrap DB operations in a transaction.
2. Return file write errors (not just log them).
3. Consider a per-note file lock (or per-user lock) to prevent concurrent file modifications:

```go
func (s *Service) ToggleDone(ctx context.Context, userID, taskID string, done bool) error {
    db, err := s.dbManager.Open(ctx, userID)
    if err != nil {
        return fmt.Errorf("task.Service.ToggleDone: open db: %w", err)
    }

    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("task.Service.ToggleDone: begin tx: %w", err)
    }
    defer tx.Rollback()

    t, err := s.store.Get(ctx, tx, taskID)
    if err != nil {
        return fmt.Errorf("task.Service.ToggleDone: %w", err)
    }

    if err := s.store.UpdateDone(ctx, tx, taskID, done); err != nil {
        return fmt.Errorf("task.Service.ToggleDone: %w", err)
    }

    // Update file BEFORE committing DB transaction.
    if s.noteSvc != nil {
        if err := s.toggleCheckboxInFile(ctx, userID, t.NoteID, t.LineNumber, done); err != nil {
            return fmt.Errorf("task.Service.ToggleDone: file update: %w", err)
        }
    }

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("task.Service.ToggleDone: commit: %w", err)
    }
    return nil
}
```

Note: The file watcher will fire after the file write and call `SyncNote`, which re-syncs from the file. This is fine as long as `SyncNote` uses the updated file content. However, consider suppressing the watcher event for self-initiated writes (the note service already has a `SetSuppressor` pattern).

---

## MEDIUM

### M1. Webhook: `Dispatch` doesn't validate eventType

**File:** `internal/webhook/service.go`, line 285
**Impact:** If caller passes `%` as eventType, the LIKE query in `ListByEvent` matches ALL webhooks.

`Dispatch(ctx, userID, eventType, eventPayload)` never validates `eventType` against `AllEventTypes`.

**Fix:** Add validation at the top of `Dispatch`:
```go
func (s *Service) Dispatch(ctx context.Context, userID, eventType string, eventPayload interface{}) {
    if !isValidEventType(eventType) {
        s.logger.Warn("webhook.Service.Dispatch: invalid event type", "event_type", eventType)
        return
    }
    // ... rest of method
}
```

---

### M2. Webhook: SSRF - initial request URL not checked at dispatch time

**File:** `internal/webhook/service.go`, line 347
**Impact:** `CheckRedirect` only fires on HTTP 3xx redirects. The initial POST to a private/metadata IP goes through unchecked.

The URL is validated at creation time (warn-only), but at dispatch time the initial request bypasses SSRF checks.

**Fix:** Add private IP check before making the HTTP request in `deliver()`:
```go
func (s *Service) deliver(ctx context.Context, db DBTX, wh *Webhook, eventType string, payloadJSON []byte) {
    // Check target URL for private IPs before delivery.
    parsed, _ := url.Parse(wh.URL)
    if parsed != nil && isPrivateIP(parsed.Hostname()) {
        // For a local-first app, log warning but allow. For stricter security, block:
        s.logger.Debug("webhook delivery to private IP", "url", wh.URL)
        // Uncomment to block: s.recordDelivery(..., "blocked: private IP"); return
    }
    // ... rest of deliver
}
```

---

### M3. Webhook: `isPrivateIP` doesn't check all dangerous addresses

**File:** `internal/webhook/service.go`, lines 478-491
**Impact:** `0.0.0.0` (unspecified) and IPv6-mapped IPv4 like `::ffff:127.0.0.1` bypass the check. Only first DNS result checked.

**Fix:**
```go
func isPrivateIP(host string) bool {
    ip := net.ParseIP(host)
    if ip == nil {
        addrs, err := net.LookupHost(host)
        if err != nil || len(addrs) == 0 {
            return true // fail closed
        }
        // Check ALL resolved addresses, not just the first.
        for _, addr := range addrs {
            resolved := net.ParseIP(addr)
            if resolved != nil && isDangerous(resolved) {
                return true
            }
        }
        return false
    }
    return isDangerous(ip)
}

func isDangerous(ip net.IP) bool {
    return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
        ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
```

---

### M4. MCP: `handleWebhookRegister` leaks raw error details

**File:** `internal/mcp/tools.go`, line 827
**Impact:** `err.Error()` exposed directly to the client, potentially leaking file paths, DB errors, etc.

```go
return mcp.NewToolResultError("webhook_register: " + err.Error()), nil
```

All other handlers use `sanitizeError()` but this one doesn't.

**Fix:** Add webhook error handling to `sanitizeError` and use it:
```go
// In sanitizeError, add cases:
case errors.Is(err, webhook.ErrNotFound):
    return tool + ": not found"
case errors.Is(err, webhook.ErrInvalidURL):
    return tool + ": invalid webhook URL"
case errors.Is(err, webhook.ErrInvalidEventType):
    return tool + ": invalid event type"
case errors.Is(err, webhook.ErrNameRequired):
    return tool + ": name is required"
case errors.Is(err, webhook.ErrURLRequired):
    return tool + ": url is required"
case errors.Is(err, webhook.ErrEventsRequired):
    return tool + ": event_types is required"

// Then in handleWebhookRegister:
return mcp.NewToolResultError(sanitizeError("webhook_register", err)), nil
```

---

### M5. MCP: `tasks_list` passes project slug as ProjectID

**File:** `internal/mcp/tools.go`, line 902
**Impact:** Project filtering in `tasks_list` and `tasks_summary` is completely broken.

The tool definition says the parameter is "Project slug to filter by" but the code sets `filter.ProjectID`. The store joins on `n.project_id = ?` which expects a ULID, not a slug.

```go
filter.ProjectID = req.GetString("project", "")  // BUG: this is a slug, not an ID
```

**Fix options:**

Option A: Add a project slug lookup in the MCP handler:
```go
projectSlug := req.GetString("project", "")
if projectSlug != "" {
    // Look up project by slug to get its ID.
    // This requires adding ProjectService to MCP Config or adding a
    // project slug resolution method to TaskService.
}
```

Option B: Change `TaskFilter` to support slug-based filtering and update the store query:
```go
// In task/store.go TaskFilter:
ProjectSlug string  // filter by project slug (joins projects table)

// In buildFilterClauses:
if filter.ProjectSlug != "" {
    baseFrom += " JOIN notes n ON n.id = t.note_id JOIN projects p ON p.id = n.project_id"
    where = append(where, "p.slug = ?")
    args = append(args, filter.ProjectSlug)
}
```

Option B is simpler and doesn't require adding a new dependency to MCP.

---

### M6. Search: FTS zero-value time on parse failure gets silent recency penalty

**File:** `internal/search/fts.go`, lines 220-225
**Impact:** Notes with unparseable `updated_at` are silently penalized as "infinitely old" instead of neutral treatment.

When `time.Parse` fails, `r.UpdatedAt` remains zero value (`0001-01-01`). `recencyWeight()` returns ~0.0, so the formula `rank / (1 + bias * ~0) = rank / ~1 = rank` applies no recency boost. This is inconsistent with the semantic path which explicitly checks for missing timestamps and skips adjustment.

**Fix:** Skip recency adjustment when timestamp parsing fails:
```go
if t, parseErr := time.Parse(time.RFC3339, updatedAtStr); parseErr == nil {
    r.UpdatedAt = t
    r.Rank = r.Rank / (1 + recencyBias*recencyWeight(r.UpdatedAt))
}
// else: leave r.Rank unchanged (neutral treatment)
```

---

### M7. Search: Score compression destroys ranking for recent notes

**File:** `internal/search/semantic.go`, lines 401-406
**Impact:** Any recent note with score > 0.5 at max bias becomes indistinguishable (all clamped to 1.0).

The formula `score * (1 + recencyBias * recencyWeight(updatedAt))` can produce values up to `score * 2.0`. Line 404 clamps to 1.0. This compresses many recently-updated notes to the same score.

**Fix:** Use a blending formula that preserves relative ordering:
```go
// Option A: Weighted geometric mean
adjustedScore := score * (1 + recencyBias * recencyWeight(updatedAt) * 0.5)
// Max amplification is 1.5x instead of 2x, reducing compression

// Option B: Additive blend (better discrimination)
adjustedScore := score*(1-recencyBias*0.3) + recencyWeight(updatedAt)*recencyBias*0.3
// Blends similarity and recency, never exceeds max(score, weight)
```

---

### M8. Search: No upper bound on semantic search limit in handler

**File:** `internal/search/handler.go`, lines 109-114
**Impact:** Client can pass `limit=100000`, causing `nResults=300000` in semantic searcher, hitting SQLite's 999 bound parameter limit in `batchGetNoteTimestamps`.

FTS handler caps limit at 500 but semantic handler doesn't.

**Fix:** Add the same cap as FTS:
```go
limit := 10 // default
if l := r.URL.Query().Get("limit"); l != "" {
    if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
        limit = parsed
    }
}
if limit > 500 {
    limit = 500
}
```

---

### M9. Task: `toggleCheckboxInFile` no path traversal guard

**File:** `internal/task/service.go`, line 232
**Impact:** If a corrupted note record has `FilePath` containing `..`, `filepath.Join` resolves it to an arbitrary path.

```go
absPath := filepath.Join(notesDir, n.FilePath)
```

**Fix:** Add the standard path traversal check used elsewhere in the project:
```go
absPath := filepath.Join(notesDir, n.FilePath)
// Defense-in-depth: reject paths that escape the notes directory.
if !strings.HasPrefix(absPath, notesDir) {
    return fmt.Errorf("invalid file path: path traversal detected")
}
```

---

### M10. Task: `ulid.MustNew` can panic in server context

**File:** `internal/task/service.go`, line 130; `internal/webhook/service.go`, lines 148 and 389
**Impact:** If `crypto/rand.Reader` fails (extremely rare), `MustNew` panics, crashing the server.

**Fix:** Replace with error-handling variant:
```go
id, err := ulid.New(ulid.Now(), rand.Reader)
if err != nil {
    return fmt.Errorf("generate ULID: %w", err)
}
t.ID = id.String()
```

Apply in all three locations.

---

### M11. Task: `parseTasks` doesn't handle `\r\n` line endings

**File:** `internal/task/service.go`, line 21
**Impact:** On Windows-style files, `(.+)$` captures trailing `\r` in content. `strings.Split(body, "\n")` leaves `\r` on lines.

**Fix:** Normalize line endings before parsing:
```go
func parseTasks(body string) []parsedTask {
    body = strings.ReplaceAll(body, "\r\n", "\n")
    body = strings.ReplaceAll(body, "\r", "\n")
    // ... rest of function
}
```

Also in `toggleCheckboxInFile`:
```go
content, err := os.ReadFile(absPath)
fileStr := strings.ReplaceAll(string(content), "\r\n", "\n")
// ... rest of function
// Write back with original line endings if needed
```

---

### M12. Search: Invalid `recency_bias` silently ignored

**File:** `internal/search/handler.go`, lines 68-72
**Impact:** `recency_bias=2.0` or `recency_bias=abc` silently falls back to 0 instead of returning 400.

```go
if rb := r.URL.Query().Get("recency_bias"); rb != "" {
    if parsed, err := strconv.ParseFloat(rb, 64); err == nil && parsed >= 0 && parsed <= 1 {
        recencyBias = parsed
    }
}
```

**Fix:** Return 400 for invalid values:
```go
if rb := r.URL.Query().Get("recency_bias"); rb != "" {
    parsed, err := strconv.ParseFloat(rb, 64)
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid recency_bias: must be a number")
        return
    }
    if parsed < 0 || parsed > 1 {
        writeError(w, http.StatusBadRequest, "invalid recency_bias: must be between 0.0 and 1.0")
        return
    }
    recencyBias = parsed
}
```

---

### M13. Webhook: `webhookSvc` nil during startup reconciliation

**File:** `cmd/seamd/main.go`, lines 308, 413, 621
**Impact:** Webhooks don't fire during startup reconciliation. Not a crash (nil guard protects), but silent behavior gap.

The closure `fileHandler` (line 308) references `webhookSvc` declared at line 621. During reconciliation (line 451), `webhookSvc` is still nil.

**Fix:** Move webhook component creation (lines 619-622) before the `fileHandler` closure definition (line 308). Alternatively, use a `var webhookSvc *webhook.Service` declaration before the closure, then assign later:

```go
// Before the closure (around line 307):
var webhookSvc *webhook.Service

// ... fileHandler closure references webhookSvc ...

// Later (current line 621), change := to =
webhookSvc = webhook.NewService(webhookStore, userDBMgr, logger)
```

---

## LOW

### L1. Task: `?done=banana` silently treated as `done=false`

**File:** `internal/task/handler.go`, lines 52-55
**Impact:** Confusing behavior; should return 400 for invalid boolean values.

**Fix:**
```go
if doneParam != "" {
    switch doneParam {
    case "true":
        d := true
        filter.Done = &d
    case "false":
        d := false
        filter.Done = &d
    default:
        writeError(w, http.StatusBadRequest, "done must be 'true' or 'false'")
        return
    }
}
```

---

### L2. Task: Missing composite index `(done, updated_at)`

**File:** `migrations/user/003_tasks.sql`
**Impact:** The query `ORDER BY t.done ASC, t.updated_at DESC` can't use separate indexes efficiently.

**Fix:** Add a new migration (or modify 003 if not yet deployed):
```sql
CREATE INDEX IF NOT EXISTS idx_tasks_done_updated ON tasks(done, updated_at DESC);
```

---

### L3. Webhook: No delivery retention/cleanup policy

**File:** `migrations/user/004_webhooks.sql`
**Impact:** `webhook_deliveries` table grows without bound.

**Fix:** Add a cleanup method to the service, called periodically or on new delivery:
```go
func (s *Service) CleanupDeliveries(ctx context.Context, userID string, retentionDays int) error {
    db, _ := s.dbManager.Open(ctx, userID)
    cutoff := time.Now().AddDate(0, 0, -retentionDays).Format(time.RFC3339)
    _, err := db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE created_at < ?`, cutoff)
    return err
}
```

---

### L4. Webhook: Store.Update doesn't allow secret rotation

**File:** `internal/webhook/store.go`, lines 146-151
**Impact:** Once created, a webhook secret can never be rotated.

**Fix:** Add a `RotateSecret` method to the service:
```go
func (s *Service) RotateSecret(ctx context.Context, userID, id string) (string, error) {
    // Generate new secret, update in DB, return new secret
}
```

---

### L5. Search: `batchGetNoteTimestamps` silently swallows all DB errors

**File:** `internal/search/semantic.go`, lines 283-333
**Impact:** If the DB is unavailable, all notes silently get no recency adjustment. No signal in logs beyond a debug/warn message.

**Fix:** At minimum, log at Warn level (not Debug). Consider returning an error to the caller so it can decide whether to proceed without timestamps.

---

### L6. Task/Webhook: `time.Parse` errors silently discarded in stores

**Files:**
- `internal/task/store.go`, lines 143-144, 241-242
- `internal/webhook/store.go`, lines 285-286, 305-306, 263

**Impact:** Malformed timestamps in DB silently produce zero-value `time.Time`. No log, no signal.

**Fix:** Log parse errors at Warn level:
```go
if t, err := time.Parse(time.RFC3339, createdAt); err != nil {
    slog.Warn("failed to parse timestamp", "value", createdAt, "error", err)
} else {
    task.CreatedAt = t
}
```

---

### L7. Webhook: `listEvents` endpoint has no auth check

**File:** `internal/webhook/handler.go`, lines 104-106
**Impact:** Inconsistent with all other webhook endpoints which check `userID`.

```go
func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, AllEventTypes)
}
```

**Fix:** The route is mounted under the auth middleware group, so auth IS enforced at the router level. However, for consistency and defense-in-depth, add the `userID` check:
```go
func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
    if reqctx.UserIDFromContext(r.Context()) == "" {
        writeError(w, http.StatusUnauthorized, "missing user identity")
        return
    }
    writeJSON(w, http.StatusOK, AllEventTypes)
}
```

---

### L8. MCP: `mcpSrv.Close()` never called in shutdown sequence

**File:** `cmd/seamd/main.go`
**Impact:** The limiter eviction goroutine leaks until process exit. Not harmful in production but incorrect for clean shutdown and could matter in tests.

**Fix:** Add `mcpSrv.Close()` to the shutdown sequence in main.go, alongside other cleanup calls.

---

### L9. Webhook: `matchesFilter` silently passes untyped payloads

**File:** `internal/webhook/service.go`, lines 413-414
**Impact:** If `eventPayload` is a struct (not `map[string]interface{}`), filter is bypassed -- webhook fires regardless of configured filters.

```go
data, ok := eventPayload.(map[string]interface{})
if !ok {
    return true // cannot inspect, let it through
}
```

**Fix:** Convert structs to maps via JSON round-trip, or document that payloads must be `map[string]interface{}`:
```go
data, ok := eventPayload.(map[string]interface{})
if !ok {
    // Try JSON round-trip for struct payloads.
    b, err := json.Marshal(eventPayload)
    if err != nil {
        return true
    }
    if err := json.Unmarshal(b, &data); err != nil {
        return true
    }
}
```

---

## Summary

| Severity | ID | Package | One-line Summary |
|----------|-----|---------|-----------------|
| Critical | C1 | task | `toggleCheckboxInFile` frontmatter offset off-by-one |
| Critical | C2 | webhook | Secret never returned to user (HMAC unusable) |
| Critical | C3 | search | Recency re-ranking after SQL pagination (wrong results) |
| High | H1 | webhook | No panic recovery in Dispatch goroutines |
| High | H2 | webhook | Concurrent SQLite writes from goroutines |
| High | H3 | task | Task IDs regenerated on every sync |
| High | H4 | task | ToggleDone race condition (no transaction, file divergence) |
| Medium | M1 | webhook | Dispatch doesn't validate eventType |
| Medium | M2 | webhook | SSRF: initial request bypasses private IP check |
| Medium | M3 | webhook | `isPrivateIP` misses unspecified/IPv6-mapped addresses |
| Medium | M4 | mcp | `handleWebhookRegister` leaks raw error details |
| Medium | M5 | mcp | tasks_list passes slug as ProjectID (filtering broken) |
| Medium | M6 | search | Zero-value time penalty on parse failure |
| Medium | M7 | search | Score compression destroys ranking discrimination |
| Medium | M8 | search | No upper bound on semantic search limit |
| Medium | M9 | task | No path traversal guard in `toggleCheckboxInFile` |
| Medium | M10 | task/webhook | `ulid.MustNew` can panic in server context |
| Medium | M11 | task | `parseTasks` doesn't handle `\r\n` line endings |
| Medium | M12 | search | Invalid `recency_bias` silently ignored |
| Medium | M13 | main | `webhookSvc` nil during startup reconciliation |
| Low | L1 | task | `?done=banana` silently treated as false |
| Low | L2 | task | Missing composite index `(done, updated_at)` |
| Low | L3 | webhook | No delivery retention/cleanup policy |
| Low | L4 | webhook | No secret rotation mechanism |
| Low | L5 | search | `batchGetNoteTimestamps` swallows DB errors |
| Low | L6 | task/webhook | `time.Parse` errors silently discarded |
| Low | L7 | webhook | `listEvents` endpoint inconsistent auth check |
| Low | L8 | main | `mcpSrv.Close()` never called on shutdown |
| Low | L9 | webhook | `matchesFilter` bypassed for struct payloads |
