.PHONY: help build build-web build-all run dev test test-integration test-race test-web test-web-watch lint fmt vet tidy typecheck coverage bench logs kill-stale clean dev-web init install-service uninstall-service install-onboard-skill uninstall-onboard-skill install-claude-hooks uninstall-claude-hooks doctor install-tui uninstall-tui service-status service-start service-stop service-restart service-logs chroma-up chroma-down chroma-logs chroma-status reindex

CHROMA_COMPOSE := docker compose -f docker/chroma-compose.yml

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Where `make install-tui` drops the seam binary. Override PREFIX to
# install into a user-local directory without sudo, e.g.:
#   make install-tui PREFIX=$$HOME/.local
PREFIX ?= /usr/local
INSTALL_BIN_DIR ?= $(PREFIX)/bin

.DEFAULT_GOAL := help

# -- discovery ----------------------------------------------------------------

help:  ## Print this help (targets + descriptions)
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[1;36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

# -- build --------------------------------------------------------------------

build:  ## Build seamd + seam (TUI) + seam-reindex to ./bin/
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/seamd ./cmd/seamd
	go build -o bin/seam ./cmd/seam
	go build -o bin/seam-reindex ./cmd/seam-reindex

build-web:  ## Install web deps and build the React SPA to web/dist
	@bash -c 'set -e; \
	    cd web; \
	    NVM_SH="$${NVM_DIR:-$$HOME/.nvm}/nvm.sh"; \
	    if [ -s "$$NVM_SH" ]; then \
	        . "$$NVM_SH" >/dev/null 2>&1; \
	        nvm use --silent >/dev/null 2>&1 || true; \
	    fi; \
	    npm install && npm run build'

build-all: build build-web  ## Build Go binaries and the React SPA

# -- run / dev ----------------------------------------------------------------

run: build  ## Build and run seamd in the foreground
	./bin/seamd

dev:  ## Run seamd + vite + Chroma in parallel (Ctrl-C to stop)
	@bash scripts/dev.sh

dev-web:  ## Start the Vite dev server (:5173) proxying /api to :8080
	cd web && npm run dev

# -- testing ------------------------------------------------------------------

test:  ## Run Go unit tests
	go test ./internal/... ./cmd/...

test-integration:  ## Run Go integration tests (real filesystem + on-disk SQLite)
	go test -tags integration ./internal/... ./cmd/...

test-race:  ## Run Go unit tests with the race detector
	go test -race ./internal/... ./cmd/...

test-web:  ## Run frontend tests (Vitest, single run)
	cd web && npx vitest run

test-web-watch:  ## Run frontend tests in Vitest watch mode
	cd web && npx vitest

coverage:  ## Run Go tests with coverage, write coverage.out + coverage.html
	go test -coverprofile=coverage.out ./internal/... ./cmd/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: $$(pwd)/coverage.html"

bench:  ## Run performance-tagged benchmarks with memory stats
	go test -tags performance -bench=. -benchmem ./internal/...

# -- quality ------------------------------------------------------------------

lint:  ## Run golangci-lint + eslint
	golangci-lint run ./...
	cd web && npx eslint .

fmt:  ## Format Go (gofmt) and web (prettier)
	gofmt -w .
	cd web && npx prettier --write ..

vet:  ## go vet ./...
	go vet ./...

tidy:  ## go mod tidy
	go mod tidy

typecheck:  ## TypeScript typecheck for web (no emit)
	cd web && npx tsc -b --noEmit

# -- service / ops ------------------------------------------------------------

init:  ## Interactive config setup (JWT, data dir, LLM, Chroma)
	@bash scripts/init.sh

install-service: build-all  ## Install seamd (and optional Chroma supervisor) as a user service
	@bash scripts/install-service.sh

uninstall-service:  ## Uninstall the seamd user service
	@bash scripts/uninstall-service.sh

install-onboard-skill:  ## Install the /seam-onboard Claude Code skill (self-removing onboarding helper)
	@bash scripts/install-onboard-skill.sh

uninstall-onboard-skill:  ## Remove the /seam-onboard Claude Code skill from ~/.claude/skills
	@bash scripts/uninstall-onboard-skill.sh

# Install the SessionStart hook so Claude Code auto-briefs each new session
# with recent Seam activity. NOT installed by `make install-service`: users
# who don't want Claude Code touching their settings.json should leave it
# alone. Re-runnable to rotate the API key or update the hook URL.
install-claude-hooks: build  ## Install seam SessionStart hook into ~/.claude/settings.json (Claude Code auto-briefing)
	@bash scripts/install-claude-hooks.sh

uninstall-claude-hooks: build  ## Remove seam SessionStart hook from ~/.claude/settings.json
	@bash scripts/uninstall-claude-hooks.sh

doctor: build  ## Run end-to-end self-checks (config, seamd, MCP registration, hooks)
	@./bin/seamd doctor

# Install just the TUI client (`seam`) into a directory on PATH so it
# can be launched from anywhere. Builds first, then copies the binary.
# If $(INSTALL_BIN_DIR) is not writable, re-run with sudo or override
# PREFIX to a user-owned location.
install-tui: build  ## Install the seam TUI to $(INSTALL_BIN_DIR)
	@mkdir -p $(INSTALL_BIN_DIR)
	@install -m 0755 bin/seam $(INSTALL_BIN_DIR)/seam
	@echo "Installed seam to $(INSTALL_BIN_DIR)/seam"

uninstall-tui:  ## Remove the seam TUI from $(INSTALL_BIN_DIR)
	@rm -f $(INSTALL_BIN_DIR)/seam
	@echo "Removed $(INSTALL_BIN_DIR)/seam"

# Day-to-day control of the installed user service. These wrap launchctl
# (macOS) and systemctl --user (Linux) so the same target works on both.
# When the optional Chroma supervisor is also installed, every action
# applies to it as well.
service-status:  ## Show status for seamd (and Chroma supervisor if installed)
	@bash scripts/service.sh status

service-start:  ## Start the installed seamd service
	@bash scripts/service.sh start

service-stop:  ## Stop the installed seamd service
	@bash scripts/service.sh stop

service-restart:  ## Restart the installed seamd service
	@bash scripts/service.sh restart

service-logs:  ## Tail seamd (+ Chroma supervisor if installed) logs
	@bash scripts/service.sh logs

# Short alias for service-logs, and the name developers reach for first.
logs:  ## Tail seamd + Chroma logs (requires `make install-service`)
	@bash scripts/service.sh logs

kill-stale:  ## Kill any stale seamd listener on the configured port
	@bash scripts/service.sh kill-stale

# ChromaDB container management. Thin wrappers around docker compose.
# Data dir comes from docker/.env (written by `make init`) or defaults
# to ./data via the compose file.
chroma-up:  ## Start the ChromaDB container (detached)
	$(CHROMA_COMPOSE) up -d

chroma-down:  ## Stop and remove the ChromaDB container
	$(CHROMA_COMPOSE) down

chroma-logs:  ## Follow ChromaDB container logs
	$(CHROMA_COMPOSE) logs -f

chroma-status:  ## Show ChromaDB container status
	$(CHROMA_COMPOSE) ps

# Re-embed every note for the default user using the embedding provider
# and model currently configured in seam-server.yaml. Run this after
# changing models.embeddings or embeddings.provider, since each
# (provider, model) tuple gets its own Chroma collection.
reindex: build  ## Re-embed all notes against the currently configured embedding model
	./bin/seam-reindex

clean:  ## Remove build artifacts (bin/, web/dist, coverage files)
	rm -rf bin
	rm -rf web/dist
	rm -f coverage.out coverage.html
