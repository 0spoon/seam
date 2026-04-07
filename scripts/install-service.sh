#!/usr/bin/env bash
#
# Install seamd as a user-level service that starts at login/boot and
# restarts on failure. Uses launchd on macOS and systemd --user on Linux.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="$REPO_ROOT/bin/seamd"
CONFIG="$REPO_ROOT/seam-server.yaml"
WEB_DIST="$REPO_ROOT/web/dist"

info() { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
ok()   { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
warn() { printf "\033[1;33m==>\033[0m %s\n" "$1"; }
err()  { printf "\033[1;31m==>\033[0m %s\n" "$1" >&2; }

# -- preflight ----------------------------------------------------------------

if [ ! -x "$BINARY" ]; then
    err "Binary not found at $BINARY"
    err "Run 'make build' first."
    exit 1
fi

if [ ! -f "$CONFIG" ]; then
    err "Config not found at $CONFIG"
    err "Run 'make init' first to generate it."
    exit 1
fi

# seamd serves both the JSON API and the React SPA from web/dist on the
# same port, so the "web server" is just the built frontend assets. Make
# sure the build exists before installing the service.
if [ ! -f "$WEB_DIST/index.html" ]; then
    err "Web frontend not built at $WEB_DIST"
    err "Run 'make build-web' (or 'make build-all') first."
    exit 1
fi

OS="$(uname -s)"

# -- macOS (launchd) ----------------------------------------------------------

install_launchd() {
    local label="com.seam.seamd"
    local agent_dir="$HOME/Library/LaunchAgents"
    local plist="$agent_dir/$label.plist"
    local log_dir="$HOME/Library/Logs/seam"

    mkdir -p "$agent_dir" "$log_dir"

    info "Writing launchd agent: $plist"
    cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$label</string>
    <key>ProgramArguments</key>
    <array>
        <string>$BINARY</string>
    </array>
    <key>WorkingDirectory</key>
    <string>$REPO_ROOT</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>$log_dir/seamd.log</string>
    <key>StandardErrorPath</key>
    <string>$log_dir/seamd.err.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
        <key>HOME</key>
        <string>$HOME</string>
    </dict>
</dict>
</plist>
EOF

    # Use the modern launchctl bootstrap/bootout API. The legacy
    # load/unload commands silently fail when the service is already
    # bootstrapped, which means a reinstall would keep running the old
    # binary. bootout cleanly tears down any previous instance so the
    # subsequent bootstrap picks up the freshly built seamd.
    local domain="gui/$(id -u)"
    local target="$domain/$label"

    if launchctl print "$target" >/dev/null 2>&1; then
        info "Service already loaded, replacing..."
        launchctl bootout "$target" 2>/dev/null || true
        # bootout returns before the process is fully gone; wait briefly.
        for _ in 1 2 3 4 5; do
            launchctl print "$target" >/dev/null 2>&1 || break
            sleep 0.2
        done
    fi

    if ! launchctl bootstrap "$domain" "$plist"; then
        err "launchctl bootstrap failed for $plist"
        exit 1
    fi
    launchctl enable "$target" 2>/dev/null || true

    ok "Installed launchd agent: $label"
    echo
    echo "  Status:    launchctl print $target | head"
    echo "  Stop:      launchctl bootout $target"
    echo "  Start:     launchctl bootstrap $domain $plist"
    echo "  Logs:      tail -f $log_dir/seamd.log"
    echo "  Errors:    tail -f $log_dir/seamd.err.log"
    echo
    echo "The service runs seamd, which serves both the JSON API and the React"
    echo "SPA (from $WEB_DIST) on the listen address in $CONFIG."
    echo "It starts at login and restarts on failure."
}

# -- Linux (systemd --user) ---------------------------------------------------

install_systemd() {
    local unit="seamd.service"
    local unit_dir="$HOME/.config/systemd/user"
    local unit_path="$unit_dir/$unit"

    if ! command -v systemctl >/dev/null 2>&1; then
        err "systemctl not found. Only systemd-based Linux is supported."
        exit 1
    fi

    mkdir -p "$unit_dir"

    info "Writing systemd unit: $unit_path"
    cat > "$unit_path" <<EOF
[Unit]
Description=Seam knowledge server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BINARY
WorkingDirectory=$REPO_ROOT
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
EOF

    systemctl --user daemon-reload
    systemctl --user enable --now "$unit"

    ok "Installed systemd unit: $unit"
    echo
    echo "  Status:    systemctl --user status $unit"
    echo "  Stop:      systemctl --user stop $unit"
    echo "  Start:     systemctl --user start $unit"
    echo "  Logs:      journalctl --user -u $unit -f"
    echo
    echo "The service runs seamd, which serves both the JSON API and the React"
    echo "SPA (from $WEB_DIST) on the listen address in $CONFIG."
    echo

    if ! loginctl show-user "$USER" 2>/dev/null | grep -q "Linger=yes"; then
        warn "User lingering is disabled. The service will only run while you are logged in."
        warn "To start it at boot without login, run:"
        echo "    sudo loginctl enable-linger $USER"
    fi
}

# -- dispatch -----------------------------------------------------------------

case "$OS" in
    Darwin) install_launchd ;;
    Linux)  install_systemd ;;
    *)
        err "Unsupported OS: $OS"
        err "Only macOS (launchd) and Linux (systemd) are supported."
        exit 1
        ;;
esac
