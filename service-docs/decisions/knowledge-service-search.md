# knowledge service search

## Why Hybrid Search Fuses FTS and Vectors With RRF Instead of Score Blending

### Decision

Search runs two independent channels — FTS5 BM25 keyword and dense vector
cosine — and merges them with Reciprocal Rank Fusion:

```
score(doc) = wFTS/(k + rank_fts) + wVec/(k + rank_vec)
k = 60, wFTS = 2.0, wVec = 1.0
```

### Why RRF and not normalized score blending

BM25 scores and cosine similarities live on incompatible scales, and BM25's
range shifts with corpus size and query length. Any attempt to normalize them
into a shared range needs constants that have to be re-tuned as the corpus
grows. RRF only consumes **ranks**, so it is scale-free and needs no retuning.
FTS is weighted 2x because exact terminology — error strings, flag names,
`kubectl` verbs — is the higher-precision signal for this corpus.

### Consequence: the fused score is not a quality signal

RRF scores are tiny by construction (~0.03 for a top hit) and a vector-only hit
scores lower than a dual-channel hit regardless of how good it is. Labelling
results off the fused score would mark almost everything "weak".

`scoreLabel` therefore reports quality from the **underlying evidence** instead
— keyword rank and raw cosine — with thresholds that differ by provider,
because TF-IDF similarities run lower than real embeddings:

| Provider | strong | relevant |
|---|---|---|
| Ollama (neural) | cosine >= 0.60 | cosine >= 0.35 |
| TF-IDF fallback | cosine >= 0.45 | cosine >= 0.25 |

A top-3 BM25 rank also counts as "strong" on its own.

## Why SRE Synonyms Are Excluded From the FTS AND Query

### Decision

`buildFTSQuery` produces two forms. The **AND** query is built from
`TokenizeRaw` (no synonym expansion); the **OR** fallback is built from
`Tokenize` (with expansion). AND runs first for precision, OR only if AND
returns nothing.

### Why expansion breaks AND

Synonym expansion injects extra tokens: `oomkilled` also emits `oom`. In an OR
query that is pure recall gain. In an AND query it becomes a **requirement** —
`oomkilled* AND oom*` demands both tokens be present.

FTS5's `unicode61` tokenizer treats `OOMKilled` as the single token
`oomkilled`. Prefix matching means `oom*` does match it, but the pattern is
fragile in general: any expansion pair where the synonym is not a prefix of the
source term turns a precise AND query into one that matches nothing, and the
search silently falls through to the broader OR path.

Keeping expansion out of AND means the precise path only ever requires terms
the user actually typed.

### Expansion pairs

Defined in `embed.sreExpansions`: `oomkilled <-> oom`,
`crashloopbackoff <-> crashloop`, `imagepullbackoff/errimagepull <-> imagepull`,
`eviction <-> evict`, `drain -> evict`, `deadline <-> timeout`,
`unreachable <-> notready`, `diskpressure -> disk`.

Expansion also applies to TF-IDF vectorization, which is why a TF-IDF-mode
query for "oom" retrieves chunks that only say "OOMKilled".

## Why a TF-IDF Fallback Must Never Overwrite Provider Vectors

### Decision

`rebuildVocabAndVectors` skips any chunk whose stored vector has a different
dimension than the active embedder produces. A TF-IDF fallback degrades search
quality but never damages the stored semantic index.

### Why it matters

If Ollama is unreachable at startup the server logs a warning and falls back to
TF-IDF. Without the dimension guard, the very next write would re-embed the
entire corpus at 1024-dim TF-IDF, destroying the 768-dim `nomic-embed-text`
vectors. Recovering means a full re-ingest with Ollama back up — and the
failure is silent, because search still "works", just worse.

The rule: **search may degrade; the index must not.**

### Mechanics

- Ollama mode: embed only chunks whose vector is missing or foreign-dimensioned
  (`c.vecLen != wantBytes`). Ollama embeddings are corpus-independent, so no
  IDF recomputation is needed.
- TF-IDF mode: re-embed all TF-IDF-dimensioned chunks, because IDF shifts
  globally with every write — but skip any chunk carrying a foreign-dimensioned
  vector.
- `SetProvider` warns at startup when stored vector length divided by 4 does not
  match the provider's dimension.

### Operational consequence

After changing `EMBED_PROVIDER` or `EMBED_MODEL`, stop the server, run
`make ingest` to re-vectorize the corpus, then restart. Running `make ingest`
against a live server on the same `DB_PATH` risks SQLite write conflicts.
