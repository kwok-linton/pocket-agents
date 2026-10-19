package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkingRootRejectsSymlinkEscapeAndMissing(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if e := os.Symlink(outside, filepath.Join(root, "escape")); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{filepath.Join(root, "escape"), filepath.Join(root, "missing"), "relative"} {
		if _, e := ValidatePath([]string{root}, path, false); e == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	path := filepath.Join(root, "new folder")
	if got, e := ValidatePath([]string{root}, path, true); e != nil || got == "" {
		t.Fatal(got, e)
	}
}
func TestIdentityNamespacesAndRestart(t *testing.T) {
	c, e := Load(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	again, e := Load(c.DataDir)
	if e != nil || again.NodeID != c.NodeID {
		t.Fatal("node identity changed", e)
	}
	seen := map[string]bool{}
	for _, node := range []string{"a", "b"} {
		for _, provider := range []string{"claude", "codex"} {
			for _, profile := range []string{"default", "work"} {
				id := SessionID(node, provider, profile, "same-native")
				if seen[id] {
					t.Fatal("identity collision")
				}
				seen[id] = true
			}
		}
	}
}
func TestRemoteHubRequiresTLS(t *testing.T) {
	for _, bad := range []string{"http://100.64.0.1:8787", "https://user:pass@example.com", "https://example.com?token=x", "file:///tmp/hub"} {
		if CheckHub(bad) == nil {
			t.Fatal("accepted", bad)
		}
	}
	for _, good := range []string{"http://127.0.0.1:8787", "https://machine.tailnet.ts.net"} {
		if e := CheckHub(good); e != nil {
			t.Fatal(e)
		}
	}
}
