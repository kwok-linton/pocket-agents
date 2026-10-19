package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"pocketagents/internal/core"
	"testing"
)

func TestRenameRejectsTerminalControlCharacters(t *testing.T) {
	p := &Provider{}
	for _, name := range []string{"", "hello\n/exit", "hello\x1b", "hello\x00", "hello\tthere", "hello\bthere"} {
		r, err := p.renameHistory(context.Background(), core.Action{Text: name})
		if err == nil || r.Status != "rejected" {
			t.Fatalf("accepted unsafe name %q", name)
		}
	}
}
func TestRenameRequiresEmptyNativePrompt(t *testing.T) {
	for _, tc := range []struct {
		screen, suggestion string
		want               bool
	}{
		{"─\n❯\n─\nbypass permissions on", "", true},
		{"─\n❯ Suggested next task\n─", "Suggested next task", true},
		{"─\n❯ unsent draft\n─", "", false},
		{"─\n❯\nwrapped draft\n─", "", false},
		{"❯ 1. Approve\n2. Deny", "", false},
		{"working…", "", false},
		{"❯", "", false},
	} {
		if got := renamePromptEmpty(tc.screen, tc.suggestion); got != tc.want {
			t.Errorf("screen %q: got %v", tc.screen, got)
		}
	}
}

func TestLiveRenameUsesExistingTerminalAndNativeConfirmation(t *testing.T) {
	dir := t.TempDir()
	owner := agent{ID: "fixture", SessionID: "10000000-0000-4000-8000-000000000001", PID: os.Getpid(), Kind: "background", Status: "idle", State: "done"}
	rows, _ := json.Marshal([]agent{owner})
	cli := filepath.Join(dir, "claude")
	os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' "+shellQuote(string(rows))+"\n"), 0700)
	v, err := New(core.Profile{Name: "fixture", Home: dir, Binary: cli})
	if err != nil {
		t.Fatal(err)
	}
	p := v.(*Provider)
	defer p.Close()
	f, err := os.CreateTemp(dir, "input")
	if err != nil {
		t.Fatal(err)
	}
	id := executionID(owner)
	p.attachments[id] = &attachment{file: f, cmd: &exec.Cmd{}, output: []byte("─\r\n❯\r\n─\r\n")}
	path := filepath.Join(dir, "history.jsonl")
	os.WriteFile(path, []byte("{\"type\":\"custom-title\",\"customTitle\":\"New name\"}\n"), 0600)
	r, err := p.renameLive(context.Background(), core.Action{ID: "rename", NativeID: owner.SessionID, ExecutionID: id}, owner, path, "New name")
	if err != nil || r.Status != "accepted" {
		t.Fatal(r, err)
	}
	input, _ := os.ReadFile(f.Name())
	if string(input) != "/rename New name\r" {
		t.Fatalf("unexpected input %q", input)
	}
	owner.State = "working"
	if _, err = p.renameLive(context.Background(), core.Action{ExecutionID: id}, owner, path, "Unsafe"); err == nil {
		t.Fatal("busy rename allowed")
	}
	after, _ := os.ReadFile(f.Name())
	if string(after) != string(input) {
		t.Fatal("busy rename wrote input")
	}
}

func TestRenameStopsCompletedResidentWorkerBeforeHistoricalRename(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "projects", "fixture")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	id := "10000000-0000-4000-8000-000000000002"
	path := filepath.Join(project, id+".jsonl")
	initial, _ := json.Marshal(map[string]any{
		"type": "user", "sessionId": id, "cwd": cwd,
		"message": map[string]string{"role": "user", "content": "test"},
	})
	if err := os.WriteFile(path, append(initial, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	owner := agent{ID: "deadbeef", SessionID: id, Cwd: cwd, PID: os.Getpid(), Kind: "background", Status: "idle", State: "done"}
	rows, _ := json.Marshal([]agent{owner})
	cli := filepath.Join(dir, "claude")
	script := `#!/bin/sh
if [ "$1" = agents ]; then
  if [ -f "` + dir + `/stopped" ]; then printf '[]'; else printf '%s' '` + string(rows) + `'; fi
  exit 0
fi
if [ "$1" = stop ]; then
  touch "` + dir + `/stopped"
  exit 0
fi
if [ "$1" = --resume ]; then
  printf '%s\n' '{"type":"custom-title","customTitle":"New name"}' >> "` + path + `"
  exit 0
fi
exit 3
`
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	p := &Provider{profile: core.Profile{Name: "fixture", Home: dir, Binary: cli}}
	r, err := p.renameHistory(context.Background(), core.Action{ID: "rename", NativeID: id, Cwd: cwd, Text: "New name"})
	if err != nil || r.Status != "accepted" {
		t.Fatalf("rename failed: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stopped")); err != nil {
		t.Fatalf("completed worker was not stopped: %v", err)
	}
	meta, err := readMetadata(path, "fixture")
	if err != nil || meta.Title != "New name" {
		t.Fatalf("native title not updated: %+v %v", meta, err)
	}
}
