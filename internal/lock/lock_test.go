package lock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Lock file exists while held.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file missing while held: %v", err)
	}

	// Second acquisition within the same process must time out (O_EXCL is
	// per-file, not per-process).
	if _, err := acquire(path, 200*time.Millisecond); err == nil {
		t.Fatal("expected second acquisition to time out")
	}

	release()

	// After release the file is gone and can be reacquired.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed on release: %v", err)
	}
	release2, err := Acquire(path)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	release2()

	// Double release is safe.
	release2()
}

func TestAcquireStealsStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.lock")

	// Simulate a crashed holder: an old lock file nobody will release.
	if err := os.WriteFile(path, []byte("pid=999 acquired=long-ago\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleAfter)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	release, err := acquire(path, 2*time.Second)
	if err != nil {
		t.Fatalf("expected stale lock to be stolen, got: %v", err)
	}
	release()
}

func TestReleaseDoesNotRemoveForeignLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.lock")

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Simulate another process stealing our lock (e.g. after a stale window):
	// replace the file with one tagged by a different pid.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("pid=4242 acquired=now\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	release()

	// The foreign lock must survive our release.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("release removed a lock held by another process: %v", err)
	}
}
