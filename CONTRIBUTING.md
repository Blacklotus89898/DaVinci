# Contributing to knowledge-service

## Prerequisites

- Go 1.24+
- [Ollama](https://ollama.ai) with `nomic-embed-text` (optional but recommended)
- Make

## Local setup

```bash
git clone https://github.com/blacklotus88888/knowledge-service
cd knowledge-service

# Build both binaries
make build

# Pull the embedding model (one-time, ~274 MB)
ollama pull nomic-embed-text

# Ingest the bundled docs
EMBED_PROVIDER=ollama make ingest

# Run all tests
go test -race ./...
```

## Project layout

```
cmd/
  knowledge-service/   # MCP server + HTTP server entry point
  knowledge/           # CLI (ingest, search)
internal/
  cache/               # Generic LRU cache (thread-safe)
  chunker/             # Markdown → chunks splitter
  embed/               # TF-IDF vectorizer + Ollama provider
  store/               # SQLite store, hybrid search, ingest
mcp/                   # JSON-RPC 2.0 MCP server implementation
docs/                  # Bundled knowledge base seed docs
```

## Development workflow

```bash
make build        # compile
make tidy         # go mod tidy
go test -race ./...             # full test suite with race detector
go vet ./...                    # static analysis
```

## Submitting changes

1. Fork the repo and create a branch: `git checkout -b feat/my-change`
2. Write tests for any new behaviour. The test suite must stay green with `-race`.
3. Run `go vet ./...` before opening a PR.
4. Open a PR against `main`. Fill in the PR template.

## Commit style

```
<type>: <short description>

Types: feat, fix, refactor, test, docs, chore
```

Examples: `feat: add path-filter param to get_tool`, `fix: drain HTTP body on non-200 Ollama response`

## Adding a new embedding provider

1. Implement `embed.Provider` (`Dims() int`, `Embed(text string) ([]float32, error)`).
2. Wire it in `cmd/knowledge-service/main.go` alongside the Ollama branch.
3. Add tests in `internal/embed/`.
4. Document the new env vars in `README.md`.

## Reporting bugs

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md).
Include: OS, Go version, Ollama version (if used), reproduction steps, and the full stderr output.
