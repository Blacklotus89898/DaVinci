// Package gitsync commits knowledge-base edits and pushes them, so the git
// remote — what other devices and Warden read — never lags the local docs
// tree. It is opt-in (KB_GIT_SYNC=1) and best-effort: a failed commit or push
// is reported to the caller, never allowed to fail the write itself.
package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// pushTimeout bounds pull+push. It stays under the write lock's 30s
// stale-steal window so a slow network never gets its lock stolen mid-rebase.
const pushTimeout = 25 * time.Second

// ErrWrongBranch means the work tree is not on the sync branch (e.g. a
// feature branch checked out for development); nothing is committed.
var ErrWrongBranch = errors.New("work tree is not on the sync branch")

// Syncer commits single files inside a git work tree and pushes its branch.
type Syncer struct {
	dir    string // docs directory; git runs here, so pathspecs are relative to it
	branch string // only commit when HEAD is this branch
	sshCmd string // GIT_SSH_COMMAND for network ops ("" = leave git's choice alone)
}

// New returns a Syncer for dir, which must be inside a git work tree.
func New(dir, branch string) (*Syncer, error) {
	s := &Syncer{dir: dir, branch: branch}
	if _, err := s.git(context.Background(), "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, fmt.Errorf("%s is not inside a git work tree: %w", dir, err)
	}
	// BatchMode makes an ssh remote without a loaded key fail fast instead of
	// waiting on a prompt nobody can answer. It is appended to the ssh the
	// user configured (core.sshCommand — e.g. Windows OpenSSH for ssh-agent),
	// never substituted for it; an explicit GIT_SSH(_COMMAND) is left alone.
	if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") == "" {
		ssh, _ := s.git(context.Background(), "config", "--get", "core.sshCommand")
		if ssh == "" {
			ssh = "ssh"
		} else if strings.ContainsAny(ssh, " \t") && !strings.ContainsAny(ssh, `"'`) {
			ssh = `"` + ssh + `"` // a bare path with spaces
		}
		s.sshCmd = ssh + " -o BatchMode=yes"
	}
	return s, nil
}

// Branch is the branch this Syncer commits to.
func (s *Syncer) Branch() string { return s.branch }

// Commit stages relPath (an edit or a deletion) and commits only that path.
// It reports false when there was nothing to commit.
func (s *Syncer) Commit(relPath, msg string) (bool, error) {
	ctx := context.Background()
	head, err := s.git(ctx, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || head != s.branch {
		return false, fmt.Errorf("%w (on %q, want %q)", ErrWrongBranch, head, s.branch)
	}
	if _, statErr := os.Stat(filepath.Join(s.dir, relPath)); os.IsNotExist(statErr) {
		// A deleted path git never tracked has nothing to commit (and
		// `git add` would fail on the unmatched pathspec).
		if tracked, _ := s.git(ctx, "ls-files", "--", relPath); tracked == "" {
			return false, nil
		}
	}
	if _, err := s.git(ctx, "add", "-A", "--", relPath); err != nil {
		return false, err
	}
	if _, err := s.git(ctx, "diff", "--cached", "--quiet", "--", relPath); err == nil {
		return false, nil // staged state equals HEAD: nothing changed
	}
	if _, err := s.git(ctx, "commit", "-q", "-m", msg, "--", relPath); err != nil {
		return false, err
	}
	return true, nil
}

// Push rebases onto the remote branch (other devices push KB entries too) and
// pushes. A failed rebase is aborted so the work tree is never left mid-rebase.
func (s *Syncer) Push() error {
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	if _, err := s.git(ctx, "pull", "--rebase", "--autostash", "-q"); err != nil {
		_, _ = s.git(context.Background(), "rebase", "--abort")
		return fmt.Errorf("pull: %w", err)
	}
	if _, err := s.git(ctx, "push", "-q"); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}

func (s *Syncer) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // fixed binary; args are git subcommands plus a validated docs-relative path
	cmd.Dir = s.dir
	// Never block on a prompt: the MCP server has no terminal.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if s.sshCmd != "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+s.sshCmd)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, msg)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(out.String()), nil
}
