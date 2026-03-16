.PHONY: build run test test-integration test-web lint fmt clean dev-web init

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/seamd ./cmd/seamd
	go build -o bin/seam ./cmd/seam

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

clean:
	rm -rf bin
	rm -rf web/dist
