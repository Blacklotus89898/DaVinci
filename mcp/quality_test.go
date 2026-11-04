package mcp

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/blacklotus88888/knowledge-service/internal/gitsync"
	"github.com/blacklotus88888/knowledge-service/internal/store"
)

// call invokes one tool and returns its text, or the error message when the
// server replied with an error.
func call(t *testing.T, srv *Server, tool string, args map[string]any) (text string, isErr bool) {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`)
	if e, ok := resp["error"].(map[string]any); ok {
		return e["message"].(string), true
	}
	return resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string), false
}

func TestReadKnowledge(t *testing.T) {
	srv, _ := newTestServer(t)
	if _, isErr := call(t, srv, "write_knowledge", map[string]any{
		"path": "runbooks/power-outage", "heading": "Recover",
		"content": "start the VMs\n\n### Verify\nkubectl get nodes",
	}); isErr {
		t.Fatal("write failed")
	}
	if _, isErr := call(t, srv, "write_knowledge", map[string]any{
		"path": "runbooks/power-outage.md", "heading": "Prepare", "content": "check the BIOS",
	}); isErr {
		t.Fatal("write failed")
	}

	whole, isErr := call(t, srv, "read_knowledge", map[string]any{"path": "runbooks/power-outage.md"})
	if isErr || !strings.Contains(whole, "start the VMs") || !strings.Contains(whole, "check the BIOS") {
		t.Errorf("whole-document read: %q", whole)
	}

	// A ## heading returns its ### subsections; case doesn't matter.
	sec, isErr := call(t, srv, "read_knowledge", map[string]any{"path": "runbooks/power-outage", "heading": "recover"})
	if isErr || !strings.Contains(sec, "start the VMs") || !strings.Contains(sec, "kubectl get nodes") {
		t.Errorf("section read missed the section or its subsection: %q", sec)
	}
	if strings.Contains(sec, "check the BIOS") {
		t.Errorf("section read leaked a sibling section: %q", sec)
	}

	// A heading exactly as a search result shows it ("Parent / Child").
	if sub, isErr := call(t, srv, "read_knowledge", map[string]any{"path": "runbooks/power-outage.md", "heading": "Recover / Verify"}); isErr || !strings.Contains(sub, "kubectl get nodes") {
		t.Errorf("subsection read: %q", sub)
	}

	if msg, isErr := call(t, srv, "read_knowledge", map[string]any{"path": "runbooks/power-outage.md", "heading": "Nope"}); !isErr || !strings.Contains(msg, "Prepare") {
		t.Errorf("unknown heading should error and list headings, got %q", msg)
	}
	if _, isErr := call(t, srv, "read_knowledge", map[string]any{"path": "runbooks/missing.md"}); !isErr {
		t.Error("missing document should error")
	}
	if _, isErr := call(t, srv, "read_knowledge", map[string]any{"path": "../../etc/passwd"}); !isErr {
		t.Error("path traversal should be rejected")
	}
}

func TestScoreLabelORFallbackIsNotStrong(t *testing.T) {
	or := store.Result{FTSRank: 1}
	if got := scoreLabel(or, false); got != "relevant" {
		t.Errorf("OR-fallback rank 1 labelled %q, want relevant", got)
	}
	and := store.Result{FTSRank: 1, FTSStrict: true}
	if got := scoreLabel(and, false); got != "strong match" {
		t.Errorf("all-terms rank 1 labelled %q, want strong match", got)
	}
}

func TestPreviewTruncationKeepsValidUTF8(t *testing.T) {
	content := strings.Repeat("—→", maxPreviewChars) // multi-byte, no spaces
	out := formatResults([]store.Result{{Path: "a.md", Heading: "H", Content: content}}, "q", false)
	if !utf8.ValidString(out) {
		t.Fatal("truncated preview split a multi-byte rune")
	}
	if !strings.Contains(out, `read_knowledge(path="a.md", heading="H")`) {
		t.Errorf("truncation note should point at read_knowledge: %q", out[len(out)-200:])
	}
}

// End to end: with git sync on, write_knowledge commits the one file and the
// background push reaches the remote before the server exits.
func TestWriteCommitsAndPushesWithGitSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...) //nolint:gosec // test helper: fixed binary, test-controlled args
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	git(root, "init", "-q", "--bare", "-b", "main", remote)
	clone := filepath.Join(root, "clone")
	git(root, "clone", "-q", remote, clone)
	git(clone, "config", "user.email", "test@example.com")
	git(clone, "config", "user.name", "test")
	git(clone, "checkout", "-q", "-b", "main")
	git(clone, "commit", "-q", "--allow-empty", "-m", "init")
	git(clone, "push", "-q", "-u", "origin", "main")
	docs := filepath.Join(clone, "docs")
	if err := os.MkdirAll(docs, 0o750); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	srv := NewServer(s, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), docs, "test")
	g, err := gitsync.New(docs, "main")
	if err != nil {
		t.Fatal(err)
	}
	srv.EnableGitSync(g)

	text, isErr := call(t, srv, "write_knowledge", map[string]any{"path": "solutions/x.md", "heading": "X", "content": "the fix"})
	if isErr || !strings.Contains(text, "committed to main") {
		t.Fatalf("write reply: %q", text)
	}
	srv.Wait(30 * time.Second)
	if got := git(remote, "log", "-1", "--format=%s", "main"); got != "docs(kb): solutions/x.md — X" {
		t.Errorf("remote head = %q", got)
	}

	if text, isErr := call(t, srv, "delete_knowledge", map[string]any{"path": "solutions/x.md"}); isErr || !strings.Contains(text, "committed to main") {
		t.Fatalf("delete reply: %q", text)
	}
	srv.Wait(30 * time.Second)
	if got := git(remote, "log", "-1", "--format=%s", "main"); got != "docs(kb): delete solutions/x.md" {
		t.Errorf("remote head after delete = %q", got)
	}
}
