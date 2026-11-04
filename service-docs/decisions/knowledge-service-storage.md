# knowledge service storage

## Why the SQLite Index Is Derived and DOCS_PATH Has No Default

### Decision

Markdown files in `DOCS_PATH` are the source of truth. `knowledge.db` is a
**derived** search index, rebuilt from disk on every startup. `DOCS_PATH` is a
required environment variable with no default — the server exits if it is unset.

### Why derived, not authoritative

The corpus stays greppable, diffable, and hand-editable. A manual edit to a
`.md` file is picked up on the next restart with no migration step, and the
whole index can be thrown away and rebuilt. `write_knowledge` therefore writes
the `.md` file first, then re-indexes only that file via `IngestOne` — O(one
file) instead of O(corpus).

### Why no default for DOCS_PATH

A relative default like `docs/` would resolve against whatever directory each
MCP client session happened to start in. Two Claude Code sessions launched from
different folders would silently build two different knowledge bases and each
would look empty to the other. Failing loudly at startup is better than
silently splitting the corpus.

`Open()` also rejects a database whose `PRAGMA user_version` is newer than the
binary supports, so an old binary never half-reads a newer schema.

### Consequences

- `delete_knowledge` removes the file from disk, not just the index — otherwise
  startup ingest would resurrect it.
- `Ingest` prunes DB rows whose `.md` files no longer exist; without that step,
  deleted files would persist in the index forever.
- Concurrent processes (several Claude Code sessions) serialise writes through
  `.knowledge.lock` in `DOCS_PATH`, with a 10s acquire timeout and stale-lock
  stealing after 30s.
