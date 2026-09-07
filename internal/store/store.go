package store

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	Updated string
	Vec     []float32
}

// Result is a single search hit. Score is the fused RRF score; FTSRank and
// VecRank are the 1-based ranks in the keyword and vector result lists (0 when
// the hit came from only one channel), and Cosine is the raw vector similarity
// when the vector channel produced the hit.
type Result struct {
	ID      int64
	Path    string
	Heading string
	Content string
	Updated string
	Score   float64
	FTSRank int
	VecRank int
	Cosine  float64
}

// DocSummary is a brief overview of a document in the knowledge base.
type DocSummary struct {
	Path     string
	Title    string
	Tags     []string
	Headings []string
}

// Store wraps a SQLite connection plus cached in-memory retrieval state.
type Store struct {
	DB        *sql.DB
	lru       *cache.LRU[string, []Result]
	mu        sync.RWMutex
	rebuildMu sync.Mutex // serialises concurrent rebuildVocabAndVectors calls
	idf       map[string]float64
	vecs      []ChunkVec
	provider  embed.Provider // nil → TF-IDF hashing
	tfidfDims int            // dimension for TF-IDF vectors (ignored when provider != nil)
}

// currentSchemaVersion is incremented when the schema changes in a way that
// requires a migration. Open() rejects databases created by a newer binary.
//
// v2 added: documents.tags, chunks.updated_at, and the meta table (which
// records the embedding vector dimensions the corpus was built with).
const currentSchemaVersion = 2

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS documents (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    path        TEXT    UNIQUE NOT NULL,
    title       TEXT,
    tags        TEXT,
    ingested_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chunks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    doc_id     INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_idx  INTEGER NOT NULL,
    heading    TEXT,
    content    TEXT NOT NULL,
    vector     BLOB,
    updated_at DATETIME,
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

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// isDupColumnErr reports whether err is SQLite's "duplicate column name"
// error, which migrations treat as success (the column already exists).
func isDupColumnErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

// migrateToV2 upgrades a v1 database in place. Every statement is idempotent:
// fresh databases created directly at v2 already contain the new columns, and
// the resulting duplicate-column errors are ignored.
func migrateToV2(db *sql.DB) error {
	if _, err := db.Exec(`ALTER TABLE documents ADD COLUMN tags TEXT`); err != nil && !isDupColumnErr(err) {
		return fmt.Errorf("migrate documents.tags: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE chunks ADD COLUMN updated_at DATETIME`); err != nil && !isDupColumnErr(err) {
		return fmt.Errorf("migrate chunks.updated_at: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("migrate meta table: %w", err)
	}
	return nil
}

// setMeta upserts a key/value pair into the meta table.
func (s *Store) setMeta(key, value string) {
	_, _ = s.DB.Exec(
		`INSERT INTO meta(key, value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
}

// Open opens (or creates) the SQLite knowledge base at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	// Check schema version. Migrate older databases; reject databases from newer binaries.
	var ver int
	_ = db.QueryRow(`PRAGMA user_version`).Scan(&ver)
	switch {
	case ver > currentSchemaVersion:
		_ = db.Close()
		return nil, fmt.Errorf("database schema version %d is newer than this binary supports (%d) — upgrade knowledge-service", ver, currentSchemaVersion)
	case ver < currentSchemaVersion:
		if err := migrateToV2(db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migrate to schema v%d: %w", currentSchemaVersion, err)
		}
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, currentSchemaVersion)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set schema version: %w", err)
		}
	}
	// ver == currentSchemaVersion: already up to date. Future migrations extend the switch above.

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
	if p != nil {
		// Query the DB directly so the warning fires even before LoadVecs() is called.
		var blobLen sql.NullInt64
		_ = s.DB.QueryRow(`SELECT length(vector) FROM chunks WHERE vector IS NOT NULL LIMIT 1`).Scan(&blobLen)
		if blobLen.Valid {
			storedDims := int(blobLen.Int64) / 4
			if storedDims != p.Dims() {
				fmt.Fprintf(os.Stderr,
					"warning: existing corpus uses %d-dim vectors but new provider uses %d-dim — run `make ingest` to re-vectorize\n",
					storedDims, p.Dims())
			}
		}
	}
	s.provider = p
	if p != nil {
		s.setMeta("embed_dims", strconv.Itoa(p.Dims()))
	}
}

// HasProvider reports whether a neural embedding backend is active (as opposed
// to the TF-IDF fallback). Callers use it to calibrate similarity thresholds.
func (s *Store) HasProvider() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.provider != nil
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
// TF-IDF mode: re-embeds all TF-IDF-dimensioned chunks because IDF changes
// globally with every write. Vectors stored with a different provider's
// dimensions are left untouched — a TF-IDF fallback (e.g. Ollama down at
// startup) must degrade search, never overwrite the semantic index.
// Ollama mode: skips the vocab step and only embeds chunks whose stored
// vector is missing or was written with different dimensions, since Ollama
// embeddings are corpus-independent (no IDF recalculation needed).
func (s *Store) rebuildVocabAndVectors() {
	// Serialise concurrent calls so two simultaneous writes don't interleave
	// their vocab table updates and per-chunk vector UPDATEs.
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()

	type chunkRow struct {
		id      int64
		heading string
		content string
		vecLen  int // stored vector length in bytes; 0 = no vector
	}

	rows, err := s.DB.Query(`SELECT id, heading, content, length(vector) FROM chunks`)
	if err != nil {
		return
	}
	var all []chunkRow
	for rows.Next() {
		var c chunkRow
		var vecLen sql.NullInt64
		if err := rows.Scan(&c.id, &c.heading, &c.content, &vecLen); err == nil {
			if vecLen.Valid {
				c.vecLen = int(vecLen.Int64)
			}
			all = append(all, c)
		}
	}
	_ = rows.Close()

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
			_ = tx.Rollback()
			return
		}
		stmt, err := tx.Prepare(`INSERT INTO vocab(term, df, idf) VALUES(?,?,?)`)
		if err != nil {
			_ = tx.Rollback()
			return
		}
		var execErr error
		for term, d := range df {
			if _, err := stmt.Exec(term, d, idf[term]); err != nil {
				execErr = err
				break
			}
		}
		_ = stmt.Close()
		if execErr != nil {
			_ = tx.Rollback()
			return
		}
		if err := tx.Commit(); err != nil {
			return
		}
	}

	// Re-vectorize chunks. Dimension mismatches are skipped so a provider
	// fallback never overwrites another provider's vectors (see comment above).
	wantBytes := s.tfidfDims * 4
	if s.provider != nil {
		wantBytes = s.provider.Dims() * 4
	}
	vstmt, err := s.DB.Prepare(`UPDATE chunks SET vector=? WHERE id=?`)
	if err != nil {
		return
	}
	defer vstmt.Close()
	for _, c := range all {
		if s.provider != nil {
			if c.vecLen == wantBytes {
				continue // Ollama: fresh embedding already stored
			}
		} else if c.vecLen > 0 && c.vecLen != wantBytes {
			continue // TF-IDF: foreign-provider vector — never overwrite
		}
		v := s.vectorizeChunk(c.heading, c.content, idf, idfDefault)
		if v == nil {
			continue
		}
		_, _ = vstmt.Exec(embed.ToBytes(v), c.id)
	}

	s.setMeta("embed_dims", strconv.Itoa(wantBytes/4))

	s.mu.Lock()
	if s.provider == nil {
		// Only update s.idf in TF-IDF mode; Ollama embeddings are corpus-independent
		// and the IDF map loaded at startup must not be overwritten with nil.
		s.idf = idf
	}
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
		SELECT c.id, d.path, c.heading, c.content, c.vector, COALESCE(c.updated_at, '')
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
		if err := rows.Scan(&cv.ID, &cv.DocPath, &cv.Heading, &cv.Content, &blob, &cv.Updated); err != nil {
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
		SELECT d.path, d.title, c.heading, COALESCE(d.tags, '')
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
		var path, title, heading, tags string
		if err := rows.Scan(&path, &title, &heading, &tags); err != nil {
			continue
		}
		if i, ok := idx[path]; ok {
			docs[i].Headings = append(docs[i].Headings, heading)
		} else {
			idx[path] = len(docs)
			var docTags []string
			for _, tag := range strings.Split(tags, ",") {
				if tag = strings.TrimSpace(tag); tag != "" {
					docTags = append(docTags, tag)
				}
			}
			docs = append(docs, DocSummary{Path: path, Title: title, Tags: docTags, Headings: []string{heading}})
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
	defer func() { _ = tx.Rollback() }()

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
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		chunkIDs = append(chunkIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

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
	defer func() { _ = tx.Rollback() }()

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

	// Doc tags (if any) are appended to the FTS content so tag terms are searchable.
	var tags string
	_ = tx.QueryRow(`SELECT COALESCE(tags,'') FROM documents WHERE id=?`, docID).Scan(&tags)
	ftsContent := content
	if tags != "" {
		ftsContent = content + "\n" + tags
	}

	committed := false

	// Upsert-by-heading when heading is non-empty.
	if heading != "" {
		var existingID int64
		err := tx.QueryRow(`SELECT id FROM chunks WHERE doc_id=? AND heading=?`, docID, heading).Scan(&existingID)
		if err == nil {
			// Clear the vector so rebuildVocabAndVectors re-embeds this chunk.
			if _, err := tx.Exec(`UPDATE chunks SET content=?, vector=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=?`, content, existingID); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE rowid=?`, existingID); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO chunks_fts(rowid, heading, content) VALUES(?,?,?)`, existingID, heading, ftsContent); err != nil {
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
			`INSERT INTO chunks(doc_id, chunk_idx, heading, content, vector, updated_at) VALUES(?,?,?,?,NULL,CURRENT_TIMESTAMP)`,
			docID, chunkIdx, heading, content,
		)
		if err != nil {
			return err
		}
		chunkID, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO chunks_fts(rowid, heading, content) VALUES(?,?,?)`, chunkID, heading, ftsContent); err != nil {
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

