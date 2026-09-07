package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blacklotus88888/knowledge-service/internal/embed"
)

// fakeProvider is a deterministic in-process embed.Provider for tests.
type fakeProvider struct{ dims int }

func (f *fakeProvider) Dims() int { return f.dims }

func (f *fakeProvider) Embed(text string) ([]float32, error) {
	v := make([]float32, f.dims)
	// Spread a simple hash of the text across the vector so different texts
	// get different (but stable) directions.
	h := uint32(2166136261)
	for i := 0; i < len(text); i++ {
		h = (h ^ uint32(text[i])) * 16777619
		v[i%f.dims] += float32(h%97) / 97.0
	}
	return embed.Normalize(v), nil
}

// vecBlobLen returns the byte length of a chunk's stored vector (0 when NULL).
func vecBlobLen(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COALESCE(length(vector), 0) FROM chunks LIMIT 1`).Scan(&n); err != nil {
		t.Fatalf("vecBlobLen: %v", err)
	}
	return n
}

// openTemp opens an in-memory (or temp-file) store for testing.
func openTemp(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedDocs writes a small docs tree and ingests it.
func seedDocs(t *testing.T, s *Store) string {
	t.Helper()
	dir := t.TempDir()

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writeFile %s: %v", name, err)
		}
	}

	writeFile("argocd.md", `# ArgoCD
Overview of ArgoCD deployment.

## OOM Fix
ArgoCD was killed by OOM when too many completed pods accumulated.
Fix: deploy pod-cleanup CronJob to prune pods every 15 minutes.

## Sync Retry
Increase the retry backoff in the ArgoCD configmap to avoid cascade failures.
`)

	writeFile("cilium.md", `# Cilium CNI
Cilium provides eBPF-based networking for Kubernetes.

## Installation
Install Cilium via Helm chart with kubeProxyReplacement enabled.
`)

	if err := Ingest(s, dir); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return dir
}

// --- Correctness tests ---

func TestIngestAndFTSSearch(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)

	results, err := Hybrid(s, "oom fix pod cleanup", 5, false)
	if err != nil {
		t.Fatalf("Hybrid: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	// Top result should be about OOM.
	top := results[0]
	if !strings.Contains(strings.ToLower(top.Heading+top.Content), "oom") {
		t.Errorf("top result doesn't mention OOM: heading=%q", top.Heading)
	}
}

func TestVectorSearchReturnsRelevant(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)

	results, err := Hybrid(s, "ebpf networking kubernetes", 5, false)
	if err != nil {
		t.Fatalf("Hybrid: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	found := false
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.Content+r.Heading), "ebpf") ||
			strings.Contains(strings.ToLower(r.Content), "cilium") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a Cilium-related result; got: %+v", results)
	}
}

func TestSearchEmptyDB(t *testing.T) {
	s := openTemp(t)
	results, err := Hybrid(s, "anything", 5, false)
	if err != nil {
		t.Fatalf("Hybrid on empty DB: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty DB, got %d", len(results))
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)
	results, err := Hybrid(s, "", 5, false)
	if err != nil {
		t.Fatalf("Hybrid with empty query: %v", err)
	}
	// An empty query (all stop words or empty string) should return 0 results gracefully.
	_ = results
}

func TestLRUCacheHit(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)

	// Prime the cache.
	r1, _ := Hybrid(s, "argocd sync retry", 3, false)
	// Second call should be a cache hit (same results).
	r2, _ := Hybrid(s, "argocd sync retry", 3, false)

	if len(r1) != len(r2) {
		t.Errorf("cache hit returned different result count: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].ID != r2[i].ID {
			t.Errorf("result[%d] ID differs: %d vs %d", i, r1[i].ID, r2[i].ID)
		}
	}
}

func TestCacheIsolation(t *testing.T) {
	// Two separate Stores must have independent caches.
	s1 := openTemp(t)
	s2 := openTemp(t)
	seedDocs(t, s1)
	// s2 has no data; its cache should be empty regardless of s1.
	results, _ := Hybrid(s2, "argocd", 5, false)
	if len(results) != 0 {
		t.Errorf("s2 should have no results, got %d", len(results))
	}
}

func TestWriteChunk(t *testing.T) {
	s := openTemp(t)
	err := s.WriteChunk("solutions/test.md", "Fix for Foo", "Run kubectl rollout restart to fix foo.")
	if err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	results, err := Hybrid(s, "fix foo restart", 5, false)
	if err != nil {
		t.Fatalf("Hybrid after write: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected to find the written chunk")
	}
}

func TestReIngestPreservesOtherDocs(t *testing.T) {
	s := openTemp(t)
	dir := seedDocs(t, s)

	// Re-ingest only the argocd doc (simulate partial re-ingest by adding a new file).
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New\n## Section\nNew content.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ingest(s, dir); err != nil {
		t.Fatalf("re-ingest: %v", err)
	}

	// Cilium doc should still be findable via FTS after re-ingest.
	results, err := Hybrid(s, "cilium ebpf", 5, false)
	if err != nil {
		t.Fatalf("Hybrid: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.Content+r.Heading), "cilium") {
			found = true
		}
	}
	if !found {
		t.Error("cilium doc lost from FTS after re-ingest of partial set")
	}
}

func TestResultLimit(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)
	results, err := Hybrid(s, "kubernetes", 1, false)
	if err != nil {
		t.Fatalf("Hybrid: %v", err)
	}
	if len(results) > 1 {
		t.Errorf("expected ≤1 result with limit=1, got %d", len(results))
	}
}

func TestListDocuments(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)

	docs, err := s.ListDocuments("")
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("expected ≥2 docs, got %d", len(docs))
	}
	for _, d := range docs {
		if d.Path == "" {
			t.Error("doc has empty path")
		}
		if len(d.Headings) == 0 {
			t.Errorf("doc %s has no headings", d.Path)
		}
	}
}

func TestListDocumentsFilter(t *testing.T) {
	s := openTemp(t)
	_ = s.WriteChunk("solutions/foo.md", "Foo Fix", "Content.")
	_ = s.WriteChunk("runbooks/bar.md", "Bar Guide", "Content.")

	docs, err := s.ListDocuments("solutions/")
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	for _, d := range docs {
		if !strings.HasPrefix(d.Path, "solutions/") {
			t.Errorf("filter didn't work: got path %q", d.Path)
		}
	}
}

func TestDeleteDocument(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)

	// Confirm searchable before delete.
	r1, _ := Hybrid(s, "argocd oom", 5, false)
	if len(r1) == 0 {
		t.Fatal("expected results before delete")
	}

	// Find the path of the argocd doc.
	docs, _ := s.ListDocuments("")
	var argoPath string
	for _, d := range docs {
		if strings.Contains(d.Path, "argocd") {
			argoPath = d.Path
			break
		}
	}
	if argoPath == "" {
		t.Fatal("could not find argocd doc path")
	}

	n, err := s.DeleteDocument(argoPath)
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if n == 0 {
		t.Error("expected chunks > 0 deleted")
	}

	// Should no longer appear in search.
	r2, _ := Hybrid(s, "argocd oom", 5, false)
	for _, r := range r2 {
		if r.Path == argoPath {
			t.Errorf("deleted doc still in results: %+v", r)
		}
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := openTemp(t)
	_, err := s.DeleteDocument("does-not-exist.md")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestFTSPrefixMatch(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)

	// "kube" should match "Kubernetes" via prefix query.
	results, err := Hybrid(s, "kube cilium", 5, false)
	if err != nil {
		t.Fatalf("Hybrid: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.Content+r.Heading), "cilium") ||
			strings.Contains(strings.ToLower(r.Content+r.Heading), "kubernetes") {
			found = true
		}
	}
	if !found {
		t.Errorf("prefix query 'kube' didn't match 'Kubernetes'; results: %+v", results)
	}
}

func TestBuildFTSQuerySynonymSafety(t *testing.T) {
	// AND query must use raw tokens only — no SRE synonym expansion.
	// "OOMKilled" should produce AND query with "oomkilled*" only, not "oom*".
	// If "oom*" were injected into the AND query it would fail recall for docs
	// that only write "OOMKilled" (FTS5 treats it as one token, not two).
	andQ, orQ := buildFTSQuery("OOMKilled")
	if andQ != "oomkilled*" {
		t.Errorf("AND query = %q; want 'oomkilled*' (no synonym injection)", andQ)
	}
	// OR query may include expansion tokens for broader recall — we only assert the AND is clean.
	_ = orQ

	// Multi-token: AND must not include synonym-only tokens.
	andQ2, _ := buildFTSQuery("OOMKilled pod")
	if strings.Contains(andQ2, "oom*") && strings.Contains(andQ2, "AND") {
		// "oom*" in an AND expression next to "oomkilled*" would break recall
		t.Errorf("AND query %q contains synonym token 'oom*' which breaks AND recall", andQ2)
	}
}

// --- New behaviour: incremental ingest, vector preservation, prefix, metadata ---

func TestIngestOneIndexesSingleFile(t *testing.T) {
	s := openTemp(t)
	dir := seedDocs(t, s)

	// Add a new file and index only it.
	newDoc := "# Postgres\n\n## Connection Pool Exhaustion\nToo many idle connections.\n"
	if err := os.WriteFile(filepath.Join(dir, "postgres-pools.md"), []byte(newDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := IngestOne(s, dir, "postgres-pools.md"); err != nil {
		t.Fatalf("IngestOne: %v", err)
	}

	results, err := Search(s, SearchOpts{Query: "idle connections postgres", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Path, "postgres-pools") {
			found = true
		}
	}
	if !found {
		t.Errorf("IngestOne did not make the new doc searchable: %+v", results)
	}

	// Other docs must remain searchable (IngestOne must not prune them).
	results, _ = Search(s, SearchOpts{Query: "cilium ebpf", Limit: 5})
	if len(results) == 0 {
		t.Error("existing docs lost after IngestOne — it must be additive")
	}
}

func TestVectorsSurviveProviderFallback(t *testing.T) {
	s := openTemp(t)
	dir := seedDocs(t, s)

	// Embed with a "neural" provider (8-dim, like Ollama would be).
	s.SetProvider(&fakeProvider{dims: 8})
	if err := Ingest(s, dir); err != nil {
		t.Fatalf("ingest with provider: %v", err)
	}
	if got := vecBlobLen(t, s); got != 8*4 {
		t.Fatalf("expected 8-dim vectors (32 bytes), got %d bytes", got)
	}

	// Provider disappears (e.g. Ollama down at startup) → TF-IDF fallback.
	// A full re-ingest must NOT overwrite the provider vectors.
	s.SetProvider(nil)
	if err := Ingest(s, dir); err != nil {
		t.Fatalf("fallback ingest: %v", err)
	}
	if got := vecBlobLen(t, s); got != 8*4 {
		t.Fatalf("fallback wiped provider vectors: got %d bytes, want 32", got)
	}

	// Provider returns: everything back to normal.
	s.SetProvider(&fakeProvider{dims: 8})
	results, err := Search(s, SearchOpts{Query: "cilium networking", Limit: 5})
	if err != nil {
		t.Fatalf("search after provider return: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results with provider restored")
	}
}

func TestUnchangedChunksKeepVectorsAcrossIngest(t *testing.T) {
	s := openTemp(t)
	dir := seedDocs(t, s)

	s.SetProvider(&fakeProvider{dims: 8})
	if err := Ingest(s, dir); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	// Re-ingest unchanged content: vectors must survive (CASE-preserved upsert,
	// and rebuild skips chunks whose vector already matches provider dims).
	if err := Ingest(s, dir); err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	var nulls int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM chunks WHERE vector IS NULL`).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 0 {
		t.Errorf("unchanged re-ingest left %d chunks unembedded", nulls)
	}
}

func TestSearchPathPrefix(t *testing.T) {
	s := openTemp(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "tools"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, "tools", "restart-deploy.md"), []byte("# T\n## Restart Deployment\nkubectl rollout restart deployment api.\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "runbook-restart.md"), []byte("# R\n## Restart Runbook\nrestart the deployment by hand\n"), 0o600)
	if err := Ingest(s, dir); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	results, err := Search(s, SearchOpts{Query: "restart deployment", Limit: 10, PathPrefix: "tools/"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected tools/ results")
	}
	for _, r := range results {
		if !strings.HasPrefix(r.Path, "tools/") {
			t.Errorf("prefix filter leaked non-tools result: %s", r.Path)
		}
	}

	// Vector channel must respect the prefix too.
	vecOnly, err := Search(s, SearchOpts{Query: "rollout restart api deployment", Limit: 10, PathPrefix: "tools/"})
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	for _, r := range vecOnly {
		if !strings.HasPrefix(r.Path, "tools/") {
			t.Errorf("vector channel leaked non-tools result: %s", r.Path)
		}
	}
}

func TestTagsIndexedAndListed(t *testing.T) {
	s := openTemp(t)
	dir := t.TempDir()
	md := "---\ntitle: ArgoCD Runbook\ntags: argocd, oom, k8s\n---\n# ArgoCD Runbook\n\n## OOM Fix\nDeploy the pod-cleanup CronJob.\n"
	if err := os.WriteFile(filepath.Join(dir, "argocd-oom.md"), []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ingest(s, dir); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// Tags are searchable (appended to FTS content).
	results, err := Search(s, SearchOpts{Query: "argocd", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search hit")
	}

	// Tags are listed.
	docs, err := s.ListDocuments("")
	if err != nil || len(docs) != 1 {
		t.Fatalf("ListDocuments: %v (%v)", docs, err)
	}
	if strings.Join(docs[0].Tags, ",") != "argocd,oom,k8s" {
		t.Errorf("tags = %v, want [argocd oom k8s]", docs[0].Tags)
	}
}

func TestUpdatedAtRecorded(t *testing.T) {
	s := openTemp(t)
	seedDocs(t, s)

	var updated string
	if err := s.DB.QueryRow(`SELECT COALESCE(updated_at, '') FROM chunks LIMIT 1`).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if len(updated) < 10 {
		t.Errorf("expected updated_at timestamp, got %q", updated)
	}

	results, _ := Search(s, SearchOpts{Query: "cilium", Limit: 5})
	for _, r := range results {
		if r.Updated == "" {
			t.Errorf("search result missing Updated: %+v", r)
		}
	}
}

func TestSchemaMigrationFromV1(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v1.db")

	// Create a v1-shaped database by hand (documents/chunks without tags/updated_at/meta).
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE documents (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT UNIQUE NOT NULL, title TEXT, ingested_at DATETIME DEFAULT CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	// Opening with the current binary migrates it to v2.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open v1 db: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var ver int
	_ = s.DB.QueryRow(`PRAGMA user_version`).Scan(&ver)
	if ver != 2 {
		t.Fatalf("user_version = %d, want 2 after migration", ver)
	}
	// The new columns accept writes.
	if _, err := s.DB.Exec(`INSERT INTO documents(path, title, tags) VALUES('x.md', 'X', 'a,b')`); err != nil {
		t.Fatalf("insert into migrated documents: %v", err)
	}
}

// --- Benchmarks ---

func BenchmarkIngest(b *testing.B) {
	dir := b.TempDir()
	md := `# Bench Doc
## Section Alpha
Alpha content about Kubernetes deployments and pod restarts.

## Section Beta
Beta content about ArgoCD sync and retry logic and backoff.

## Section Gamma
Gamma content about Cilium CNI eBPF networking and routing.
`
	for i := 0; i < 20; i++ {
		path := filepath.Join(dir, strings.Repeat("a", i+1)+".md")
		_ = os.WriteFile(path, []byte(md), 0o600)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, _ := Open(filepath.Join(b.TempDir(), "bench.db"))
		_ = Ingest(s, dir)
		_ = s.Close()
	}
}

func BenchmarkHybridCold(b *testing.B) {
	s, _ := Open(filepath.Join(b.TempDir(), "bench.db"))
	defer s.Close() //nolint:errcheck
	dir := b.TempDir()
	md := `# Doc
## Section
Kubernetes ArgoCD pod deployment sync failed OOM disk pressure Cilium eBPF networking.
`
	for i := 0; i < 20; i++ {
		_ = os.WriteFile(filepath.Join(dir, strings.Repeat("d", i+1)+".md"), []byte(md), 0o600)
	}
	_ = Ingest(s, dir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.lru.Purge()
		_, _ = Hybrid(s, "kubernetes pod oom fix", 5, false)
	}
}

func BenchmarkHybridWarm(b *testing.B) {
	s, _ := Open(filepath.Join(b.TempDir(), "bench.db"))
	defer s.Close() //nolint:errcheck
	dir := b.TempDir()
	md := `# Doc
## Section
Kubernetes ArgoCD pod deployment sync failed OOM disk pressure Cilium eBPF networking.
`
	for i := 0; i < 20; i++ {
		_ = os.WriteFile(filepath.Join(dir, strings.Repeat("e", i+1)+".md"), []byte(md), 0o600)
	}
	_ = Ingest(s, dir)
	_, _ = Hybrid(s, "kubernetes pod oom fix", 5, false) // prime cache

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Hybrid(s, "kubernetes pod oom fix", 5, false)
	}
}
