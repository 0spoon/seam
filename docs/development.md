# Development

## Build & Run

```bash
make init                 # interactive config setup (JWT, data dir, LLM provider)
make build                # build seamd + seam to ./bin/
make run                  # build and run the server
make dev-web              # React dev server (Vite on :5173, proxies /api to :8080)
make clean                # remove build artifacts + web/dist
```

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
