package config

import (
	"os"
	"path/filepath"
	"testing"
)

// clearEnv unsets the keys for one test (t.Setenv restores them afterwards).
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
}

var keys = []string{"DOCS_PATH", "DB_PATH", "EMBED_PROVIDER", "KB_GIT_SYNC"}

func TestRoot(t *testing.T) {
	repo := filepath.Join("r", "DaVinci")
	if got := Root(filepath.Join(repo, "bin", "knowledge-service.exe")); got != repo {
		t.Errorf("bin layout: got %q want %q", got, repo)
	}
	if got := Root(filepath.Join(repo, "knowledge-service")); got != repo {
		t.Errorf("flat layout: got %q want %q", got, repo)
	}
}

func TestDefaultsFromInstallRoot(t *testing.T) {
	clearEnv(t, keys...)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOCS_PATH"); got != filepath.Join(root, "docs") {
		t.Errorf("DOCS_PATH = %q", got)
	}
	if got := os.Getenv("DB_PATH"); got != filepath.Join(root, "knowledge.db") {
		t.Errorf("DB_PATH = %q", got)
	}
}

func TestEnvFileFillsOnlyWhatTheEnvironmentLacks(t *testing.T) {
	clearEnv(t, keys...)
	root := t.TempDir()
	absDB := filepath.Join(t.TempDir(), "k.db") // absolute on every OS
	file := "# per-device settings\nexport EMBED_PROVIDER=ollama\nKB_GIT_SYNC=\"1\"\nDOCS_PATH=notes\nDB_PATH=" + absDB + "\n"
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	// The registration passes only one flag — the regression this fixes.
	t.Setenv("EMBED_PROVIDER", "tfidf-override")

	applied, err := Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"EMBED_PROVIDER": "tfidf-override",             // env wins
		"KB_GIT_SYNC":    "1",                          // quotes stripped
		"DOCS_PATH":      filepath.Join(root, "notes"), // relative to the file
		"DB_PATH":        absDB,                        // absolute kept as-is
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for _, a := range applied {
		if a.Key == "EMBED_PROVIDER" {
			t.Errorf("EMBED_PROVIDER was set by the environment and must not be reported as applied: %+v", a)
		}
	}
}

func TestUnrecognisedDirInventsNothing(t *testing.T) {
	clearEnv(t, keys...)
	if _, err := Apply(t.TempDir()); err != nil { // no file, no docs/ — e.g. `go run`
		t.Fatal(err)
	}
	for _, k := range []string{"DOCS_PATH", "DB_PATH"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s should stay unset, got %q", k, v)
		}
	}
}

func TestBadLineIsAnError(t *testing.T) {
	clearEnv(t, keys...)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("just words\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root); err == nil {
		t.Error("want a parse error for a line without '='")
	}
}
