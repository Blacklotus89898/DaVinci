package store

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/blacklotus88888/knowledge-service/internal/chunker"
)

type pendingDoc struct {
	relPath string
	title   string
	tags    string // comma-separated frontmatter tags ("" when none)
	chunks  []chunker.Chunk
}

// Ingest walks docsPath for .md files and re-indexes them into s.
// Documents not in the current walk are pruned. After all docs are committed,
// IDF and vectors are rebuilt over the full corpus.
//
// Ingest never embeds chunks itself: changed chunks get a NULL vector and
// rebuildVocabAndVectors embeds exactly those (in provider mode). This keeps
// startup and re-ingest cheap when content is unchanged.
func Ingest(s *Store, docsPath string) error {
	docs, err := loadDocs(docsPath)
	if err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for i := range docs {
		if err := ingestDoc(tx, &docs[i]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Prune documents whose source .md files no longer exist in docsPath.
	// Ingest only upserts what it finds; without this, deleted files persist forever.
	walked := make(map[string]bool, len(docs))
	for _, d := range docs {
		walked[d.relPath] = true
	}
	dbRows, err := s.DB.Query(`SELECT path FROM documents`)
	if err == nil {
		var stale []string
		for dbRows.Next() {
			var p string
			if dbRows.Scan(&p) == nil && !walked[p] {
				stale = append(stale, p)
			}
		}
		_ = dbRows.Close()
		for _, p := range stale {
			_, _ = s.DeleteDocument(p)
		}
	}

	// Recompute IDF from the full corpus (all docs, not just the batch) and
	// embed only chunks whose vector is missing or foreign-dimensioned.
	s.rebuildVocabAndVectors()
	s.lru.Purge()
	return nil
}

// IngestOne re-indexes a single markdown file (relPath, slash-separated and
// relative to docsPath) without touching any other document. This is what
// write_knowledge uses — O(one file) instead of O(corpus).
func IngestOne(s *Store, docsPath, relPath string) error {
	p := filepath.Join(docsPath, filepath.FromSlash(relPath))
	data, err := os.ReadFile(p) //nolint:gosec // p is derived from docsPath + a validated relative path
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	doc, err := buildDoc(docsPath, p, string(data))
	if err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if err := ingestDoc(tx, &doc); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	s.rebuildVocabAndVectors()
	s.lru.Purge()
	return nil
}

// ingestDoc upserts one document and its chunks inside tx. Existing vectors
// are preserved when the chunk content is unchanged, so unchanged chunks are
// never re-embedded.
func ingestDoc(tx *sql.Tx, doc *pendingDoc) error {
	if _, err := tx.Exec(
		`INSERT INTO documents(path, title, tags) VALUES(?,?,?)
		 ON CONFLICT(path) DO UPDATE SET title=excluded.title, tags=excluded.tags, ingested_at=CURRENT_TIMESTAMP`,
		doc.relPath, doc.title, doc.tags,
	); err != nil {
		return fmt.Errorf("insert document %s: %w", doc.relPath, err)
	}

	var docID int64
	if err := tx.QueryRow(`SELECT id FROM documents WHERE path=?`, doc.relPath).Scan(&docID); err != nil {
		return fmt.Errorf("fetch doc id for %s: %w", doc.relPath, err)
	}

	// Delete FTS entries for this doc's existing chunks before replacing them.
	idRows, err := tx.Query(`SELECT id FROM chunks WHERE doc_id=?`, docID)
	if err != nil {
		return err
	}
	var existingIDs []int64
	for idRows.Next() {
		var id int64
		if err := idRows.Scan(&id); err == nil {
			existingIDs = append(existingIDs, id)
		}
	}
	_ = idRows.Close()
	for _, id := range existingIDs {
		if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE rowid=?`, id); err != nil {
			return err
		}
	}

	// Remove stale chunks (chunk_idx >= new count).
	if _, err := tx.Exec(`DELETE FROM chunks WHERE doc_id=? AND chunk_idx>=?`, docID, len(doc.chunks)); err != nil {
		return err
	}

	for i, ch := range doc.chunks {
		// vector=NULL on insert; on conflict the vector is dropped only when the
		// content changed, so unchanged chunks keep their embedding.
		if _, err := tx.Exec(
			`INSERT INTO chunks(doc_id, chunk_idx, heading, content, vector, updated_at)
			 VALUES(?,?,?,?,NULL,CURRENT_TIMESTAMP)
			 ON CONFLICT(doc_id, chunk_idx) DO UPDATE SET
			   heading=excluded.heading,
			   content=excluded.content,
			   updated_at=CURRENT_TIMESTAMP,
			   vector=CASE WHEN chunks.content != excluded.content THEN NULL ELSE chunks.vector END`,
			docID, i, ch.Heading, ch.Content,
		); err != nil {
			return fmt.Errorf("insert chunk %d of %s: %w", i, doc.relPath, err)
		}
	}

	// Re-insert FTS for this doc's current chunks only. Doc tags are appended
	// to the indexed content so tag terms are searchable.
	if _, err := tx.Exec(
		`INSERT INTO chunks_fts(rowid, heading, content)
		 SELECT id, heading, CASE WHEN ?='' THEN content ELSE content || ' ' || ? END
		 FROM chunks WHERE doc_id=?`,
		doc.tags, doc.tags, docID,
	); err != nil {
		return err
	}
	return nil
}

// buildDoc reads one markdown file into a pendingDoc.
func buildDoc(root, p, data string) (pendingDoc, error) {
	tags := chunker.Tags(data)
	chunks := chunker.Split(data)
	title := ""
	if len(chunks) > 0 {
		title = chunks[0].Heading
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(p), ".md")
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return pendingDoc{}, fmt.Errorf("rel path for %s: %w", p, err)
	}
	return pendingDoc{
		relPath: filepath.ToSlash(rel),
		title:   title,
		tags:    strings.Join(tags, ", "),
		chunks:  chunks,
	}, nil
}

func loadDocs(root string) ([]pendingDoc, error) {
	var docs []pendingDoc
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // p comes from filepath.WalkDir within the trusted DOCS_PATH directory
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		if len(chunker.Split(string(data))) == 0 {
			return nil
		}
		doc, err := buildDoc(root, p, string(data))
		if err != nil {
			return err
		}
		docs = append(docs, doc)
		return nil
	})
	return docs, err
}
