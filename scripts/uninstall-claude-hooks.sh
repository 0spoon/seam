#!/usr/bin/env bash
#
# Remove the seam SessionStart hook from Claude Code's settings.json.
# Counterpart to scripts/install-claude-hooks.sh. Safe to run even when no
# seam hook is installed -- the underlying `seamd uninstall-hooks` warns
# instead of erroring in that case.

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

info "Removing seam SessionStart hook from ~/.claude/settings.json"
"$BIN" uninstall-hooks "$@"

echo
ok "Done."
