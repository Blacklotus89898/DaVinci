# DaVinci (knowledge-service) — Claude Code Instructions

Go MCP server: persistent markdown knowledge base with hybrid search (FTS5 BM25 + Ollama `nomic-embed-text` vectors, merged via RRF). Registered user-scope in Claude Code as the `knowledge` MCP.

**Read `AGENTS.md` for the mandatory knowledge-tool workflow** (search before answering, write after solving, path conventions) — it applies to every session in this repo and everywhere the `knowledge` MCP is connected.

## Commands (Makefile)

```bash
make build    # both binaries -> bin/knowledge-service, bin/knowledge
make test     # go test -race ./...
make lint     # golangci-lint run ./...
make ingest   # rebuild DB index from docs/
make install  # copy to ~/.local/bin (what the user-scoped MCP runs)
```

Layout: `cmd/{knowledge,knowledge-service}` entrypoints, `internal/{store,chunker,embed,cache}`, `mcp/` server wiring, `docs/` = the knowledge corpus itself.

## Non-negotiables

- `docs/` is the source of truth; `knowledge.db` is a derived index (gitignored) rebuilt on ingest/startup. Never hand-edit the DB.
- The MCP registration runs `bin/knowledge-service.exe` — after code changes, `make build` + restart Claude Code sessions for the server to pick them up.
- Releases flow through release-please (conventional commits); never hand-tag.
- Ollama at http://localhost:11434 — without it, search silently degrades to TF-IDF.
