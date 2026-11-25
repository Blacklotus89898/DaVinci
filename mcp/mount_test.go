package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blacklotus88888/knowledge-service/internal/store"
)

// newMountServer builds a server whose docs dir has one personal doc and a
// read-only "team" mount laid out like Martlet: repos/<repo>/_meta.yaml pins
// the code date; one doc verified before it moved, one after.
func newMountServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	srv, s := newTestServer(t)
	team := t.TempDir()
	write := func(root, rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(srv.docsPath, "runbooks/own.md", "# Own\n\n## Zebra restart\n\nrestart the zebra daemon\n")
	write(team, "repos/site/_meta.yaml", "repo: site\nverified_at: 2026-11-20\n")
	write(team, "repos/site/architecture.md", "---\ntitle: Site architecture\ncovers:\n  - src/**\nlast_verified: 2026-11-14\n---\n# Site\n\n## Netlify deploy\n\nthe site deploys on netlify via quokka build\n")
	write(team, "repos/site/overview.md", "---\ntitle: Site overview\ncovers: [README.md]\nlast_verified: 2026-11-21\n---\n# Overview\n\n## Stack\n\nnext.js with platypus cms\n")
	mounts := []store.Mount{{Name: "team", Root: team}}
	srv.SetMounts(mounts)
	if err := store.Ingest(s, srv.docsPath, mounts...); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return srv, s, team
}

func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	if e, ok := resp["error"].(map[string]any); ok {
		return e["message"].(string), false
	}
	return resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string), true
}

func TestMountSearchShowsFreshness(t *testing.T) {
	srv, _, _ := newMountServer(t)
	text, ok := toolText(t, exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"quokka netlify"}}}`))
	if !ok || !strings.Contains(text, "team/repos/site/architecture.md") {
		t.Fatalf("mount doc not found: %s", text)
	}
	if !strings.Contains(text, "possibly stale: code moved 2026-11-20, last verified 2026-11-14") {
		t.Errorf("stale label missing: %s", text)
	}
	text, _ = toolText(t, exchange(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"platypus"}}}`))
	if !strings.Contains(text, "verified 2026-11-21") || strings.Contains(text, "stale") {
		t.Errorf("fresh doc mislabelled: %s", text)
	}
}

func TestMountReadAndReadOnly(t *testing.T) {
	srv, _, team := newMountServer(t)
	text, ok := toolText(t, exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_knowledge","arguments":{"path":"team/repos/site/overview","heading":"Stack"}}}`))
	if !ok || !strings.Contains(text, "platypus") {
		t.Fatalf("read through mount failed: %s", text)
	}
	for _, call := range []string{
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"write_knowledge","arguments":{"path":"team/repos/site/gotchas.md","heading":"X","content":"y"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_knowledge","arguments":{"path":"team/repos/site/overview.md"}}}`,
	} {
		if text, ok := toolText(t, exchange(t, srv, call)); ok || !strings.Contains(text, "read-only mount") {
			t.Errorf("mount write/delete not refused: %s", text)
		}
	}
	if _, err := os.Stat(filepath.Join(team, "repos", "site", "overview.md")); err != nil {
		t.Errorf("mount file touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(team, "repos", "site", "gotchas.md")); err == nil {
		t.Error("write landed in the mount")
	}
	// Escaping the mount root is still refused.
	if text, ok := toolText(t, exchange(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"read_knowledge","arguments":{"path":"team/../../etc/passwd"}}}`)); ok {
		t.Errorf("traversal allowed: %s", text)
	}
}

func TestMountSurvivesReingestAndPersonalWrite(t *testing.T) {
	srv, s, _ := newMountServer(t)
	exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_knowledge","arguments":{"path":"solutions/new.md","heading":"New","content":"fresh fact"}}}`)
	docs, err := s.ListDocuments("team/")
	if err != nil || len(docs) != 2 {
		t.Fatalf("mount docs after write: %v %v", docs, err)
	}
	// Ingest without the mount prunes it (the CLI must pass mounts).
	if err := store.Ingest(s, srv.docsPath); err != nil {
		t.Fatal(err)
	}
	if docs, _ := s.ListDocuments("team/"); len(docs) != 0 {
		t.Errorf("unmounted docs not pruned: %v", docs)
	}
}

func TestSearchLogsGaps(t *testing.T) {
	srv, s, _ := newMountServer(t)
	for range 2 {
		exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"xylophone calibration"}}}`)
	}
	exchange(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"zebra restart"}}}`)
	gaps, err := s.Gaps(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) == 0 || gaps[0].Query != "xylophone calibration" || gaps[0].Count != 2 {
		t.Fatalf("gaps = %+v", gaps)
	}
	for _, g := range gaps {
		if g.Query == "zebra restart" {
			t.Errorf("answered query logged as gap: %+v", g)
		}
	}
}
