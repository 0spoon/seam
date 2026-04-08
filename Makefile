.PHONY: build build-web build-all run test test-integration test-web lint fmt clean dev-web init install-service uninstall-service install-tui uninstall-tui service-status service-start service-stop service-restart service-logs chroma-up chroma-down chroma-logs chroma-status reindex

CHROMA_COMPOSE := docker compose -f docker/chroma-compose.yml

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Where `make install-tui` drops the seam binary. Override PREFIX to
# install into a user-local directory without sudo, e.g.:
#   make install-tui PREFIX=$$HOME/.local
PREFIX ?= /usr/local
INSTALL_BIN_DIR ?= $(PREFIX)/bin

build:
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/seamd ./cmd/seamd
	go build -o bin/seam ./cmd/seam
	go build -o bin/seam-reindex ./cmd/seam-reindex

build-web:
	cd web && npm install && npm run build

build-all: build build-web

run: build
	./bin/seamd

test:
	go test ./internal/... ./cmd/...

test-integration:
	go test -tags integration ./internal/... ./cmd/...

test-web:
	cd web && npx vitest run

lint:
	golangci-lint run ./...
	cd web && npx eslint .

fmt:
	gofmt -w .
	cd web && npx prettier --write ..

dev-web:
	cd web && npm run dev

init:
	@bash scripts/init.sh

install-service: build-all
	@bash scripts/install-service.sh

uninstall-service:
	@bash scripts/uninstall-service.sh

# Install just the TUI client (`seam`) into a directory on PATH so it
# can be launched from anywhere. Builds first, then copies the binary.
# If $(INSTALL_BIN_DIR) is not writable, re-run with sudo or override
# PREFIX to a user-owned location.
install-tui: build
	@mkdir -p $(INSTALL_BIN_DIR)
	@install -m 0755 bin/seam $(INSTALL_BIN_DIR)/seam
	@echo "Installed seam to $(INSTALL_BIN_DIR)/seam"

uninstall-tui:
	@rm -f $(INSTALL_BIN_DIR)/seam
	@echo "Removed $(INSTALL_BIN_DIR)/seam"

# Day-to-day control of the installed user service. These wrap launchctl
# (macOS) and systemctl --user (Linux) so the same target works on both.
# When the optional Chroma supervisor is also installed, every action
# applies to it as well.
service-status:
	@bash scripts/service.sh status

service-start:
	@bash scripts/service.sh start

service-stop:
	@bash scripts/service.sh stop

service-restart:
	@bash scripts/service.sh restart

service-logs:
	@bash scripts/service.sh logs

# ChromaDB container management. Thin wrappers around docker compose.
# Data dir comes from docker/.env (written by `make init`) or defaults
# to ./data via the compose file.
chroma-up:
	$(CHROMA_COMPOSE) up -d

chroma-down:
	$(CHROMA_COMPOSE) down

chroma-logs:
	$(CHROMA_COMPOSE) logs -f

chroma-status:
	$(CHROMA_COMPOSE) ps

# Re-embed every note for the default user using the embedding provider
# and model currently configured in seam-server.yaml. Run this after
# changing models.embeddings or embeddings.provider, since each
# (provider, model) tuple gets its own Chroma collection.
reindex: build
	./bin/seam-reindex

clean:
	rm -rf bin
	rm -rf web/dist
