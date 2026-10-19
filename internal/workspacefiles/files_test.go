package workspacefiles

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestTransferAndBoundaries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600)
	_ = os.Symlink(outside, filepath.Join(root, "escape"))
	for _, path := range []string{"../secret", filepath.Join(outside, "secret"), "escape/secret"} {
		if _, err := Read(root, path); err == nil {
			t.Fatalf("escape accepted: %s", path)
		}
	}
	content := []byte("中文附件\x00binary")
	e, err := Write(root, "1234567890123456", "图片.png", content)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Read(root, e.Path)
	if err != nil || !bytes.Equal(b, content) {
		t.Fatalf("roundtrip: %v", err)
	}
	if _, err = Write(root, "1234567890123456", "图片.png", []byte("overwrite")); err == nil {
		t.Fatal("duplicate overwrote attachment")
	}
	for _, name := range []string{"../outside", "/absolute", "a\nb", "a\\b", ".."} {
		if _, err := Write(root, "2345678901234567", name, content); err == nil {
			t.Fatalf("invalid name accepted: %q", name)
		}
	}
	if _, err := Write(root, "2345678901234567", "large", make([]byte, MaxBytes+1)); err == nil {
		t.Fatal("oversized upload")
	}
	_ = os.WriteFile(filepath.Join(root, "large"), make([]byte, MaxBytes+1), 0600)
	if _, err := Read(root, "large"); err == nil {
		t.Fatal("oversized download")
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "pipe"); err == nil {
		t.Fatal("named pipe accepted")
	}
	entries, err := List(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "escape" || e.Name == "pipe" {
			t.Fatal("unsafe file listed")
		}
	}
}
