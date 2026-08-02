# Changelog

All notable changes to knowledge-service are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [0.2.2](https://github.com/Blacklotus89898/DaVinci/compare/knowledge-service-v0.2.1...knowledge-service-v0.2.2) (2026-08-02)


### Bug Fixes

* gosec G104 on rows.Close and tx.Rollback in store.go ([5fe0894](https://github.com/Blacklotus89898/DaVinci/commit/5fe08943bc45b1541c42adc75fcb9742634639b8))

## [0.2.1](https://github.com/Blacklotus89898/DaVinci/compare/knowledge-service-v0.2.0...knowledge-service-v0.2.1) (2026-08-02)


### Features

* add Ollama nomic-embed-text as active embedding provider ([ef3fa08](https://github.com/Blacklotus89898/DaVinci/commit/ef3fa08790f6b32c12ef2fc5fd980a230d9b3a06))
* adding ci/cd ([b3b40b0](https://github.com/Blacklotus89898/DaVinci/commit/b3b40b0bc8067926de9d6e17de5144c310ab932d))
* knowledge-service v1 — hybrid BM25+TF-IDF MCP server ([2c52a73](https://github.com/Blacklotus89898/DaVinci/commit/2c52a73625345e2355174c2ec27310acc6856fd8))
* make markdown the unconditional source of truth for all knowledge ([4989f4c](https://github.com/Blacklotus89898/DaVinci/commit/4989f4cad28c44d48b569ff9f87577f0917f55df))
* ship v0.1.0 — production-ready, contributable, markdown-as-source-of-truth ([8da806e](https://github.com/Blacklotus89898/DaVinci/commit/8da806e51dbadfc78a6d9da587bce5b330112f3f))


### Bug Fixes

* apply 15 code review findings + add CI/release workflows ([d491aca](https://github.com/Blacklotus89898/DaVinci/commit/d491acad8d73e316e5b2c3f1eb48592373c3d27f))
* extend errcheck exclusions for golangci-lint v2 ([1c50fd3](https://github.com/Blacklotus89898/DaVinci/commit/1c50fd3c5665b9a4802acecc3242a9a29754af3b))
* kill remaining 31 lint issues in one shot ([a6636cd](https://github.com/Blacklotus89898/DaVinci/commit/a6636cd3809fc8e55de9f1c9f3e0e4516feacfc7))
* markdown path normalization, DOCS_PATH wiring, and README refresh ([fbcc05e](https://github.com/Blacklotus89898/DaVinci/commit/fbcc05e6f45d4ce18b81f396984155ffc6b10b7d))
* remove unparseable exclude-function, add nolint to Close() callers, bump manifest to v0.2.0 ([a822d7b](https://github.com/Blacklotus89898/DaVinci/commit/a822d7b91228cb0ee02108bcdba7b9af44b4c773))
* resolve all 43 golangci-lint v2 issues ([03230f9](https://github.com/Blacklotus89898/DaVinci/commit/03230f92aad2b29793fe43e0d9e689e6b230304c))
* resolve all CI failures — lint, coverage, govulncheck, security ([007c753](https://github.com/Blacklotus89898/DaVinci/commit/007c75304ea61a360f9f681b41b4999f700dd551))
* update golangci-lint config to v2 format ([ea6280d](https://github.com/Blacklotus89898/DaVinci/commit/ea6280d1006dc5132c141de4b664b6e61d84c1aa))

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
