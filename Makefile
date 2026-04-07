.PHONY: build build-web build-all run test test-integration test-web lint fmt clean dev-web init install-service uninstall-service

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/seamd ./cmd/seamd
	go build -o bin/seam ./cmd/seam

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

clean:
	rm -rf bin
	rm -rf web/dist
