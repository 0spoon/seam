# Development

## Build & Run

```bash
make init                 # interactive config setup (JWT, data dir, LLM provider, Chroma)
make build                # build seamd + seam to ./bin/
make run                  # build and run the server
make dev-web              # React dev server (Vite on :5173, proxies /api to :8080)
make clean                # remove build artifacts + web/dist
```

## ChromaDB container

If you opted into the Docker-managed Chroma during `make init`, manage the container with these targets. They are thin wrappers around `docker compose -f docker/chroma-compose.yml` and read `docker/.env` (written by `make init`) for the data directory.

```bash
make chroma-up            # start (or recreate) the ChromaDB container
make chroma-down          # stop and remove the container
make chroma-logs          # follow container logs
make chroma-status        # show container status
```

For a hands-off setup, `make install-service` will additionally offer to install a sibling launchd/systemd unit running `scripts/chroma-supervisor.sh`, which wakes Docker on demand and keeps the container alive across reboots. See [Getting Started](getting-started.md#chromadb) for the full flow.

## Testing

```bash
make test                 # all Go unit tests
make test-integration     # integration tests (real filesystem, on-disk SQLite)
make test-web             # all frontend tests (Vitest)
```

### Running Specific Tests

```bash
go test ./internal/note/ -run TestService_Create_WritesFile -v   # single test
go test ./internal/note/ -v                                       # one package
go test ./internal/note/ -run "TestStore_.*" -v                   # pattern match
go test -race ./internal/...                                      # race detector

cd web && npx vitest run                       # all frontend
cd web && npx vitest run src/api/client        # single file
```

### Build Tags

| Tag | Purpose |
|---|---|
| *(default)* | Unit tests. No filesystem, no external services |
| `integration` | Real filesystem, on-disk SQLite |
| `external` | Requires running Ollama and/or ChromaDB |
| `performance` | Benchmarks |

## Linting & Formatting

```bash
make lint                 # golangci-lint + eslint
make fmt                  # gofmt + prettier
```
