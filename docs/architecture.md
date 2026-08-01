# Architecture

## System Overview

```mermaid
graph TD
    subgraph Clients["Clients (any model)"]
        CC[Claude Code<br/>stdio MCP]
        OC[OpenCode<br/>stdio MCP]
        GPT[GPT-4o / Gemini<br/>HTTP REST]
        OL[Ollama / local<br/>HTTP REST]
    end

    subgraph Server["knowledge-service binary"]
        STDIO[stdio transport<br/>JSON-RPC 2.0]
        HTTP[HTTP transport<br/>POST /mcp · GET /schema.json]
        TOOLS[Tool handlers<br/>search · list · write · delete · get_tool]
        SEARCH[Hybrid search<br/>FTS5 BM25 + TF-IDF → RRF]
        LRU[LRU cache<br/>256 slots · sub-µs hit]
    end

    subgraph Storage["SQLite WAL"]
        DOCS[(documents)]
        CHUNKS[(chunks + vectors)]
        FTS[(chunks_fts<br/>FTS5 BM25)]
        VOCAB[(vocab · IDF)]
    end

    CC -->|newline-delimited JSON| STDIO
    OC -->|newline-delimited JSON| STDIO
    GPT -->|POST /mcp or REST| HTTP
    OL -->|POST /mcp or REST| HTTP

    STDIO --> TOOLS
    HTTP --> TOOLS
    TOOLS --> SEARCH
    SEARCH --> LRU
    LRU -->|miss| DOCS
    LRU -->|miss| CHUNKS
    LRU -->|miss| FTS
    CHUNKS --- VOCAB
```

## Retrieval Pipeline

```mermaid
flowchart LR
    Q[query] --> TOK[tokenize + SRE synonyms]
    TOK --> FTS5["FTS5 MATCH<br/>AND-first · OR fallback"]
    TOK --> EMB["Ollama nomic-embed-text<br/>768-dim · falls back to TF-IDF 1024-dim"]
    FTS5 -->|BM25 ranked list| RRF
    EMB -->|cosine sim ranked list| RRF
    RRF["Reciprocal Rank Fusion<br/>FTS weight 4× · vec weight 0.5× · k=60"]
    RRF --> TOP[top-k results]
    TOP --> CACHE["LRU cache<br/>key = query + limit"]
```

## Self-Improvement Loop

```mermaid
sequenceDiagram
    participant U as User
    participant C as Claude
    participant K as knowledge-service
    participant DB as SQLite

    U->>C: "ArgoCD is crashing with OOM"
    C->>K: search_knowledge("argocd oom crash")
    K->>DB: FTS5 + vector search
    DB-->>K: ranked results
    K-->>C: existing runbooks (if any)
    C->>U: diagnosis + fix
    Note over C: session ends
    C->>K: write_knowledge(path, heading, content)
    K->>DB: upsert chunk + FTS + vector
    Note over DB: permanent memory
    Note over C,K: Next session: Claude searches first
```

## Knowledge Layout Convention

```
knowledge.db  ←  single SQLite file, portable
docs/         ←  source markdown (batch-ingested)
  getting-started.md
  architecture.md     ← this file
  tools-guide.md

Written at runtime via write_knowledge:
  runbooks/<service>-<symptom>.md   ← incident runbooks
  solutions/<topic>.md              ← one-time fixes
  tools/<name>.md                   ← executable scripts / one-liners
  guides/<topic>.md                 ← how-tos
  architecture/<component>.md       ← architecture notes
```

## Transport Modes

| Mode | Flag | Used by |
|---|---|---|
| stdio MCP | _(default)_ | Claude Code, OpenCode |
| HTTP MCP | `--http :3737` | Any HTTP client, Cursor, web tools |

The HTTP mode exposes `POST /mcp` (same JSON-RPC) and `GET /schema.json` (OpenAI function-calling format) so any model can call the same tools without needing native MCP support.

## Embedding

Two embedding modes are supported; switch via the `EMBED_PROVIDER` env var.

### TF-IDF (default, no dependencies)

```
Tokenize + SRE synonym expansion
  → TermFreq (heading tokens weighted 3×)
  → IDF lookup (vocab table, rebuilt after every write)
  → FNV-32a hashing trick → 1024-dim float32
  → L2 normalize
```

- Dimensions: 1024 (was 512; halves collision rate)
- IDF: `log((N+1)/(df+1)) + 1` (sklearn smooth), recomputed over **all chunks** after every `write_knowledge` call and every `ingest` run — no staleness
- Heading tokens repeated 3× before computing TF so headings carry proportionally more weight
- SRE synonym pairs hard-wired in `Tokenize`: `OOMKilled ↔ oom`, `CrashLoopBackOff ↔ crashloop`, `evict ↔ eviction`, `drain ↔ evict`, `timeout ↔ deadline`, `imagePullBackOff ↔ imagepull`, `notReady ↔ unreachable`

### Ollama (optional, semantic)

Set `EMBED_PROVIDER=ollama` to call a local Ollama instance instead of TF-IDF.
Recommended model: `nomic-embed-text` (768-dim, runs on CPU, excellent quality).

```
EMBED_PROVIDER=ollama
EMBED_URL=http://localhost:11434    # default
EMBED_MODEL=nomic-embed-text        # default
```

After switching providers, run `make ingest` to re-vectorize all existing chunks.

**Search quality comparison**:

| Scenario | TF-IDF | Ollama |
|---|---|---|
| Exact term (`OOMKilled`) | ✓ excellent (FTS5 BM25) | ✓ excellent |
| Partial prefix (`kube*`) | ✓ good | ✓ good |
| SRE synonyms (`oom` / `OOMKilled`) | ✓ via expansion map | ✓ semantic |
| General paraphrases | ✗ | ✓ |
| Semantic intent (`slow` ↔ `high latency`) | ✗ | ~ partial |

## Known Limitations

| Limitation | Status |
|---|---|
| Linear vector scan O(n) | Fine to ~5,000 chunks; LRU caches repeated queries |
| No metadata/tag filtering | Use `list_knowledge("runbooks/")` to scope, then search |
| HTTP endpoint unauthenticated | Set `AUTH_TOKEN` env var to enable Bearer auth |
| Switching embedding providers requires re-ingest | Run `make ingest` after changing `EMBED_PROVIDER` |
| General paraphrases without Ollama | Use Ollama mode or write runbooks with multiple term forms |
