package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/blacklotus88888/knowledge-service/internal/chunker"
	"github.com/blacklotus88888/knowledge-service/internal/embed"
)

type pendingDoc struct {
	relPath string
	title   string
	chunks  []chunker.Chunk
}

// Ingest walks docsPath for .md files and re-indexes them into s.
// Documents not in the current walk are left untouched (incremental FTS update).
// After all docs are committed, IDF and vectors are rebuilt over the full corpus.
func Ingest(s *Store, docsPath string) error {
	docs, err := loadDocs(docsPath)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return nil
	}

	// Use current in-memory IDF as a placeholder; rebuildVocabAndVectors corrects it.
	s.mu.RLock()
	idf := s.idf
	s.mu.RUnlock()
	idfDefault := embed.SmoothedIDF(idfLen(idf)+10, 0)

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	docStmt, err := tx.Prepare(
		`INSERT INTO documents(path, title) VALUES(?,?)
		 ON CONFLICT(path) DO UPDATE SET title=excluded.title, ingested_at=CURRENT_TIMESTAMP`,
	)
	if err != nil {
		return err
	}
	defer docStmt.Close()

	chunkStmt, err := tx.Prepare(
		`INSERT INTO chunks(doc_id, chunk_idx, heading, content, vector) VALUES(?,?,?,?,?)
		 ON CONFLICT(doc_id, chunk_idx) DO UPDATE SET
		   heading=excluded.heading, content=excluded.content, vector=excluded.vector`,
	)
	if err != nil {
		return err
	}
	defer chunkStmt.Close()

	for _, doc := range docs {
		if _, err := docStmt.Exec(doc.relPath, doc.title); err != nil {
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
		idRows.Close()
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
			v := s.vectorizeChunk(ch.Heading, ch.Content, idf, idfDefault)
			blob := embed.ToBytes(v)
			if _, err := chunkStmt.Exec(docID, i, ch.Heading, ch.Content, blob); err != nil {
				return fmt.Errorf("insert chunk %d of %s: %w", i, doc.relPath, err)
			}
		}

		// Re-insert FTS for this doc's current chunks only.
		if _, err := tx.Exec(
			`INSERT INTO chunks_fts(rowid, heading, content)
			 SELECT id, heading, content FROM chunks WHERE doc_id=?`, docID,
		); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Recompute IDF from the full corpus (all docs, not just the batch) and
	// re-vectorize all chunks so stored vectors match the updated IDF.
	s.rebuildVocabAndVectors()
	return nil
}

func loadDocs(root string) ([]pendingDoc, error) {
	var docs []pendingDoc
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		chunks := chunker.Split(string(data))
		if len(chunks) == 0 {
			return nil
		}
		title := chunks[0].Heading
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(p), ".md")
		}
		rel, _ := filepath.Rel(root, p)
		docs = append(docs, pendingDoc{relPath: filepath.ToSlash(rel), title: title, chunks: chunks})
		return nil
	})
	return docs, err
}
