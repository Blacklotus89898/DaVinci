package store

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DocStatus is how far a document can be trusted, from Martlet-style
// frontmatter: a code-tied doc (non-empty `covers`) is stale when the code it
// explains moved (`verified_at` in the nearest _meta.yaml) after a human last
// verified it (`last_verified`), or when it was never verified at all.
type DocStatus struct {
	CodeTied     bool
	LastVerified string // YYYY-MM-DD, "" when never verified
	CodeMoved    string // YYYY-MM-DD the pinned code last moved, "" when unknown
}

// Stale reports whether the doc may no longer match the code it covers.
// ISO dates compare correctly as strings.
func (d DocStatus) Stale() bool {
	if !d.CodeTied || d.CodeMoved == "" {
		return false
	}
	return d.LastVerified == "" || d.CodeMoved > d.LastVerified
}

// Label is the short annotation search results carry; "" when there is
// nothing to say.
func (d DocStatus) Label() string {
	switch {
	case d.Stale() && d.LastVerified == "":
		return "⚠ never verified against its code"
	case d.Stale():
		return "⚠ possibly stale: code moved " + d.CodeMoved + ", last verified " + d.LastVerified
	case d.LastVerified != "":
		return "verified " + d.LastVerified
	}
	return ""
}

// DocStatusOf returns the freshness metadata stored for path.
func (s *Store) DocStatusOf(path string) DocStatus {
	var d DocStatus
	var tied int
	_ = s.DB.QueryRow(
		`SELECT code_tied, last_verified, code_moved FROM documents WHERE path=?`, path,
	).Scan(&tied, &d.LastVerified, &d.CodeMoved)
	d.CodeTied = tied != 0
	return d
}

// LogGap records a search that found nothing useful (outcome "none" or
// "weak"), so the missing knowledge can be written later. Best-effort.
func (s *Store) LogGap(query, pathPrefix, outcome string) {
	_, _ = s.DB.Exec(`INSERT INTO gaps(query, path_prefix, outcome) VALUES(?,?,?)`, query, pathPrefix, outcome)
}

// Gap is one aggregated unanswered query.
type Gap struct {
	Query, Outcome, Last string
	Count                int
}

// Gaps returns the most frequent unanswered queries, most asked first.
func (s *Store) Gaps(limit int) ([]Gap, error) {
	rows, err := s.DB.Query(`
		SELECT lower(query), MIN(outcome), COUNT(*), MAX(at) FROM gaps
		GROUP BY lower(query) ORDER BY COUNT(*) DESC, MAX(at) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []Gap
	for rows.Next() {
		var g Gap
		if err := rows.Scan(&g.Query, &g.Outcome, &g.Count, &g.Last); err == nil {
			out = append(out, g)
		}
	}
	return out, rows.Err()
}

// pinDate returns `verified_at` from the nearest _meta.yaml between dir and
// root (inclusive) — Martlet pins each product repo's verified commit there
// and moves the date whenever that repo's code changes.
func pinDate(root, dir string) string {
	root = filepath.Clean(root)
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if v := readVerifiedAt(filepath.Join(d, "_meta.yaml")); v != "" {
			return v
		}
		if d == root || filepath.Dir(d) == d || !strings.HasPrefix(d, root) {
			return ""
		}
	}
}

func readVerifiedAt(p string) string {
	f, err := os.Open(p) //nolint:gosec // fixed file name inside a configured corpus
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if ok && strings.TrimSpace(k) == "verified_at" {
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if v == "null" || v == "~" {
				return ""
			}
			return v
		}
	}
	return ""
}
