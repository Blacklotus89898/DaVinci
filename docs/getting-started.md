# Knowledge Service

A local MCP server that indexes your documentation and exposes a hybrid
BM25 + TF-IDF search tool to AI coding assistants.

## Architecture

The service stores chunks in SQLite (WAL mode) and provides two retrieval paths:

- **FTS5 BM25** — keyword relevance via SQLite's built-in full-text search; AND-first with OR fallback
- **TF-IDF cosine** — semantic proximity using the hashing trick (1024 dims, heading boost 3×, SRE synonym expansion)
- **RRF fusion** — Reciprocal Rank Fusion combines both lists (FTS weight 4×, vector weight 0.5×, k=60)
- **Ollama (optional)** — set `EMBED_PROVIDER=ollama` for dense semantic embeddings

Results are cached in an in-process LRU (256 slots). A cold search takes ~5 ms;
a cache hit is sub-microsecond.

## Quick Start

```bash
# Build both binaries
make build

# Index docs/
make ingest

# Try a search
./bin/knowledge search "how does ingestion work"

# Install to ~/.local/bin and wire into OpenCode
make install
```

## Document Format

Markdown files are split on `#` and `##` headings. Each heading + the content
that follows it becomes one searchable chunk. Aim for self-contained sections —
each chunk is retrieved independently.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DB_PATH` | `knowledge.db` | Path to the SQLite database |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `EMBED_PROVIDER` | _(TF-IDF)_ | Set to `ollama` to use Ollama for semantic embeddings |
| `EMBED_URL` | `http://localhost:11434` | Ollama base URL |
| `EMBED_MODEL` | `nomic-embed-text` | Ollama model (`nomic-embed-text` is recommended) |
| `EMBED_DIMS` | `1024` | TF-IDF vector dimension |
| `AUTH_TOKEN` | _(none)_ | Bearer token for HTTP mode; unset = no auth |

## Adding to OpenCode

Copy `opencode.json` to your workspace root, then restart OpenCode.
The agent will use `search_knowledge` before answering and `write_knowledge`
to persist solutions for future sessions.
