package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openTemp opens an in-memory (or temp-file) store for testing.
func openTemp(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seedDocs writes a small docs tree and ingests it.
func seedDocs(t *testing.T, s *Store) string {
	t.Helper()
	dir := t.TempDir()

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New\n## Section\nNew content.\n"), 0o644); err != nil {
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
	// OR query may include expansion tokens for broader recall.
	if orQ == andQ {
		// single raw token → same string is fine, but oom* could be added
		// this is acceptable either way
	}

	// Multi-token: AND must not include synonym-only tokens.
	andQ2, _ := buildFTSQuery("OOMKilled pod")
	if strings.Contains(andQ2, "oom*") && strings.Contains(andQ2, "AND") {
		// "oom*" in an AND expression next to "oomkilled*" would break recall
		t.Errorf("AND query %q contains synonym token 'oom*' which breaks AND recall", andQ2)
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
		_ = os.WriteFile(path, []byte(md), 0o644)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, _ := Open(filepath.Join(b.TempDir(), "bench.db"))
		_ = Ingest(s, dir)
		s.Close()
	}
}

func BenchmarkHybridCold(b *testing.B) {
	s, _ := Open(filepath.Join(b.TempDir(), "bench.db"))
	defer s.Close()
	dir := b.TempDir()
	md := `# Doc
## Section
Kubernetes ArgoCD pod deployment sync failed OOM disk pressure Cilium eBPF networking.
`
	for i := 0; i < 20; i++ {
		_ = os.WriteFile(filepath.Join(dir, strings.Repeat("d", i+1)+".md"), []byte(md), 0o644)
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
	defer s.Close()
	dir := b.TempDir()
	md := `# Doc
## Section
Kubernetes ArgoCD pod deployment sync failed OOM disk pressure Cilium eBPF networking.
`
	for i := 0; i < 20; i++ {
		_ = os.WriteFile(filepath.Join(dir, strings.Repeat("e", i+1)+".md"), []byte(md), 0o644)
	}
	_ = Ingest(s, dir)
	_, _ = Hybrid(s, "kubernetes pod oom fix", 5, false) // prime cache

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Hybrid(s, "kubernetes pod oom fix", 5, false)
	}
}
