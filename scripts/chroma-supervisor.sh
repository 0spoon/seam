#!/usr/bin/env bash
#
# ChromaDB supervisor for Seam.
#
# Wakes the Docker engine if it is not already running, waits for the
# daemon socket, then execs `docker compose up` (foreground) so that
# the lifetime of this process matches the lifetime of the ChromaDB
# container. Designed to be run by launchd (macOS) or systemd --user
# (Linux) with KeepAlive / Restart=always. When the container exits or
# the user runs `docker compose down`, this script exits too and the
# service manager restarts it after a throttle interval.
#
# Exit codes:
#   0  -- compose exited cleanly (will be restarted by service manager)
#   1  -- precondition failed (missing docker, missing compose file, etc.)
#   2  -- docker daemon did not become ready in time

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="$REPO_ROOT/docker/chroma-compose.yml"
WAIT_SECONDS=60

log() { printf "[chroma-supervisor] %s\n" "$1"; }
err() { printf "[chroma-supervisor] ERROR: %s\n" "$1" >&2; }

# -- preflight ----------------------------------------------------------------

if [ ! -f "$COMPOSE_FILE" ]; then
    err "compose file not found at $COMPOSE_FILE"
    exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
    err "docker not found on PATH"
    exit 1
fi

# -- wake docker if needed ----------------------------------------------------

wake_docker() {
    if docker info >/dev/null 2>&1; then
        return 0
    fi

    case "$(uname -s)" in
        Darwin)
            log "Docker daemon unreachable; launching Docker Desktop"
            # -g: do not bring to foreground. -a: application name.
            open -ga Docker 2>/dev/null || true
            ;;
        Linux)
            log "Docker daemon unreachable; attempting to start it"
            if systemctl --user is-enabled docker.service >/dev/null 2>&1; then
                systemctl --user start docker.service 2>/dev/null || true
            elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
                sudo systemctl start docker 2>/dev/null || true
            else
                err "cannot start docker daemon without sudo and no rootless docker user unit is enabled"
            fi
            ;;
        *)
            err "unsupported OS: $(uname -s)"
            return 1
            ;;
    esac

    log "waiting up to ${WAIT_SECONDS}s for docker daemon"
    local i
    for ((i = 1; i <= WAIT_SECONDS; i++)); do
        if docker info >/dev/null 2>&1; then
            log "docker daemon ready after ${i}s"
            return 0
        fi
        sleep 1
    done

    err "docker daemon did not become ready in ${WAIT_SECONDS}s"
    return 1
}

if ! wake_docker; then
    exit 2
fi

# -- run compose (foreground) -------------------------------------------------

# cd so that docker compose picks up docker/.env automatically.
cd "$REPO_ROOT/docker"

log "starting: docker compose -f chroma-compose.yml up"
exec docker compose -f chroma-compose.yml up
