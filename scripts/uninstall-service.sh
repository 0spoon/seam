#!/usr/bin/env bash
#
# Stop and remove the seamd user-level service installed by install-service.sh.

set -euo pipefail

info() { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
ok()   { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
warn() { printf "\033[1;33m==>\033[0m %s\n" "$1"; }
err()  { printf "\033[1;31m==>\033[0m %s\n" "$1" >&2; }

OS="$(uname -s)"

# -- macOS (launchd) ----------------------------------------------------------

uninstall_launchd() {
    local label="com.seam.seamd"
    local plist="$HOME/Library/LaunchAgents/$label.plist"
    local target="gui/$(id -u)/$label"

    # Tear down a running instance even if the plist has already been
    # deleted (e.g. partial cleanup from a previous run).
    if launchctl print "$target" >/dev/null 2>&1; then
        info "Stopping launchd agent: $label"
        launchctl bootout "$target" 2>/dev/null || true
    fi

    if [ ! -f "$plist" ]; then
        warn "No launchd agent plist found at $plist"
        return 0
    fi

    rm -f "$plist"
    ok "Removed launchd agent: $plist"
}

# -- Linux (systemd --user) ---------------------------------------------------

uninstall_systemd() {
    local unit="seamd.service"
    local unit_path="$HOME/.config/systemd/user/$unit"

    if [ ! -f "$unit_path" ]; then
        warn "No systemd unit found at $unit_path"
        return 0
    fi

    if ! command -v systemctl >/dev/null 2>&1; then
        err "systemctl not found."
        exit 1
    fi

    info "Stopping and disabling: $unit"
    systemctl --user disable --now "$unit" 2>/dev/null || true

    rm -f "$unit_path"
    systemctl --user daemon-reload
    ok "Removed systemd unit: $unit_path"
}

# -- dispatch -----------------------------------------------------------------

case "$OS" in
    Darwin) uninstall_launchd ;;
    Linux)  uninstall_systemd ;;
    *)
        err "Unsupported OS: $OS"
        exit 1
        ;;
esac
