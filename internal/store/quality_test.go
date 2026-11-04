package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A re-ingest of unchanged content must keep each chunk's real last-modified
// date; only a chunk whose text changed gets a new one.
func TestReIngestKeepsUpdatedAtOfUnchangedChunks(t *testing.T) {
	s := openTemp(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "runbook.md")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	updatedAt := func(heading string) string {
		t.Helper()
		var u string
		if err := s.DB.QueryRow(`SELECT updated_at FROM chunks WHERE heading=?`, heading).Scan(&u); err != nil {
			t.Fatalf("updated_at of %q: %v", heading, err)
		}
		return u
	}

	write("## Stable\nnever changes\n\n## Edited\nfirst version\n")
	if err := Ingest(s, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE chunks SET updated_at='2020-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}

	write("## Stable\nnever changes\n\n## Edited\nsecond version\n")
	if err := Ingest(s, dir); err != nil {
		t.Fatal(err)
	}
	if got := updatedAt("Stable"); !strings.HasPrefix(got, "2020-01-01") {
		t.Errorf("unchanged chunk's updated_at moved to %q", got)
	}
	if got := updatedAt("Edited"); strings.HasPrefix(got, "2020-01-01") {
		t.Error("edited chunk kept its old updated_at")
	}
}

// The "# title" line alone is not a chunk: it has nothing to read and would
// spend a search result slot. It still names the document.
func TestHeadingOnlySectionsAreNotIndexed(t *testing.T) {
	s := openTemp(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "power.md"), []byte("# power outage\n\n## Recover\nstart the VMs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.md"), []byte("# nothing here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ingest(s, dir); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM chunks WHERE content=''`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d empty chunks indexed", n)
	}
	docs, err := s.ListDocuments("")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "power.md" || docs[0].Title != "power outage" {
		t.Errorf("want only power.md titled %q, got %+v", "power outage", docs)
	}
}

// Two processes (two Claude Code sessions) share one DB. A write in one must
// be visible to the other's next search, even for a query it already cached.
func TestWriteInOneProcessInvalidatesTheOther(t *testing.T) {
	db := filepath.Join(t.TempDir(), "shared.db")
	a, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })

	if got, _ := Search(b, SearchOpts{Query: "zebrafish"}); len(got) != 0 {
		t.Fatalf("empty KB returned %v", got)
	}
	if err := a.WriteChunk("solutions/fish.md", "Zebrafish tank", "zebrafish need warm water"); err != nil {
		t.Fatal(err)
	}
	got, err := Search(b, SearchOpts{Query: "zebrafish"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("second process served its stale cached result")
	}
	if len(got[0].Content) == 0 || got[0].VecRank == 0 {
		t.Errorf("want the hit from both channels (vectors reloaded), got %+v", got[0])
	}
}

// FTSStrict separates "every term matched" from the OR fallback, which ranks
// a chunk sharing a single common word just as high.
func TestFTSStrictMarksAndMatchesOnly(t *testing.T) {
	s := openTemp(t)
	if err := s.WriteChunk("runbooks/power.md", "Power outage", "recover after the electricity goes out"); err != nil {
		t.Fatal(err)
	}

	got, err := Search(s, SearchOpts{Query: "electricity out"})
	if err != nil || len(got) == 0 {
		t.Fatalf("AND query: %v %v", got, err)
	}
	if !got[0].FTSStrict {
		t.Error("all-terms match not marked strict")
	}

	got, err = Search(s, SearchOpts{Query: "electricity unicorn"})
	if err != nil || len(got) == 0 {
		t.Fatalf("OR fallback: %v %v", got, err)
	}
	if got[0].FTSStrict {
		t.Error("OR-fallback match marked strict")
	}
	if got[0].FTSCoverage != 0.5 {
		t.Errorf("OR-fallback coverage = %v, want 0.5 (electricity yes, unicorn no)", got[0].FTSCoverage)
	}
}
