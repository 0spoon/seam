.PHONY: build build-web build-all run test test-integration test-web lint fmt clean dev-web init install-service uninstall-service chroma-up chroma-down chroma-logs chroma-status reindex

CHROMA_COMPOSE := docker compose -f docker/chroma-compose.yml

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

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

dev-web:
	cd web && npm run dev

init:
	@bash scripts/init.sh

install-service: build-all
	@bash scripts/install-service.sh

uninstall-service:
	@bash scripts/uninstall-service.sh

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
