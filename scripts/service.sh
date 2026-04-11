#!/usr/bin/env bash
#
# Day-to-day management for the seamd user-level service installed by
# install-service.sh. Wraps launchctl (macOS) and systemctl --user (Linux)
# so the Makefile targets stay portable.
#
# Usage: scripts/service.sh <status|start|stop|restart|logs|kill-stale>
#
# When the optional Chroma supervisor service is also installed, every
# action applies to it as well, so a single 'make service-restart' brings
# both halves back together.

set -euo pipefail

ACTION="${1:-}"
OS="$(uname -s)"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

SEAMD_LABEL="com.seam.seamd"
CHROMA_LABEL="com.seam.chroma"
SEAMD_UNIT="seamd.service"
CHROMA_UNIT="seamd-chroma.service"

LAUNCHD_DIR="$HOME/Library/LaunchAgents"
SYSTEMD_DIR="$HOME/.config/systemd/user"
MAC_LOG_DIR="$HOME/Library/Logs/seam"

info() { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
ok()   { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
warn() { printf "\033[1;33m==>\033[0m %s\n" "$1"; }
err()  { printf "\033[1;31m==>\033[0m %s\n" "$1" >&2; }

# Extract the listen port from seam-server.yaml (default 8080).
seamd_port() {
    local config="$REPO_ROOT/seam-server.yaml"
    if [ -f "$config" ]; then
        local port
        port=$(grep -E '^listen:' "$config" | grep -oE '[0-9]+' | tail -1)
        echo "${port:-8080}"
    else
        echo "8080"
    fi
}

# Kill any LISTENING seamd process on the configured port. Prevents
# "address already in use" crash loops when a stale seamd from
# `make run` or a prior crash is still holding the port.
#
# IMPORTANT: filter to processes in LISTEN state AND whose command is
# `seamd`. `lsof -i:PORT` also returns client-side connections (any
# process with an open socket to that port), so an unfiltered kill
# would nuke unrelated tools that happen to be talking to seamd.
kill_stale_on_port() {
    local port
    port="$(seamd_port)"
    # -sTCP:LISTEN filters to listeners only. -c seamd matches the
    # command name. -t emits just PIDs.
    local pids
    pids="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -c seamd -t 2>/dev/null || true)"
    if [ -n "$pids" ]; then
        warn "killing stale seamd listener(s) on port $port: $pids"
        echo "$pids" | xargs kill 2>/dev/null || true
        sleep 0.5
        pids="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -c seamd -t 2>/dev/null || true)"
        if [ -n "$pids" ]; then
            echo "$pids" | xargs kill -9 2>/dev/null || true
            sleep 0.3
        fi
    fi
}

usage() {
    cat <<EOF
Usage: scripts/service.sh <action>

Actions:
  status       Show status for seamd (and Chroma supervisor if installed)
  start        Start the services
  stop         Stop the services
  restart      Stop then start
  logs         Follow seamd (+ Chroma supervisor when installed) logs
  kill-stale   Kill any stale seamd listener on the configured port

Install with: make install-service
Remove with:  make uninstall-service
EOF
}

if [ -z "$ACTION" ]; then
    usage
    exit 1
fi

# -- launchd helpers (macOS) --------------------------------------------------

ld_target() { echo "gui/$(id -u)/$1"; }
ld_plist()  { echo "$LAUNCHD_DIR/$1.plist"; }

ld_loaded() {
    launchctl print "$(ld_target "$1")" >/dev/null 2>&1
}

ld_status() {
    local label="$1"
    local target
    target="$(ld_target "$label")"
    if ! ld_loaded "$label"; then
        printf "  %-18s not loaded\n" "$label"
        return 0
    fi
    # 'launchctl print' is verbose; pull out the few fields people care about.
    local state pid exit_code
    state=$(launchctl print "$target" 2>/dev/null | awk -F'= ' '/^\tstate / {print $2; exit}')
    pid=$(launchctl print "$target" 2>/dev/null | awk -F'= ' '/^\tpid / {print $2; exit}')
    exit_code=$(launchctl print "$target" 2>/dev/null | awk -F'= ' '/^\tlast exit code / {print $2; exit}')
    printf "  %-18s state=%s pid=%s last_exit=%s\n" "$label" "${state:-?}" "${pid:-none}" "${exit_code:-?}"
}

ld_start() {
    local label="$1"
    local plist
    plist="$(ld_plist "$label")"
    if [ ! -f "$plist" ]; then
        warn "$label: plist not found at $plist (run 'make install-service')"
        return 0
    fi
    if ld_loaded "$label"; then
        info "$label: already running"
        return 0
    fi
    if launchctl bootstrap "gui/$(id -u)" "$plist"; then
        ok "$label: started"
    else
        err "$label: bootstrap failed"
        return 1
    fi
}

ld_stop() {
    local label="$1"
    if ! ld_loaded "$label"; then
        info "$label: not running"
        return 0
    fi
    launchctl bootout "$(ld_target "$label")" 2>/dev/null || true
    # bootout returns before the process is fully gone; wait briefly.
    for _ in 1 2 3 4 5; do
        ld_loaded "$label" || break
        sleep 0.2
    done
    ok "$label: stopped"
}

# -- systemd --user helpers (Linux) -------------------------------------------

sd_installed() {
    [ -f "$SYSTEMD_DIR/$1" ]
}

sd_status() {
    local unit="$1"
    if ! sd_installed "$unit"; then
        printf "  %-22s not installed\n" "$unit"
        return 0
    fi
    # --no-pager keeps output flat for the make wrapper.
    systemctl --user status "$unit" --no-pager || true
}

sd_start()   { systemctl --user start "$1"   && ok "$1: started"; }
sd_stop()    { systemctl --user stop "$1"    && ok "$1: stopped"; }
sd_restart() { systemctl --user restart "$1" && ok "$1: restarted"; }

# -- macOS dispatch -----------------------------------------------------------

run_darwin() {
    local has_chroma="no"
    [ -f "$(ld_plist "$CHROMA_LABEL")" ] && has_chroma="yes"

    case "$ACTION" in
        status)
            info "Service status"
            ld_status "$SEAMD_LABEL"
            [ "$has_chroma" = "yes" ] && ld_status "$CHROMA_LABEL"
            ;;
        start)
            # Start Chroma first so seamd can reach it on its first probe.
            [ "$has_chroma" = "yes" ] && ld_start "$CHROMA_LABEL"
            kill_stale_on_port
            ld_start "$SEAMD_LABEL"
            ;;
        stop)
            # Stop seamd first so it doesn't log connection errors against
            # a Chroma supervisor that's tearing down underneath it.
            ld_stop "$SEAMD_LABEL"
            [ "$has_chroma" = "yes" ] && ld_stop "$CHROMA_LABEL"
            ;;
        restart)
            ld_stop "$SEAMD_LABEL"
            [ "$has_chroma" = "yes" ] && ld_stop "$CHROMA_LABEL"
            kill_stale_on_port
            [ "$has_chroma" = "yes" ] && ld_start "$CHROMA_LABEL"
            ld_start "$SEAMD_LABEL"
            ;;
        logs)
            if [ ! -d "$MAC_LOG_DIR" ]; then
                err "Log directory not found at $MAC_LOG_DIR"
                err "Has the service ever run? Try 'make install-service'."
                exit 1
            fi
            local files=("$MAC_LOG_DIR/seamd.log" "$MAC_LOG_DIR/seamd.err.log")
            local banner="seamd"
            if [ "$has_chroma" = "yes" ]; then
                files+=("$MAC_LOG_DIR/chroma.log" "$MAC_LOG_DIR/chroma.err.log")
                banner="seamd + chroma"
            fi
            info "Tailing $banner logs in $MAC_LOG_DIR (Ctrl-C to exit)"
            # tail -F tolerates files that don't exist yet and prints a
            # header when output switches between files, so each line's
            # origin stays obvious.
            exec tail -F "${files[@]}"
            ;;
        *)
            usage
            exit 1
            ;;
    esac
}

# -- Linux dispatch -----------------------------------------------------------

run_linux() {
    if ! command -v systemctl >/dev/null 2>&1; then
        err "systemctl not found. Only systemd-based Linux is supported."
        exit 1
    fi

    local has_chroma="no"
    sd_installed "$CHROMA_UNIT" && has_chroma="yes"

    case "$ACTION" in
        status)
            info "Service status"
            sd_status "$SEAMD_UNIT"
            [ "$has_chroma" = "yes" ] && sd_status "$CHROMA_UNIT"
            ;;
        start)
            [ "$has_chroma" = "yes" ] && sd_start "$CHROMA_UNIT"
            kill_stale_on_port
            sd_start "$SEAMD_UNIT"
            ;;
        stop)
            sd_stop "$SEAMD_UNIT"
            [ "$has_chroma" = "yes" ] && sd_stop "$CHROMA_UNIT"
            ;;
        restart)
            sd_stop "$SEAMD_UNIT"
            [ "$has_chroma" = "yes" ] && sd_stop "$CHROMA_UNIT"
            kill_stale_on_port
            [ "$has_chroma" = "yes" ] && sd_restart "$CHROMA_UNIT"
            sd_start "$SEAMD_UNIT"
            ;;
        logs)
            local units=(-u "$SEAMD_UNIT")
            local banner="$SEAMD_UNIT"
            if [ "$has_chroma" = "yes" ]; then
                units+=(-u "$CHROMA_UNIT")
                banner="$SEAMD_UNIT + $CHROMA_UNIT"
            fi
            info "Tailing journal for $banner (Ctrl-C to exit)"
            exec journalctl --user "${units[@]}" -f
            ;;
        *)
            usage
            exit 1
            ;;
    esac
}

# kill-stale works identically on both platforms (lsof is present on
# macOS and available via apt/yum on Linux), so handle it before the
# OS-specific dispatch.
#
# Refuse to touch a seamd that's under launchd/systemd supervision --
# killing it would just trigger an immediate restart and the "stale"
# label doesn't apply to a healthy managed process. The user should
# reach for `service-stop` in that case.
supervised_seamd_running() {
    case "$OS" in
        Darwin)
            ld_loaded "$SEAMD_LABEL"
            ;;
        Linux)
            command -v systemctl >/dev/null 2>&1 && \
                systemctl --user is-active --quiet "$SEAMD_UNIT"
            ;;
        *)
            return 1
            ;;
    esac
}

if [ "$ACTION" = "kill-stale" ]; then
    if ! command -v lsof >/dev/null 2>&1; then
        err "lsof not found on PATH; cannot inspect listeners"
        exit 1
    fi
    if supervised_seamd_running; then
        warn "seamd is running under a service manager; not touching it"
        warn "Use 'make service-stop' if you really want it down"
        exit 0
    fi
    kill_stale_on_port
    ok "kill-stale: done"
    exit 0
fi

case "$OS" in
    Darwin) run_darwin ;;
    Linux)  run_linux  ;;
    *)
        err "Unsupported OS: $OS"
        exit 1
        ;;
esac
