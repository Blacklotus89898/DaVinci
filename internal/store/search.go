package store

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blacklotus88888/knowledge-service/internal/embed"
)

const (
	rrfK         = 60
	wFTS         = 4.0
	wVec         = 0.5
	cacheSize    = 256
	minCosineSim = float32(0.01)
)

// Hybrid performs BM25 FTS + vector search and merges results via Reciprocal Rank Fusion.
func Hybrid(s *Store, query string, limit int, invalidate bool) ([]Result, error) {
	if invalidate {
		s.lru.Purge()
	}
	key := fmt.Sprintf("%s\x00%d", query, limit)
	if cached, ok := s.lru.Get(key); ok {
		return cached, nil
	}

	fts, err := ftsSearch(s, query, limit*4)
	if err != nil {
		return nil, fmt.Errorf("fts: %w", err)
	}
	vec, err := vectorSearch(s, query, limit*4)
	if err != nil {
		return nil, fmt.Errorf("vec: %w", err)
	}

	out := rrfMerge(fts, vec, limit)
	s.lru.Set(key, out)
	return out, nil
}

// InvalidateCache purges the search result cache for s.
func InvalidateCache(s *Store) { s.lru.Purge() }

// ftsSearch tries AND semantics first for precision; falls back to OR for recall.
func ftsSearch(s *Store, query string, limit int) ([]Result, error) {
	andQ, orQ := buildFTSQuery(query)
	if andQ == "" {
		return nil, nil
	}
	// AND: precise — all query tokens must appear.
	if results, _ := runFTSQuery(s, andQ, limit); len(results) > 0 {
		return results, nil
	}
	// OR fallback: broader recall.
	if orQ != andQ {
		if results, _ := runFTSQuery(s, orQ, limit); len(results) > 0 {
			return results, nil
		}
	}
	return nil, nil
}

func runFTSQuery(s *Store, ftsQuery string, limit int) ([]Result, error) {
	rows, err := s.DB.Query(`
		SELECT c.id, d.path, c.heading, c.content, -bm25(chunks_fts) AS score
		FROM chunks_fts
		JOIN chunks c ON chunks_fts.rowid = c.id
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ?
		ORDER BY score DESC
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, nil // degrade gracefully on malformed query
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.Path, &r.Heading, &r.Content, &r.Score); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func vectorSearch(s *Store, query string, limit int) ([]Result, error) {
	if err := s.LoadVecs(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	vecs := s.vecs
	idf := s.idf
	s.mu.RUnlock()

	if len(vecs) == 0 {
		return nil, nil
	}

	// Embed query: use provider if set, else TF-IDF.
	var qvec []float32
	if s.provider != nil {
		v, err := s.provider.Embed(query)
		if err != nil {
			return nil, nil // degrade gracefully
		}
		qvec = v
	} else {
		// Use corpus size (chunk count) as N, matching how rebuildVocabAndVectors
		// computes idfDefault. len(idf) is vocabulary size, which is wrong here.
		idfDefault := embed.SmoothedIDF(len(vecs)+10, 0)
		tokens := embed.Tokenize(query)
		tf := embed.TermFreq(tokens)
		if tf == nil {
			return nil, nil
		}
		qvec = embed.Vectorize(tf, idf, idfDefault, s.tfidfDims)
	}

	type scoredVec struct {
		cv  ChunkVec
		sim float32
	}
	scored := make([]scoredVec, len(vecs))
	for i, cv := range vecs {
		scored[i] = scoredVec{cv, embed.CosineSim(qvec, cv.Vec)}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].sim > scored[j].sim })

	out := make([]Result, 0, limit)
	for _, sc := range scored {
		if len(out) >= limit || sc.sim < minCosineSim {
			break
		}
		out = append(out, Result{
			ID:      sc.cv.ID,
			Path:    sc.cv.DocPath,
			Heading: sc.cv.Heading,
			Content: sc.cv.Content,
			Score:   float64(sc.sim),
		})
	}
	return out, nil
}

func rrfMerge(fts, vec []Result, limit int) []Result {
	scores := make(map[int64]float64)
	meta := make(map[int64]Result)

	for i, r := range fts {
		scores[r.ID] += wFTS / float64(rrfK+i+1)
		meta[r.ID] = r
	}
	for i, r := range vec {
		scores[r.ID] += wVec / float64(rrfK+i+1)
		if _, ok := meta[r.ID]; !ok {
			meta[r.ID] = r
		}
	}

	type item struct {
		id    int64
		score float64
	}
	items := make([]item, 0, len(scores))
	for id, sc := range scores {
		items = append(items, item{id, sc})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	out := make([]Result, 0, limit)
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		r := meta[it.id]
		r.Score = it.score
		out = append(out, r)
	}
	return out
}

// buildFTSQuery returns AND and OR forms of an FTS5 MATCH expression with prefix tokens.
// The AND query uses raw tokens only (no SRE synonym expansion) so that injected synonym
// tokens like "oom*" don't break AND recall for compound FTS5 tokens like "oomkilled".
// The OR query uses the full expanded token set for broader recall.
// Single-token queries return the same string for both.
func buildFTSQuery(query string) (andQuery, orQuery string) {
	rawTokens := embed.TokenizeRaw(query)
	if len(rawTokens) == 0 {
		return "", ""
	}
	andParts := make([]string, len(rawTokens))
	for i, t := range rawTokens {
		andParts[i] = t + "*"
	}

	allTokens := embed.Tokenize(query)
	orParts := make([]string, len(allTokens))
	for i, t := range allTokens {
		orParts[i] = t + "*"
	}

	if len(andParts) == 1 && len(orParts) == 1 {
		return andParts[0], orParts[0]
	}
	return strings.Join(andParts, " AND "), strings.Join(orParts, " OR ")
}
