#!/usr/bin/env bash
#
# Install the seam SessionStart hook into Claude Code's settings.json so
# every Claude Code session is auto-briefed with recent Seam activity (open
# sessions, recent memories, open tasks). The actual JSON merge happens in
# `seamd install-hooks` -- this wrapper just builds the binary if needed and
# prints a friendly summary.
#
# Idempotent: safe to re-run after rotating mcp.api_key or moving seamd.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

info() { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
ok()   { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
warn() { printf "\033[1;33m==>\033[0m %s\n" "$1"; }
err()  { printf "\033[1;31m==>\033[0m %s\n" "$1" >&2; }

BIN="$REPO_ROOT/bin/seamd"
if [ ! -x "$BIN" ]; then
    info "seamd binary not found, building..."
    (cd "$REPO_ROOT" && make build >/dev/null)
fi

info "Installing seam SessionStart hook into ~/.claude/settings.json"
"$BIN" install-hooks "$@"

echo
ok   "Done. Run './bin/seamd doctor' to verify the install end-to-end."
echo
echo "  Note: 'make install-service' does NOT install this hook."
echo "        Run 'make uninstall-claude-hooks' to remove it."
