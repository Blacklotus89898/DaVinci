# knowledge-service

A lightweight MCP server that gives AI assistants a persistent, searchable knowledge base.
Write runbooks, solutions, and one-liners into it from conversations; future sessions search it before answering. Built for SRE and DevOps workflows.

## What it does

- **Stores** markdown chunks (runbooks, solutions, tools, guides) in a single SQLite file
- **Searches** them using hybrid BM25 full-text + Ollama semantic vector retrieval, merged via Reciprocal Rank Fusion
- **Exposes** 5 tools over [Model Context Protocol](https://modelcontextprotocol.io/) (stdio) or HTTP (for non-MCP clients)
- **Self-improves**: after every debugging session the agent writes what it learned back into the knowledge base, making every future session smarter

## Quick Start

**Prerequisites**: Go 1.24+, Make

```bash
# Build both binaries
make build

# Index the bundled docs
make ingest

# Try a search
./bin/knowledge search "how does search work"

# Install to ~/.local/bin (for OpenCode / PATH access)
make install
```

## Wiring into Claude Code

```bash
claude mcp add knowledge /path/to/knowledge-service \
  -e DB_PATH=/path/to/knowledge.db \
  -e EMBED_PROVIDER=ollama \
  -e EMBED_MODEL=nomic-embed-text \
  -e LOG_LEVEL=warn \
  --scope user
```

The `search_knowledge`, `list_knowledge`, `write_knowledge`, `delete_knowledge`, and `get_tool` tools are then available in every Claude Code session. No restart needed — takes effect immediately.

## Wiring into OpenCode

Copy `opencode.json` to your workspace root and restart OpenCode. Edit the paths to match your install location.

## Tools

| Tool | What it does |
|---|---|
| `search_knowledge` | Hybrid BM25 + Ollama semantic search. Call this before answering any SRE/DevOps question. |
| `list_knowledge` | List all documents and headings. Use to discover what exists before writing. |
| `write_knowledge` | Save a new chunk (path, **heading required**, content). Call this after solving any non-trivial problem. |
| `delete_knowledge` | Remove an outdated or superseded document by path. |
| `get_tool` | Retrieve a stored script or one-liner by name. Searches `tools/` prefix only; returns raw executable code. |

## Architecture

```
Query
  ├─ FTS5 MATCH "token1* AND token2*" (OR fallback)  →  BM25 ranked list
  └─ Ollama nomic-embed-text (768-dim)               →  cosine sim ranked list
         ↓
  Reciprocal Rank Fusion (FTS weight 4×, vec weight 0.5×, k=60)
         ↓
  LRU cache (256 slots, sub-µs hit)
         ↓
  Top-k results
```

**Storage**: SQLite WAL mode — `documents`, `chunks` (with vector blob), `chunks_fts` (FTS5 virtual table), `vocab` (IDF values). Single portable `.db` file.

**Embedding (active)**: Ollama `nomic-embed-text` — 768-dim dense semantic vectors via local Ollama. Catches paraphrases and intent that keyword search misses. Requires `ollama pull nomic-embed-text` and `EMBED_PROVIDER=ollama`.

**Embedding (fallback)**: TF-IDF with feature hashing — 1024-dim, offline, zero deps. Heading-boost (3×) and SRE synonym expansion (`OOMKilled ↔ oom`, `CrashLoopBackOff ↔ crashloop`, etc.). Used automatically if Ollama is unavailable.

See [docs/architecture.md](docs/architecture.md) for diagrams, embedding details, and known limitations.

## Document Format

Markdown files are split at `#` and `##` headings. Each heading + its body becomes one searchable chunk retrieved independently. Write self-contained sections.

### Path conventions

| Path prefix | Content type | Retrieved via |
|---|---|---|
| `runbooks/<service>-<symptom>.md` | Incident runbooks | `search_knowledge` |
| `solutions/<topic>.md` | One-time fixes with context | `search_knowledge` |
| `tools/<name>.md` | Executable scripts, one-liners | `get_tool("<name>")` |
| `guides/<topic>.md` | How-to guides | `search_knowledge` |
| `architecture/<component>.md` | Architecture notes | `search_knowledge` |

`tools/` entries must contain a fenced code block. `get_tool` extracts and returns the first one as raw executable code.

### Write-back template (for agents)

```
## Symptom
[What the alert or user saw]

## Root Cause
[Why it happened]

## Fix
```bash
# Exact commands
```

## Prevention
[How to stop recurrence]
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DB_PATH` | `knowledge.db` | Path to the SQLite database |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `EMBED_PROVIDER` | _(unset = TF-IDF)_ | Set to `ollama` for semantic embeddings |
| `EMBED_URL` | `http://localhost:11434` | Ollama base URL (only used when `EMBED_PROVIDER=ollama`) |
| `EMBED_MODEL` | `nomic-embed-text` | Ollama model name (only used when `EMBED_PROVIDER=ollama`) |
| `EMBED_DIMS` | `1024` | TF-IDF vector dimension (only used when provider is TF-IDF) |
| `AUTH_TOKEN` | _(unset = no auth)_ | Bearer token required for HTTP mode requests |

## HTTP Mode (non-MCP clients)

```bash
knowledge-service --http :3737
```

Exposes:
- `POST /mcp` — JSON-RPC 2.0, same as stdio
- `GET /schema.json` — OpenAI function-calling schema (for GPT-4o, Gemini, Ollama)
- `GET /health` — readiness probe

> **Note**: Set `AUTH_TOKEN` to require `Authorization: Bearer <token>` on all requests (the `/health` endpoint is always exempt). Without it the endpoint is open — run it on localhost or behind a firewall.

## Building

```bash
make build      # builds bin/knowledge-service and bin/knowledge
make tidy       # go mod tidy
make clean      # removes bin/ and knowledge.db
```

The binary is ~14 MB, statically linked, no CGo required.

## Testing

```bash
go test -race ./...
```

47 tests across chunker, embed, store, and mcp packages.

## Known Limitations

- **Linear vector scan**: all chunk vectors are loaded into RAM and scanned per query. Fine under ~5,000 chunks; an ANN index would be needed beyond that.
- **No metadata filtering**: `search_knowledge` searches all paths; use `list_knowledge("runbooks/")` to scope discovery first.
- **Paraphrases in TF-IDF mode**: `"disk full"` and `"no space left on device"` won't match each other without Ollama. Use `EMBED_PROVIDER=ollama` (recommended) or write runbooks with both forms.
- **Provider switch requires re-ingest**: run `make ingest` after changing `EMBED_PROVIDER` so all stored vectors use the new embedding.

## License

[LICENSE](LICENSE)
