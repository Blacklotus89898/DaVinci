# Knowledge Service

A local MCP server that gives AI assistants a persistent, searchable knowledge base.
Write runbooks and solutions into it from conversations; future sessions search it before answering.

## Architecture

The service stores chunks in SQLite (WAL mode) and provides two retrieval paths:

- **FTS5 BM25** — keyword relevance; AND-first with OR fallback for robustness
- **Ollama `nomic-embed-text`** — 768-dim dense semantic vectors (active); falls back to TF-IDF 1024-dim if Ollama is unavailable
- **RRF fusion** — Reciprocal Rank Fusion merges both lists (FTS weight 4×, vec weight 0.5×, k=60)

Results are cached in an in-process LRU (256 slots). A cold search takes ~10 ms with Ollama; a cache hit is sub-microsecond.

## Quick Start

```bash
# 1. Build both binaries
make build

# 2. Pull the embedding model (one-time, ~274 MB)
ollama pull nomic-embed-text

# 3. Index docs/ with semantic embeddings
EMBED_PROVIDER=ollama bin/knowledge.exe ingest

# 4. Try a search
EMBED_PROVIDER=ollama bin/knowledge.exe search "pod keeps crashing"

# 5. Wire into Claude Code (global, persists across sessions)
claude mcp add knowledge /path/to/knowledge-service.exe \
  -e DB_PATH=/path/to/knowledge.db \
  -e EMBED_PROVIDER=ollama \
  -e EMBED_MODEL=nomic-embed-text \
  -e LOG_LEVEL=warn \
  --scope user
```

## Document Format

Markdown files are split on `#`, `##`, and `###` headings. Each heading + the content
that follows it becomes one searchable chunk. Aim for self-contained sections —
each chunk is retrieved independently.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DB_PATH` | `knowledge.db` | Path to the SQLite database |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `EMBED_PROVIDER` | _(TF-IDF)_ | Set to `ollama` to use Ollama for semantic embeddings |
| `EMBED_URL` | `http://localhost:11434` | Ollama base URL |
| `EMBED_MODEL` | `nomic-embed-text` | Ollama model — recommended: `nomic-embed-text` (274 MB, CPU-friendly) |
| `EMBED_DIMS` | `1024` | TF-IDF vector dimension (ignored when `EMBED_PROVIDER=ollama`) |
| `AUTH_TOKEN` | _(none)_ | Bearer token for HTTP mode; unset = no auth |

## Wiring into Claude Code

```bash
claude mcp add knowledge "C:\path\to\knowledge-service.exe" \
  -e DB_PATH="C:\path\to\knowledge.db" \
  -e EMBED_PROVIDER=ollama \
  -e EMBED_MODEL=nomic-embed-text \
  -e LOG_LEVEL=warn \
  --scope user
```

Restart Claude Code. The `search_knowledge`, `list_knowledge`, `write_knowledge`, `delete_knowledge`, and `get_tool` tools are available in every session.

> **Note**: If Ollama is not running when the server starts, it logs a warning and falls back to TF-IDF automatically — no crash.
