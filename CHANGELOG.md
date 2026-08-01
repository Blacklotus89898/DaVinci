# Changelog

All notable changes to knowledge-service are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [0.1.0] — 2026-08-01

Initial public release.

### Added
- **Hybrid search** — BM25 FTS5 + optional Ollama nomic-embed-text semantic embeddings, merged via Reciprocal Rank Fusion
- **MCP server** — JSON-RPC 2.0 over stdio (Claude Code, OpenCode) and HTTP (any OpenAI-compatible client)
- **Five MCP tools**: `search_knowledge`, `list_knowledge`, `write_knowledge`, `delete_knowledge`, `get_tool`
- **CLI** (`knowledge ingest` / `knowledge search`) for batch ingestion and ad-hoc queries
- **Markdown-as-source-of-truth** — set `DOCS_PATH` to have `write_knowledge` write `.md` files and re-ingest; the DB is a derived index
- **SRE synonym expansion** — queries for `OOMKilled` also match `oom`, `CrashLoopBackOff` ↔ `crashloop`, etc.
- **Thread-safe LRU cache** with full `sync.Mutex` on all paths (including eviction via `MoveToFront`)
- **SQLite WAL mode** with `busy_timeout=5000` for concurrent HTTP reads
- **Schema versioning** via `PRAGMA user_version` — future binary upgrades can detect and migrate stale DBs
- **Graceful shutdown** — SIGINT/SIGTERM captured; 10-second drain window before `http.Server.Shutdown`
- **Request timeout middleware** — 30-second per-request context deadline on all HTTP handlers
- **`/health` DB check** — `SELECT 1` ping returns 503 if the DB is unavailable
- **`ReadHeaderTimeout`** on the HTTP server to mitigate Slowloris attacks
- **Dockerfile** (multi-stage Alpine) + **docker-compose.yml** wiring knowledge-service with Ollama
- **CI pipeline** — test (race detector, 70% coverage gate), golangci-lint, govulncheck
- **Release pipeline** — cross-compiled binaries (linux/amd64, linux/arm64, darwin/arm64, windows/amd64) + SHA-256 checksums
- **CONTRIBUTING.md**, issue templates, and PR template

### Fixed
- `embedWith` now uses `http.NewRequestWithContext` instead of `client.Post` (noctx compliance)
- SRE synonym tokens no longer injected into FTS5 AND queries (broke recall for compound tokens like `OOMKilled`)
- `rebuildVocabAndVectors` skips chunks that already have Ollama embeddings — reduces from O(n) to O(1) Ollama calls per `write_knowledge`
- `LoadVecs` holds a full write lock for its entire duration, eliminating TOCTOU under concurrent searches
- LRU `Get` uses `sync.Mutex` (not `RWMutex`) because `MoveToFront` mutates the underlying list

[0.1.0]: https://github.com/Blacklotus89898/DaVinci/releases/tag/v0.1.0
