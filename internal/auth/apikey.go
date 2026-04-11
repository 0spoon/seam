package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// VerifyMCPAPIKey returns true when the request carries an Authorization
// header of the form "Bearer <apiKey>" matching apiKey via constant-time
// comparison. An empty apiKey rejects every request, since it would
// otherwise let unauthenticated callers through.
//
// Used by the MCP server (long-lived bearer token alternative to JWT) and
// the SessionStart hook endpoint that Claude Code calls into.
func VerifyMCPAPIKey(r *http.Request, apiKey string) bool {
	if apiKey == "" {
		return false
	}
	authHeader := r.Header.Get("Authorization")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(apiKey)) == 1
}
