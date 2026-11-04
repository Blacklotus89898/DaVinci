package gitsync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// run executes git in dir and fails the test on error.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // test helper: fixed binary, test-controlled args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newClone returns the docs dir of a fresh clone of a bare remote whose main
// branch has one commit, plus the remote's path.
func newClone(t *testing.T) (docs, remote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	run(t, root, "init", "-q", "--bare", "-b", "main", remote)
	clone := filepath.Join(root, "clone")
	run(t, root, "clone", "-q", remote, clone)
	run(t, clone, "config", "user.email", "test@example.com")
	run(t, clone, "config", "user.name", "test")
	run(t, clone, "checkout", "-q", "-b", "main")
	docs = filepath.Join(clone, "docs")
	if err := os.MkdirAll(docs, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "seed.md"), []byte("## Seed\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, clone, "add", ".")
	run(t, clone, "commit", "-q", "-m", "seed")
	run(t, clone, "push", "-q", "-u", "origin", "main")
	return docs, remote
}

func TestCommitAndPushOneFile(t *testing.T) {
	docs, remote := newClone(t)
	s, err := New(docs, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docs, "runbooks"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "runbooks", "a.md"), []byte("## A\nfix\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An unrelated local edit must not ride along in the KB commit.
	if err := os.WriteFile(filepath.Join(docs, "seed.md"), []byte("## Seed\nedited\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	committed, err := s.Commit("runbooks/a.md", "docs(kb): runbooks/a.md — A")
	if err != nil || !committed {
		t.Fatalf("Commit = %v, %v", committed, err)
	}
	if err := s.Push(); err != nil {
		t.Fatal(err)
	}
	if got := run(t, remote, "log", "-1", "--format=%s", "main"); got != "docs(kb): runbooks/a.md — A" {
		t.Errorf("remote head = %q", got)
	}
	if files := run(t, remote, "show", "--name-only", "--format=", "main"); files != "docs/runbooks/a.md" {
		t.Errorf("commit touched %q, want only docs/runbooks/a.md", files)
	}

	// Same content again: nothing to commit.
	if committed, err := s.Commit("runbooks/a.md", "noop"); err != nil || committed {
		t.Errorf("unchanged file: Commit = %v, %v", committed, err)
	}
}

func TestCommitDeletion(t *testing.T) {
	docs, _ := newClone(t)
	s, err := New(docs, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(docs, "seed.md")); err != nil {
		t.Fatal(err)
	}
	if committed, err := s.Commit("seed.md", "docs(kb): delete seed.md"); err != nil || !committed {
		t.Fatalf("tracked deletion: Commit = %v, %v", committed, err)
	}
	// A path git never tracked has nothing to commit — and must not error.
	if committed, err := s.Commit("never-existed.md", "x"); err != nil || committed {
		t.Errorf("untracked deletion: Commit = %v, %v", committed, err)
	}
}

func TestCommitRefusesOtherBranch(t *testing.T) {
	docs, _ := newClone(t)
	run(t, docs, "checkout", "-q", "-b", "feat/dev-work")
	s, err := New(docs, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "b.md"), []byte("## B\ny\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("b.md", "x"); !errors.Is(err, ErrWrongBranch) {
		t.Fatalf("want ErrWrongBranch on a feature branch, got %v", err)
	}
	if status := run(t, docs, "status", "--porcelain", "--", "b.md"); !strings.HasPrefix(status, "??") {
		t.Errorf("b.md should stay untracked, status %q", status)
	}
}

func TestNewRejectsNonRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := New(t.TempDir(), "main"); err == nil {
		t.Error("New accepted a directory outside any git work tree")
	}
}
