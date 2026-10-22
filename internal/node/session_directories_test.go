package node

import (
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"testing"
)

func TestCanonicalSessionDirectoriesKeepsIdentityAndDistinctProjects(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "work", "project")
	other := filepath.Join(root, "other", "project")
	for _, dir := range []string{real, other} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "old-project")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing")
	input := []core.Session{{ID: "old", NativeID: "native", Cwd: alias}, {ID: "new", Cwd: real}, {ID: "other", Cwd: other}, {Cwd: missing}, {Cwd: ""}, {Cwd: filepath.Join(alias, "removed-worktree")}}
	out := canonicalSessionDirectories(input)
	if out[0].Cwd != real || out[1].Cwd != real || out[2].Cwd != other || out[3].Cwd != missing || out[4].Cwd != "" || out[5].Cwd != filepath.Join(real, "removed-worktree") {
		t.Fatalf("bad directories: %+v", out)
	}
	if input[0].Cwd != alias || out[0].ID != "old" || out[0].NativeID != "native" {
		t.Fatal("provider record or native identity changed")
	}
}
