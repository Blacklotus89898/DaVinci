package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMounts(t *testing.T) {
	root, docs := t.TempDir(), t.TempDir()
	team := filepath.Join(root, "team-kb")
	if err := os.MkdirAll(team, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docs, "runbooks"), 0o750); err != nil {
		t.Fatal(err)
	}

	got, err := Mounts(" team=team-kb ; ", root, docs) // relative → install root
	if err != nil || len(got) != 1 || got[0].Name != "team" || got[0].Root != team {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got, err := Mounts("", root, docs); err != nil || got != nil {
		t.Fatalf("empty: %+v, %v", got, err)
	}
	for _, bad := range []string{
		"Team=" + team,                   // uppercase
		"team",                           // no dir
		"team=" + team + ";team=" + team, // duplicate
		"x=" + filepath.Join(root, "missing"),
		"runbooks=" + team, // shadows docs/runbooks
	} {
		if _, err := Mounts(bad, root, docs); err == nil {
			t.Errorf("Mounts(%q) accepted", bad)
		}
	}
}
