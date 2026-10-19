package node

import (
	"context"
	"os"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/store"
	"strings"
	"testing"
)

func TestAttachmentsStayInTargetWorkspace(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(cwd, "note.txt")
	_ = os.WriteFile(path, []byte("content"), 0600)
	n := &Node{C: &config.Config{Roots: []string{cwd, outside}}}
	a := core.Action{Kind: "send", Cwd: cwd, Files: []string{path}}
	if err := n.prepareFiles(&a); err != nil {
		t.Fatal(err)
	}
	if len(a.InputFiles) != 1 || a.InputFiles[0].MIME != "text/plain; charset=utf-8" || !strings.Contains(a.Text, path) {
		t.Fatal("file reference not prepared")
	}
	other := filepath.Join(outside, "other.txt")
	_ = os.WriteFile(other, []byte("private"), 0600)
	if err := n.prepareFiles(&core.Action{Kind: "send", Cwd: cwd, Files: []string{other}}); err == nil {
		t.Fatal("other workspace accepted")
	}
	_ = os.Symlink(other, filepath.Join(cwd, "linked"))
	if err := n.prepareFiles(&core.Action{Kind: "send", Cwd: cwd, Files: []string{filepath.Join(cwd, "linked")}}); err == nil {
		t.Fatal("outside symlink accepted")
	}
}

func TestResumeAcceptsWorkspaceAttachment(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "note.txt")
	os.WriteFile(path, []byte("resume attachment"), 0600)
	n := &Node{C: &config.Config{Roots: []string{cwd}}}
	a := core.Action{Kind: "resume", Cwd: cwd, Files: []string{path}}
	if err := n.prepareFiles(&a); err != nil {
		t.Fatal(err)
	}
	if len(a.InputFiles) != 1 || !strings.Contains(a.Text, path) {
		t.Fatal("resume lost attachment")
	}
}

// Slow delivery is checked after Execute returns (and may be checked after a
// restart). The persisted action must be identical to the actual native input.
type attachmentReceiptProvider struct {
	testProvider
	sent core.Action
}

func (p *attachmentReceiptProvider) Execute(_ context.Context, a core.Action) (core.Result, error) {
	p.sent = a
	return core.Result{Status: "submitted"}, nil
}

func TestAttachmentReceiptPersistsNativePrompt(t *testing.T) {
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.Roots = []string{c.DataDir}
	s, err := store.Open(filepath.Join(c.DataDir, "node.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.Join(c.DataDir, "note.txt")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &attachmentReceiptProvider{}
	n := New(c, s, map[string]core.Provider{"claude:default": p})
	a := core.Action{ID: "attachment-receipt-test", NodeID: c.NodeID, Provider: "claude", Profile: "default", Kind: "send", NativeID: "fixture", ExecutionID: "generation", Cwd: c.DataDir, Text: "看看附件", Files: []string{path}}
	r, err := n.execute(context.Background(), a)
	if err != nil || r.Status != "submitted" {
		t.Fatal(r, err)
	}
	var saved core.Action
	if err := s.Get("node-action:"+a.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Text == a.Text || saved.Text != p.sent.Text {
		t.Fatal("receipt reconciliation lost the native attachment prompt")
	}
}
