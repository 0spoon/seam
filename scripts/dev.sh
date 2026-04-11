#!/usr/bin/env bash
#
# Dev stack launcher. Brings up the full local loop in a single
# terminal: ChromaDB container (optional), seamd, and the Vite web dev
# server. Output from seamd and vite is prefixed so you can tell which
# side is speaking. Ctrl-C tears both down cleanly; Chroma is left
# running because it's a container -- manage it with `make chroma-down`.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

info() { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
ok()   { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
warn() { printf "\033[1;33m==>\033[0m %s\n" "$1"; }
err()  { printf "\033[1;31m==>\033[0m %s\n" "$1" >&2; }

# -- refuse to fight with an installed service -------------------------------
#
# If seamd is running under launchd/systemd, `make dev` would race it on
# the listen port and the installed service's supervisor would restart
# it as fast as we kill it. Fail fast with an actionable hint instead.
OS="$(uname -s)"
service_running() {
    case "$OS" in
        Darwin)
            launchctl print "gui/$(id -u)/com.seam.seamd" >/dev/null 2>&1
            ;;
        Linux)
            command -v systemctl >/dev/null 2>&1 && \
                systemctl --user is-active --quiet seamd.service
            ;;
        *)
            return 1
            ;;
    esac
}
if service_running; then
    err "Installed seamd service is running -- 'make dev' would race it."
    err "Stop it first:   make service-stop"
    err "Or tail its logs instead: make logs"
    exit 1
fi

# -- chroma (optional) --------------------------------------------------------
#
# If the compose file and docker are present, make sure the container is
# up. `docker compose up -d` is idempotent, so it's safe whether or not
# the container was already running.

COMPOSE_FILE="docker/chroma-compose.yml"
if [ -f "$COMPOSE_FILE" ] && command -v docker >/dev/null 2>&1; then
    if docker info >/dev/null 2>&1; then
        info "Ensuring ChromaDB container is up"
        docker compose -f "$COMPOSE_FILE" up -d >/dev/null
    else
        warn "Docker daemon not reachable; skipping Chroma bring-up"
        warn "Start Docker and run 'make chroma-up' if you need vector search"
    fi
else
    warn "Docker or compose file missing; skipping Chroma bring-up"
fi

# -- clear a stale listener ---------------------------------------------------
#
# If a previous `make dev` exited uncleanly, seamd may still be bound to
# the port. Reuse the same kill-stale helper the service wrapper uses so
# the logic stays in one place.
bash scripts/service.sh kill-stale >/dev/null 2>&1 || true

# -- build --------------------------------------------------------------------
#
# Build once up-front so compile errors show before vite starts and the
# seamd binary is ready to exec immediately.
info "Building seamd"
make build >/dev/null

# -- parallel run -------------------------------------------------------------

# Line prefixer. A plain `while read` loop keeps this dependency-free
# (no need for ts, awk tricks, or a multiplexer like foreman/overmind).
prefix() {
    local tag="$1"
    local color="$2"
    while IFS= read -r line; do
        printf "\033[1;%sm[%s]\033[0m %s\n" "$color" "$tag" "$line"
    done
}

cleanup() {
    # Kill every child we spawned. `jobs -p` lists PIDs of background
    # jobs in this shell. `|| true` keeps cleanup idempotent on repeat
    # signals.
    local pids
    pids="$(jobs -p 2>/dev/null || true)"
    if [ -n "$pids" ]; then
        # shellcheck disable=SC2086
        kill $pids 2>/dev/null || true
        wait 2>/dev/null || true
    fi
}
trap cleanup INT TERM EXIT

info "Starting seamd + vite (Ctrl-C to stop)"
echo

# Run seamd under the prefixer. 2>&1 merges stderr so error lines also
# get tagged. The pipeline itself is backgrounded with `&`.
./bin/seamd 2>&1 | prefix seamd 34 &

# Same pattern for vite. `cd web && npm run dev` is what `make dev-web`
# does today; keeping the command identical avoids surprise.
(cd web && npm run dev) 2>&1 | prefix vite 35 &

wait
