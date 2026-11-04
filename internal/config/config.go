// Package config makes the binaries self-locating: settings come from the
// process environment, then a knowledge.env file next to the install, then
// install-relative defaults — never from the caller's working directory.
//
// Why: an MCP client registration used to carry every setting as -e flags.
// Re-registering to add one flag silently dropped the others (DOCS_PATH has
// no default), and the server exited at startup — CONNECTION_CLOSED. Now the
// registration can be just the executable path; flags only override.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the per-device settings file, looked up in the install root.
const FileName = "knowledge.env"

// Origin says where an effective setting came from (for the startup log).
type Origin struct {
	Key, Value, From string // From: "env", the file path, or "default"
}

// Root returns the install root for an executable: the directory holding it,
// or that directory's parent when it is named bin (the repo layout
// <repo>/bin/knowledge-service[.exe] next to <repo>/docs).
func Root(exe string) string {
	dir := filepath.Dir(exe)
	if strings.EqualFold(filepath.Base(dir), "bin") {
		return filepath.Dir(dir)
	}
	return dir
}

// Apply fills unset environment variables from <root>/knowledge.env, then
// defaults DOCS_PATH to <root>/docs (if that directory exists) and DB_PATH to
// <root>/knowledge.db (only when the install was recognised: a settings file
// or a docs directory — so `go run` from a temp dir never invents a DB there).
// Variables already set in the environment always win. It returns the
// settings it applied, in a stable order, for logging.
func Apply(root string) ([]Origin, error) {
	var applied []Origin
	set := func(k, v, from string) {
		if _, ok := os.LookupEnv(k); ok && os.Getenv(k) != "" {
			return
		}
		_ = os.Setenv(k, v)
		applied = append(applied, Origin{k, v, from})
	}

	path := filepath.Join(root, FileName)
	kv, err := readEnvFile(path)
	recognised := err == nil
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	}
	for _, p := range kv {
		v := p[1]
		if (p[0] == "DOCS_PATH" || p[0] == "DB_PATH") && v != "" && !filepath.IsAbs(v) {
			v = filepath.Join(root, v) // relative to the file, not the cwd
		}
		set(p[0], v, path)
	}

	if docs := filepath.Join(root, "docs"); isDir(docs) {
		recognised = true
		set("DOCS_PATH", docs, "default")
	}
	if recognised {
		set("DB_PATH", filepath.Join(root, "knowledge.db"), "default")
	}
	return applied, nil
}

// ApplyForExecutable is Apply for the running binary's install root.
func ApplyForExecutable() (root string, applied []Origin, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	root = Root(exe)
	applied, err = Apply(root)
	return root, applied, err
}

// readEnvFile parses KEY=VALUE lines; blank lines and # comments are
// skipped, surrounding quotes and an `export ` prefix are tolerated.
func readEnvFile(path string) ([][2]string, error) {
	f, err := os.Open(path) //nolint:gosec // fixed name in the install root
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	var out [][2]string
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, n)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out = append(out, [2]string{k, v})
	}
	return out, sc.Err()
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
