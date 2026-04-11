# Contributing to Seam

Thanks for your interest in contributing. Seam is a local-first knowledge system built from a Go backend, a React web frontend, and a Bubble Tea TUI. This guide covers how to get set up, how to make changes, and what we expect from a pull request.

If you are new to the project, skim [`README.md`](README.md) first for the pitch and [`docs/architecture.md`](docs/architecture.md) for the lay of the land.

## Ways to contribute

- **Report a bug.** Open a GitHub issue with steps to reproduce, what you expected, and what happened instead. Include your OS, Go version, and relevant log lines from `make logs` if applicable.
- **Propose a feature.** Open an issue first to discuss fit and scope before writing code. Seam is intentionally opinionated; not every idea lands.
- **Fix a bug or land a feature.** Pick an open issue (or one you filed), comment that you are working on it, and send a pull request.
- **Improve docs.** Corrections, clarifications, and missing examples under `docs/` are always welcome.

## Development setup

Prerequisites, installation, and configuration walkthrough live in [`docs/getting-started.md`](docs/getting-started.md). The short version:

```bash
git clone https://github.com/0x3k/seam.git
cd seam
make build   # builds bin/seamd, bin/seam, bin/seam-reindex
make init    # interactive config (JWT secret, data dir, LLM provider, Chroma)
make dev     # runs seamd + Vite + Chroma in parallel
```

All day-to-day build, run, test, and lint targets are documented in [`docs/development.md`](docs/development.md). `make help` lists every target with a one-line description.

## Coding conventions

Before writing code, read [`AGENTS.md`](AGENTS.md). It covers Go style, package layout, error handling, logging, database and HTTP conventions, testing patterns, security invariants, and the frontend rules for the React app. The highlights:

- Go 1.25+, no CGO, pure-Go SQLite driver. Format with `gofmt`.
- Strict layering: `cmd/` -> `internal/server` -> `internal/{domain}`. No package imports `internal/server`.
- Domain packages follow `handler.go` / `service.go` / `store.go` / `*_test.go`.
- Domain errors are `Err{Condition}` sentinels wrapped with `fmt.Errorf("pkg.Service.Method: %w", err)` and mapped to HTTP status codes in handlers.
- IDs are ULIDs everywhere. Never UUID.
- Frontend is React 19 + TypeScript + Zustand + CSS Modules. Icons come from Lucide only. Dark theme only.
- No emojis in code or comments. No attribution lines in commits, PRs, or code.

## Testing

Every change that touches code needs tests. See [`docs/development.md`](docs/development.md#testing) for the full matrix; the common entry points:

```bash
make test               # Go unit tests
make test-race          # Go unit tests with race detector
make test-integration   # real filesystem, on-disk SQLite
make test-web           # frontend tests (Vitest)
make lint               # golangci-lint + eslint
make typecheck          # TypeScript typecheck
```

Unit tests must not call real external services. Mock Ollama and ChromaDB with `httptest.NewServer()`. Integration tests (build tag `integration`) are the right place for real filesystem and on-disk SQLite coverage.

## Commit style

- Write small, focused commits. One logical change per commit.
- Use imperative mood in the subject: `fix(note): reject paths containing ..`, not `fixed paths`.
- Prefer conventional-style prefixes (`feat`, `fix`, `chore`, `docs`, `refactor`, `test`) when they fit naturally; match the repo's recent history (`git log --oneline`) rather than inventing a new style.
- Keep the subject under ~72 characters; put detail in the body.
- Do not include credit or attribution lines (no `Co-Authored-By`, no `Generated with ...`).

## Pull requests

Before opening a PR:

1. Rebase onto the latest `main`.
2. Run `make fmt`, `make lint`, `make vet`, `make typecheck`, and the relevant test targets. PRs that fail CI will be asked to fix the basics before review.
3. Update docs under `docs/` if behavior, commands, or config changed.
4. If your change affects the database schema, update `migrations/001_initial.sql` (single flattened migration) and make sure it is idempotent.
5. If your change affects the API, update [`docs/api.md`](docs/api.md). If it affects MCP tools, update [`docs/mcp.md`](docs/mcp.md).

In the PR description, explain:

- **What** the change does.
- **Why** it is needed (link the issue if one exists).
- **How** you tested it -- which tests you added, which manual flows you exercised.
- Any migration or config impact for existing users.

Expect review feedback. Seam favors small, reviewable changes; large or scope-creeping PRs will usually be asked to split.

## Security

Do not open public issues for security vulnerabilities. See [`docs/security.md`](docs/security.md) for the threat model and reporting guidance, and follow the instructions there for private disclosure.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE) that covers the project.
