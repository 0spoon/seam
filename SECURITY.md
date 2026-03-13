# Security

## Threat Model

### Assets
- **User credentials**: bcrypt-hashed passwords and email addresses in server.db
- **JWT signing secret**: used to mint and verify all access tokens; compromise allows full impersonation of any user
- **Refresh tokens**: SHA-256 hashes stored in server.db; raw tokens returned to clients
- **User notes**: plain-text Markdown files and per-user SQLite databases containing personal knowledge
- **AI conversation history**: stored in per-user databases, may contain sensitive discussions
- **Configuration secrets**: JWT secret, Ollama/ChromaDB URLs in seam-server.yaml

### Threat Actors
- **Local network users**: Seam binds to a network address (default `:8080`), so anyone on the same network can reach the API
- **Malicious content in captured URLs**: URL capture fetches external web pages; malicious HTML could attempt SSRF or resource exhaustion
- **Compromised Ollama/ChromaDB**: these are trusted internal services with no auth; a compromised instance could return malicious data

### Attack Surface
- HTTP REST API (chi router, ~30 endpoints)
- WebSocket endpoint (`/api/ws`) with JWT-based auth handshake
- URL capture: outbound HTTP fetcher with SSRF protections
- Voice capture: shells out to `whisper-cli` and `ffmpeg` via `exec.Command`
- File watcher: monitors user note directories for external edits
- Static file server: serves `web/dist` SPA assets

## Security Architecture

**Authentication**: JWT access tokens (HS256, 15-minute TTL) + opaque refresh tokens (7-day TTL, SHA-256 hashed in DB, rotated on each use). Passwords hashed with bcrypt cost 12. User ID resolved from JWT claims in middleware -- never accepted from request body/params. Password changes revoke all existing refresh tokens.

**Authorization**: Per-user SQLite databases provide tenant isolation at the storage layer. The `userdb.Manager.Open()` validates user IDs against a strict alphanumeric regex before constructing filesystem paths.

**Input validation**: Centralized `validate` package enforces path traversal prevention (rejects `..`, absolute paths, null bytes), filesystem-safe names (no `/`, `\`, `..`), and user ID format validation. Applied consistently across note, project, template, and userdb packages.

**SSRF protection**: URL capture uses a custom `DialContext` that resolves DNS, checks all resolved IPs against private/loopback/link-local/unspecified ranges, then connects directly to the validated IP (preventing DNS rebinding). Redirect targets are restricted to HTTP/HTTPS schemes. Response bodies limited to 2MB.

**SQL injection prevention**: All queries use parameterized statements (`?` placeholders). FTS5 queries are sanitized by stripping operators and quoting terms. The `ORDER BY` clause in note listing uses only hardcoded column names derived from a controlled switch, not user input.

**Request hardening**: All JSON request bodies wrapped in `http.MaxBytesReader` (1MB). WebSocket read limit set to 64KB. AI input fields capped at 100KB. Audio uploads limited to 25MB form / 100MB file.

**Cryptography**: `crypto/rand` used for all random values (ULIDs, refresh tokens, request IDs). JWT verification checks signing method is HMAC before accepting. No use of `math/rand` for security-sensitive operations.

**Error handling**: Internal error details are never exposed in HTTP responses. All handlers map domain errors to appropriate status codes and return sanitized messages. Panic recovery middleware catches and logs stack traces server-side only.

## Audit Findings

### Critical

**C-1: JWT secret committed to git in `seam-server.yaml`**
- **Status**: FIXED (2026-03-13)
- **Location**: `seam-server.yaml` (formerly tracked, now gitignored)
- **Description**: The file `seam-server.yaml` contained a hardcoded JWT secret and was tracked by git despite being listed in `.gitignore` (committed before the gitignore rule took effect).
- **Impact**: Anyone with read access to the repository history could extract the JWT signing secret and forge valid access tokens for any user.
- **Resolution**: File purged from all branches and all git history using `git filter-repo --invert-paths --path seam-server.yaml`. The `.gitignore` entry prevents future re-commits. The existing JWT secret should still be rotated on any deployed instance since the old secret may exist in clones or remote mirrors.
- **Remaining action**: Rotate the JWT secret on any deployed instance. Force-push to remote (`git push --force --all`) to propagate the cleaned history.

### High

**H-1: Missing `WriteTimeout` on HTTP server**
- **Status**: FIXED (2026-03-13)
- **Location**: `internal/server/server.go`
- **Description**: The HTTP server sets `ReadTimeout: 15s` and `IdleTimeout: 60s` but omits `WriteTimeout`. A slow-read attack (slowloris variant on the response side) can hold server goroutines open indefinitely by reading the response body at an extremely slow rate.
- **Impact**: An attacker can exhaust server goroutines and file descriptors, causing denial of service for all users. Since Seam is intended for local/small deployments, even a modest number of slow connections could be disruptive.
- **Resolution**: Added `WriteTimeout: 30 * time.Second` to the `http.Server` config. The SSE streaming endpoint (`/api/ai/synthesize/stream`) uses `http.ResponseController` to disable the global deadline and reset per-token write deadlines instead.

**H-2: No rate limiting on authentication endpoints**
- **Status**: FIXED (2026-03-13)
- **Location**: `internal/auth/handler.go`
- **Description**: The `/api/auth/login`, `/api/auth/register`, and `/api/auth/refresh` endpoints have no rate limiting. The AI handler has per-user rate limiting (`ai/handler.go:89-103`), but auth endpoints -- the most security-critical -- do not.
- **Impact**: An attacker can brute-force user passwords via unlimited login attempts or flood registration to create mass accounts.
- **Resolution**: Added per-IP rate limiting (5 requests/minute, burst 5) to all public auth endpoints via `authRateLimitMiddleware`. Stale limiter entries are evicted every 5 minutes.

### Medium

**M-1: Password change does not invalidate existing sessions**
- **Status**: FIXED (2026-03-13)
- **Location**: `internal/auth/service.go`
- **Description**: When a user changes their password via `ChangePassword()`, existing refresh tokens for that user are not revoked. The `DeleteRefreshTokensByUser` method exists in the store but is not called.
- **Impact**: If an attacker has obtained a valid refresh token (e.g., via session hijacking), changing the password does not lock them out. The compromised session remains valid for up to 7 days.
- **Resolution**: `ChangePassword()` now calls `s.store.DeleteRefreshTokensByUser(ctx, userID)` after the password update succeeds, forcing all sessions to re-authenticate.

**M-2: Refresh tokens not rotated on use**
- **Status**: FIXED (2026-03-13)
- **Location**: `internal/auth/service.go`
- **Description**: The `Refresh()` method returns the same refresh token that was submitted. If a refresh token is intercepted, the attacker can use it indefinitely (for up to 7 days) without detection.
- **Impact**: Extends the window of compromise for stolen refresh tokens. Without rotation, there is no way to detect concurrent use of the same token by both the legitimate user and an attacker.
- **Resolution**: `Refresh()` now deletes the old refresh token and issues a new one via `generateTokenPair()`. Reuse of a rotated-away token returns ErrInvalidCredentials.

### Low / Informational

**L-1: Per-user rate limiter map grows without eviction**
- **Status**: FIXED (2026-03-13)
- **Location**: `internal/ai/handler.go`
- **Description**: The `limiters` map creates a new `rate.Limiter` for every unique user ID that hits an AI endpoint. There is no eviction mechanism, so the map grows monotonically.
- **Impact**: Minimal in practice since user IDs come from authenticated JWTs (bounded by registered user count), but on a long-running server with many users, this represents a minor memory leak.
- **Resolution**: Rate limiter entries now track a `lastSeen` timestamp. A background goroutine evicts entries not seen in the last 10 minutes every 5 minutes.

**L-2: WebSocket connections not bounded per user**
- **Status**: FIXED (2026-03-13)
- **Location**: `internal/ws/hub.go`
- **Description**: The Hub does not limit how many WebSocket connections a single user can open. A user could open hundreds of connections.
- **Impact**: Potential resource exhaustion, though mitigated by the fact that only authenticated users can connect.
- **Resolution**: `Hub.Register()` now enforces a per-user limit of 10 connections (`maxConnsPerUser`). Excess connections are rejected with `ErrTooManyConns` and closed with `StatusTryAgainLater`.

**L-3: Open user registration without restrictions**
- **Status**: MITIGATED (2026-03-13)
- **Location**: `internal/auth/handler.go`
- **Description**: Anyone who can reach the server can register a new account. There is no invite code or admin approval.
- **Impact**: For a "single machine, multi-user" system this is likely intentional, but in any network-exposed deployment it means unlimited account creation.
- **Mitigation**: Registration is now rate-limited per IP (H-2 fix). Consider adding an optional invite code or admin toggle for network-exposed deployments.

## Areas Reviewed With No Issues

| Category | Details |
|---|---|
| SQL Injection | All queries use parameterized statements (`?` placeholders). FTS5 queries sanitized via `sanitizeFTSQuery()`. `ORDER BY` columns are hardcoded Go strings, not user input. |
| Path Traversal | `validate.Path()` rejects `..`, absolute paths, null bytes. `validate.PathWithinDir()` verifies resolved paths stay within base directory. `validate.Name()` blocks `/`, `\`, `..` in titles/tags. Applied in note creation, update, reindex, template loading, and userdb open. |
| SSRF | URL capture resolves DNS, validates all IPs against private ranges, connects to validated IP directly (prevents DNS rebinding). Scheme restricted to HTTP/HTTPS. Redirects limited to 10 hops with scheme validation. |
| XSS | Backend is API-only (JSON responses). Frontend is React (JSX auto-escaping). No `dangerouslySetInnerHTML` usage detected. FTS snippets use `<mark>` tags but these are rendered by the React frontend which escapes by default. |
| User Isolation | User ID resolved exclusively from JWT claims in middleware. Per-user SQLite databases keyed by validated user ID. No endpoint accepts user ID from request body/params for authorization. |
| Cryptographic Practices | `crypto/rand` used for ULIDs, refresh tokens, and request IDs. JWT signing method validated as HMAC before key use. Refresh token hashes stored (not raw tokens). bcrypt cost 12 for passwords. |
| Error Information Leakage | All handlers return sanitized error messages. Stack traces logged server-side only. `safeRegistrationMessage()` maps internal errors to user-safe strings. |
| Request Size Limits | `MaxBytesReader` (1MB) on all JSON endpoints. WebSocket read limit 64KB. Audio upload form limit 25MB. AI input field limit 100KB. URL fetch response limit 2MB. |
| Command Injection | Voice transcription uses `exec.CommandContext` with explicit argument arrays (not shell interpolation). File paths are constructed from temp file names, not user input. |
| CSRF | Not applicable. Authentication uses `Authorization: Bearer` header, not cookies. Browsers do not auto-attach this header, so CSRF attacks cannot work. |
| Dependency Vulnerabilities | `govulncheck` found 13 vulnerabilities, all in the Go standard library (go1.25.0). Fixed in go1.25.8. No third-party dependency vulnerabilities affect the code. 3 additional vulnerabilities exist in imported packages but are not called by this codebase. **Action: upgrade to go1.25.8+.** |

## Accepted Risks

None at this time.

## Security Checklist

- [x] Authentication: all sensitive endpoints require auth
- [x] Authorization: users cannot access other users' resources (per-user SQLite isolation)
- [x] Input validation: all external input is validated/sanitized
- [x] SQL injection: all queries use parameterized statements
- [x] XSS: API-only backend + React auto-escaping
- [x] CSRF: not applicable (bearer token auth, not cookies)
- [x] Cryptography: strong algorithms, proper key management (HS256 JWT, bcrypt, crypto/rand)
- [x] Secrets management: JWT secret scrubbed from git history (C-1 FIXED); rotate secret on deployed instances
- [x] Rate limiting: per-IP rate limiting on auth endpoints (H-2 FIXED), per-user rate limiting on AI endpoints
- [x] Request size limits: body size limits on all endpoints
- [x] Error handling: no internal details leaked to clients
- [ ] Dependencies: govulncheck found 13 stdlib vulnerabilities in go1.25.0 (fixed in go1.25.8); no third-party vulnerabilities affect code
- [ ] TLS: no TLS configuration; relies on reverse proxy or local-only deployment
- [x] Logging: security events logged (login, registration, password change), no sensitive data in logs
- [ ] Container security: no containerization in current deployment model

## Reporting Vulnerabilities

If you discover a security vulnerability in Seam, please report it by opening a private issue or contacting the maintainer directly. Do not open a public issue for security vulnerabilities.

## Last Audited

2026-03-13
