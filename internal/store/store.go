package store

import (
	"database/sql"
	"fmt"
	"os"
	"sync"

	"github.com/blacklotus88888/knowledge-service/internal/cache"
	"github.com/blacklotus88888/knowledge-service/internal/embed"
	_ "modernc.org/sqlite"
)

// ChunkVec holds in-memory chunk data for vector search.
type ChunkVec struct {
	ID      int64
	DocPath string
	Heading string
	Content string
	Vec     []float32
}

// Result is a single search hit.
type Result struct {
	ID      int64
	Path    string
	Heading string
	Content string
	Score   float64
}

// DocSummary is a brief overview of a document in the knowledge base.
type DocSummary struct {
	Path     string
	Title    string
	Headings []string
}

// Store wraps a SQLite connection plus cached in-memory retrieval state.
type Store struct {
	DB        *sql.DB
	lru       *cache.LRU[string, []Result]
	mu        sync.RWMutex
	idf       map[string]float64
	vecs      []ChunkVec
	provider  embed.Provider // nil → TF-IDF hashing
	tfidfDims int            // dimension for TF-IDF vectors (ignored when provider != nil)
}

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS documents (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    path        TEXT    UNIQUE NOT NULL,
    title       TEXT,
    ingested_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chunks (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    doc_id    INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_idx INTEGER NOT NULL,
    heading   TEXT,
    content   TEXT NOT NULL,
    vector    BLOB,
    UNIQUE(doc_id, chunk_idx)
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    heading,
    content,
    tokenize='unicode61'
);

CREATE TABLE IF NOT EXISTS vocab (
    term TEXT    PRIMARY KEY,
    df   INTEGER NOT NULL,
    idf  REAL    NOT NULL
);
`

// Open opens (or creates) the SQLite knowledge base at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &Store{
		DB:        db,
		lru:       cache.New[string, []Result](cacheSize),
		tfidfDims: embed.DefaultDims,
	}
	s.reloadIDF()
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.DB.Close() }

// SetProvider sets an optional neural embedding backend (e.g. Ollama).
// Must be called before any ingest or search. Pass nil to use TF-IDF.
// If the store already contains vectors from a different provider dimension,
// run `make ingest` to re-vectorize the entire corpus before searching.
func (s *Store) SetProvider(p embed.Provider) {
	if p != nil && len(s.vecs) > 0 {
		// Warn if existing in-memory vectors have a different dimension — they will
		// score 0 against the new provider's query vectors until re-ingest is run.
		if len(s.vecs) > 0 && len(s.vecs[0].Vec) != p.Dims() {
			fmt.Fprintf(os.Stderr,
				"warning: existing corpus uses %d-dim vectors but new provider uses %d-dim — run `make ingest` to re-vectorize\n",
				len(s.vecs[0].Vec), p.Dims())
		}
	}
	s.provider = p
}

// SetTFIDFDims overrides the TF-IDF vector dimension (default: embed.DefaultDims).
// Only takes effect when no Provider is set.
func (s *Store) SetTFIDFDims(n int) {
	if n > 0 {
		s.tfidfDims = n
	}
}

// SetMaxConns adjusts the DB connection pool size (increase to 4+ for HTTP mode).
func (s *Store) SetMaxConns(n int) { s.DB.SetMaxOpenConns(n) }

// vectorizeChunk produces an embedding for a heading+content pair.
// With a Provider it calls Embed(heading+"\n"+content).
// Without a Provider it uses TF-IDF with a 3× heading-token boost.
func (s *Store) vectorizeChunk(heading, content string, idf map[string]float64, idfDefault float64) []float32 {
	if s.provider != nil {
		text := heading
		if content != "" {
			text += "\n" + content
		}
		v, err := s.provider.Embed(text)
		if err != nil {
			return nil
		}
		return v
	}
	// Repeat heading tokens 3× so headings carry proportionally more TF weight.
	h := embed.Tokenize(heading)
	b := embed.Tokenize(content)
	all := make([]string, 0, len(h)*3+len(b))
	all = append(all, h...)
	all = append(all, h...)
	all = append(all, h...)
	all = append(all, b...)
	tf := embed.TermFreq(all)
	if tf == nil {
		return nil
	}
	return embed.Vectorize(tf, idf, idfDefault, s.tfidfDims)
}

// reloadIDF fetches IDF values from the vocab table into the in-memory map.
func (s *Store) reloadIDF() {
	rows, err := s.DB.Query(`SELECT term, idf FROM vocab`)
	if err != nil {
		return
	}
	defer rows.Close()
	idf := make(map[string]float64)
	for rows.Next() {
		var term string
		var val float64
		if err := rows.Scan(&term, &val); err == nil {
			idf[term] = val
		}
	}
	s.mu.Lock()
	s.idf = idf
	s.vecs = nil
	s.mu.Unlock()
}

// rebuildVocabAndVectors recomputes IDF over all chunks, updates the vocab table,
// and re-vectorizes chunks whose stored vector is outdated or missing.
//
// TF-IDF mode: re-embeds ALL chunks because IDF changes globally with every write.
// Ollama mode: skips the vocab step and only embeds chunks with a NULL vector blob,
// since Ollama embeddings are corpus-independent (no IDF recalculation needed).
func (s *Store) rebuildVocabAndVectors() {
	type chunkRow struct {
		id         int64
		heading    string
		content    string
		hasVector  bool
	}

	rows, err := s.DB.Query(`SELECT id, heading, content, (vector IS NOT NULL) FROM chunks`)
	if err != nil {
		return
	}
	var all []chunkRow
	for rows.Next() {
		var c chunkRow
		if err := rows.Scan(&c.id, &c.heading, &c.content, &c.hasVector); err == nil {
			all = append(all, c)
		}
	}
	rows.Close()

	N := len(all)
	if N == 0 {
		return
	}

	var idf map[string]float64
	idfDefault := embed.SmoothedIDF(N, 0)

	if s.provider == nil {
		// Compute document frequency over all chunks.
		df := make(map[string]int)
		for _, c := range all {
			seen := make(map[string]bool)
			for _, t := range embed.Tokenize(c.heading + " " + c.content) {
				if !seen[t] {
					df[t]++
					seen[t] = true
				}
			}
		}
		idf = make(map[string]float64, len(df))
		for term, d := range df {
			idf[term] = embed.SmoothedIDF(N, d)
		}

		// Persist fresh vocab.
		tx, err := s.DB.Begin()
		if err != nil {
			return
		}
		if _, err := tx.Exec(`DELETE FROM vocab`); err != nil {
			tx.Rollback() //nolint:errcheck
			return
		}
		stmt, err := tx.Prepare(`INSERT INTO vocab(term, df, idf) VALUES(?,?,?)`)
		if err != nil {
			tx.Rollback() //nolint:errcheck
			return
		}
		var execErr error
		for term, d := range df {
			if _, err := stmt.Exec(term, d, idf[term]); err != nil {
				execErr = err
				break
			}
		}
		stmt.Close()
		if execErr != nil {
			tx.Rollback() //nolint:errcheck
			return
		}
		if err := tx.Commit(); err != nil {
			return
		}
	}

	// Re-vectorize chunks. In Ollama mode, only process chunks missing a vector
	// (embeddings are corpus-independent so existing vectors remain valid).
	// In TF-IDF mode, re-embed all chunks since IDF changed globally.
	vstmt, err := s.DB.Prepare(`UPDATE chunks SET vector=? WHERE id=?`)
	if err != nil {
		return
	}
	defer vstmt.Close()
	for _, c := range all {
		if s.provider != nil && c.hasVector {
			continue // Ollama: skip already-embedded chunks
		}
		v := s.vectorizeChunk(c.heading, c.content, idf, idfDefault)
		if v == nil {
			continue
		}
		vstmt.Exec(embed.ToBytes(v), c.id) //nolint:errcheck
	}

	s.mu.Lock()
	s.idf = idf
	s.vecs = nil
	s.mu.Unlock()
}

// LoadVecs ensures the in-memory vector cache is populated. Idempotent.
// The write lock is held for the duration to prevent duplicate DB loads under concurrent searches.
func (s *Store) LoadVecs() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vecs != nil {
		return nil
	}

	rows, err := s.DB.Query(`
		SELECT c.id, d.path, c.heading, c.content, c.vector
		FROM chunks c
		JOIN documents d ON d.id = c.doc_id
		WHERE c.vector IS NOT NULL
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var vecs []ChunkVec
	for rows.Next() {
		var cv ChunkVec
		var blob []byte
		if err := rows.Scan(&cv.ID, &cv.DocPath, &cv.Heading, &cv.Content, &blob); err != nil {
			continue
		}
		v := embed.FromBytes(blob)
		if v == nil {
			continue
		}
		cv.Vec = v
		vecs = append(vecs, cv)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.vecs = vecs
	return nil
}

func (s *Store) invalidateVecs() {
	s.mu.Lock()
	s.vecs = nil
	s.mu.Unlock()
}

// ListDocuments returns metadata for all documents whose path starts with filter.
func (s *Store) ListDocuments(filter string) ([]DocSummary, error) {
	rows, err := s.DB.Query(`
		SELECT d.path, d.title, c.heading
		FROM documents d
		JOIN chunks c ON c.doc_id = d.id
		WHERE d.path LIKE ?
		ORDER BY d.ingested_at DESC, d.path, c.chunk_idx
	`, filter+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocSummary
	idx := make(map[string]int)
	for rows.Next() {
		var path, title, heading string
		if err := rows.Scan(&path, &title, &heading); err != nil {
			continue
		}
		if i, ok := idx[path]; ok {
			docs[i].Headings = append(docs[i].Headings, heading)
		} else {
			idx[path] = len(docs)
			docs = append(docs, DocSummary{Path: path, Title: title, Headings: []string{heading}})
		}
	}
	return docs, rows.Err()
}

// DeleteDocument removes a document and all its chunks (including FTS entries).
func (s *Store) DeleteDocument(path string) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	var docID int64
	if err := tx.QueryRow(`SELECT id FROM documents WHERE path=?`, path).Scan(&docID); err != nil {
		return 0, fmt.Errorf("not found: %s", path)
	}

	rows, err := tx.Query(`SELECT id FROM chunks WHERE doc_id=?`, docID)
	if err != nil {
		return 0, err
	}
	var chunkIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			chunkIDs = append(chunkIDs, id)
		}
	}
	rows.Close()

	for _, cid := range chunkIDs {
		if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE rowid=?`, cid); err != nil {
			return 0, err
		}
	}

	res, err := tx.Exec(`DELETE FROM documents WHERE id=?`, docID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.invalidateVecs()
	s.lru.Purge()
	return int(n) * len(chunkIDs), nil
}

// WriteChunk saves a knowledge chunk under path/heading.
// Upsert semantics: if a chunk with the same non-empty heading already exists
// under this path it is updated in place; otherwise a new chunk is appended.
// After the write, IDF and vectors are rebuilt (rebuildVocabAndVectors handles embedding).
func (s *Store) WriteChunk(path, heading, content string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`INSERT INTO documents(path, title) VALUES(?,?) ON CONFLICT(path) DO NOTHING`,
		path, heading,
	); err != nil {
		return err
	}

	var docID int64
	if err := tx.QueryRow(`SELECT id FROM documents WHERE path=?`, path).Scan(&docID); err != nil {
		return err
	}

	committed := false

	// Upsert-by-heading when heading is non-empty.
	if heading != "" {
		var existingID int64
		err := tx.QueryRow(`SELECT id FROM chunks WHERE doc_id=? AND heading=?`, docID, heading).Scan(&existingID)
		if err == nil {
			// Clear the vector so rebuildVocabAndVectors re-embeds this chunk.
			if _, err := tx.Exec(`UPDATE chunks SET content=?, vector=NULL WHERE id=?`, content, existingID); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE rowid=?`, existingID); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO chunks_fts(rowid, heading, content) VALUES(?,?,?)`, existingID, heading, content); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			committed = true
		}
	}

	if !committed {
		var maxIdx sql.NullInt64
		_ = tx.QueryRow(`SELECT MAX(chunk_idx) FROM chunks WHERE doc_id=?`, docID).Scan(&maxIdx)
		chunkIdx := 0
		if maxIdx.Valid {
			chunkIdx = int(maxIdx.Int64) + 1
		}
		res, err := tx.Exec(
			`INSERT INTO chunks(doc_id, chunk_idx, heading, content, vector) VALUES(?,?,?,?,NULL)`,
			docID, chunkIdx, heading, content,
		)
		if err != nil {
			return err
		}
		chunkID, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO chunks_fts(rowid, heading, content) VALUES(?,?,?)`, chunkID, heading, content); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	// Rebuild IDF and re-vectorize. In Ollama mode only the new NULL-vector chunk is embedded;
	// in TF-IDF mode all chunks are re-embedded with the updated IDF.
	s.rebuildVocabAndVectors()
	s.lru.Purge()
	return nil
}

func idfLen(m map[string]float64) int {
	if m == nil {
		return 0
	}
	return len(m)
}

