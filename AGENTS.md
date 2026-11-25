# AGENTS.md — DaVinci (knowledge-service)

Guide for any coding agent (Claude Code, Codex, Cursor, Copilot, Gemini…) and human contributor.
Two parts: **developing this repo** (below), and **using the knowledge tools** (the rest of this
file — other repos, e.g. Homelab, point here for those rules).

## Developing this repo

Go MCP server: a persistent markdown knowledge base with hybrid search (FTS5 BM25 + Ollama
`nomic-embed-text` vectors, merged via RRF; TF-IDF fallback). `docs/` is both the corpus and the
source of truth; `knowledge.db` is a derived index. Architecture: `README.md`,
`service-docs/architecture.md`; contributor details: `CONTRIBUTING.md`.

```bash
make build            # bin/knowledge-service + bin/knowledge (or: pwsh scripts/setup.ps1 / sh scripts/setup.sh)
make test             # go test -race ./...  — must stay green
make lint             # golangci-lint run ./...
go vet ./...
make ingest           # rebuild the index from docs/ (stop live servers first if EMBED_* changed)
```

Layout: `cmd/knowledge-service` (MCP stdio + HTTP), `cmd/knowledge` (CLI), `internal/{store,chunker,embed,cache,config,gitsync,lock}`,
`mcp/` (JSON-RPC server + tool definitions), `scripts/` (setup, kb-sync), `docs/` (the corpus).

Non-negotiables:
- `docs/` is the source of truth; never hand-edit `knowledge.db` or make correctness depend on it.
- Search degrades, the index never does: provider failures fall back to TF-IDF without overwriting stored vectors.
- Settings are self-located (env → `<repo>/knowledge.env` → defaults); never resolve paths from the caller's cwd.
- Writes stay serialized through `.knowledge.lock`; KB git-sync only commits on the configured branch.
- Releases via release-please — conventional commits, never hand-tag.

Adding a feature: branch `feat/<topic>` → tests (with `-race`) → `go vet` + lint → PR using the
template, conventional title. New MCP tool: define it in `mcp/`, test it in `mcp/server_test.go`,
document it in the README tools table and in the tool table below. New embedding provider: see
`CONTRIBUTING.md`. Corpus-only changes (`docs/**`) may go straight to `main` via `scripts/kb-sync`.

## Knowledge Base — Use These Tools

| Tool | When to use |
|---|---|
| `list_knowledge` | Start of any SRE/DevOps task — discover what runbooks already exist |
| `search_knowledge` | Before answering any non-trivial question |
| `read_knowledge` | A search preview ends in `[...truncated` or you are about to follow a runbook — read the full section (`path` + `heading` exactly as the result shows them) |
| `write_knowledge` | After solving a problem, resolving an incident, or finding something non-obvious. **heading is required** — without it the write is rejected. |
| `delete_knowledge` | When a runbook is outdated, wrong, or superseded |
| `get_tool` | Retrieve a stored script/one-liner by name. Searches `tools/` prefix only — won't return runbooks or docs. |

### Search Tips

- Use **concrete SRE terms**: service names, error messages, exit codes, kubectl commands
- Try the exact error string first (`OOMKilled`, `CrashLoopBackOff`, `no space left on device`)
- If nothing returns, broaden: `search_knowledge("oom memory")` instead of `"OOMKilled"`
- Scope with `path_prefix` when you know the category: `path_prefix: "runbooks/"` or `"tools/"`
- The search uses FTS5 BM25 (keyword) + Ollama `nomic-embed-text` semantic vectors merged via RRF
- Exact terms score highest via BM25; paraphrases and synonyms are caught by the semantic channel
- Prefix matching is on: `kube` hits `kubernetes`, `argo` hits `argocd`
- SRE synonym expansion: `OOMKilled` → also matches `oom`; `CrashLoopBackOff` → `crashloop`; `eviction/drain` → `evict`
- A result tagged `⚠ possibly stale` / `⚠ never verified` describes code that moved since a human checked it — confirm against the code before acting on it
- Paths under a mount prefix (e.g. `csus/`) are read-only team docs: propose changes as a PR in that repo, not via `write_knowledge`
- `get_tool("drain node")` matches `tools/drain-node.md` **exactly** (spaces → hyphens); fuzzy search over `tools/` is only the fallback

## Workflow

### Before answering

1. Call `list_knowledge` (no filter) to orient yourself — see what categories exist.
2. Call `search_knowledge` with the user's question. Use concrete terms: service names, error messages, kubectl commands, pod names.
3. Use the returned context. Cite the source path.

### After solving

1. Call `write_knowledge` immediately after closing an incident or solving a non-trivial issue.
2. Write content a future on-call could act on in the dark at 3am:
   - What the symptom was
   - Root cause
   - Exact commands / manifests to apply
   - How to prevent recurrence

### Pruning

Call `delete_knowledge` when:
- A runbook references old image tags, deprecated flags, or removed resources
- A fix was superseded by a permanent infra change
- You wrote something incorrect and want to replace it

## Path Conventions

| Category | Path prefix | Retrieved via |
|---|---|---|
| Incident runbooks | `runbooks/<service>-<symptom>.md` | `search_knowledge` |
| One-time fixes | `solutions/<topic>.md` | `search_knowledge` |
| Executable scripts / one-liners | `tools/<name>.md` | `get_tool("<name>")` |
| How-to guides | `guides/<topic>.md` | `search_knowledge` |
| Architecture notes | `architecture/<component>.md` | `search_knowledge` |
| Settled rationale ("why is it built this way") | `decisions/<topic>.md` | `search_knowledge` |
| Environment-specific facts (hosts, ports, quirks) | `environment/<component>.md` | `search_knowledge` |

A `tools/` entry must contain a fenced code block — `get_tool` extracts the first one and returns raw code for execution.

### What not to write

- **Todos and open questions.** They match a symptom's keywords, outrank real runbooks, and return no fix. Keep them in a `todo.md` outside the corpus.
- **Speculative ideas.** `decisions/` is for choices already made. Unsettled ideas expire and pollute.
- **Anything a competent model already knows.** Generic advice costs retrieval precision without adding information. Write what is true about *this* environment — versions, hostnames, port allocations, the workaround that only applies here.

## Write-back Template

The `heading` argument becomes the file's `##` section. Structure the `content` with `###` subsections — any `##` you pass inside content is demoted to `###` automatically, and re-writing the same heading replaces its whole section (stale subsections do not linger):

```
heading: "ArgoCD OOM — Pod Accumulation"
content:
### Symptom
[What the user/alert saw]

### Root Cause
[Why it happened]

### Fix
```bash
# Exact commands
```

### Prevention
[How to stop it from happening again]
```

## Examples

```json
{
  "path": "runbooks/argocd-oom.md",
  "heading": "ArgoCD OOM — Pod Accumulation",
  "content": "### Symptom\nArgoCD OOMKilled. Disk pressure on nodes.\n\n### Root Cause\nCompleted pods not GC'd, fill /var/lib/kubelet.\n\n### Fix\nkubectl apply -f infrastructure/pod-cleanup/\n\n### Prevention\nPod-cleanup CronJob runs every 15 min."
}
```
