# Security

## Invariants

- **Path traversal protection** -- rejects `..`, absolute paths, null bytes in all file operations
- **User isolation** -- user ID resolved from JWT in middleware, never accepted from request body or params
- **SSRF protection** -- URL capture and webhook delivery reject private IPs, localhost, `file://` protocol, with DNS rebinding mitigation (TOCTOU-safe via direct IP connection)
- **Input validation** -- note titles, project names, tags sanitized for filesystem safety (no `/`, `\`, `..`, `\x00`)
- **Request body limits** -- 1MB JSON, 100MB audio uploads
- **XSS prevention** -- DOMPurify on all rendered HTML in the web frontend
- **Rate limiting** -- per-IP on auth endpoints, per-user on AI and MCP endpoints
- **JWT** -- short-lived access tokens (15m default), longer refresh tokens (7d default), bcrypt password hashing
- **HMAC webhook signatures** -- `X-Seam-Signature` header on all webhook deliveries, per-webhook secrets

## Reporting

If you find a security issue, please report it privately. Do not open a public issue.
