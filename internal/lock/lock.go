// Package lock provides a cross-process advisory lock built on an exclusively
// created lock file. It serialises markdown writes between concurrent
// knowledge-service processes (e.g. several Claude Code sessions at once).
package lock

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultTimeout = 10 * time.Second
	staleAfter     = 30 * time.Second
	retryEvery     = 50 * time.Millisecond
)

// Acquire takes an exclusive lock at path, blocking up to 10 seconds.
// Lock files older than staleAfter (left behind by a crashed process) are
// stolen. The returned release func removes the lock file; calling it more
// than once is safe, and it never removes a lock currently held by another
// process.
func Acquire(path string) (func(), error) {
	return acquire(path, defaultTimeout)
}

func acquire(path string, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	pidTag := fmt.Sprintf("pid=%d", os.Getpid())
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // path is derived from the server's DOCS_PATH, not user input
		if err == nil {
			_, _ = fmt.Fprintf(f, "%s acquired=%s\n", pidTag, time.Now().Format(time.RFC3339))
			_ = f.Close()
			var released bool
			return func() {
				if released {
					return
				}
				released = true
				// Only remove the lock if it is still ours — another process
				// may have stolen a stale lock and recreated it in the meantime.
				if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), pidTag) { //nolint:gosec // lock path built from DOCS_PATH, and reading it back is harmless
					_ = os.Remove(path)
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > staleAfter {
			_ = os.Remove(path) // stale lock from a crashed holder — steal it
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out acquiring %s — another knowledge-service process is writing", path)
		}
		time.Sleep(retryEvery)
	}
}
