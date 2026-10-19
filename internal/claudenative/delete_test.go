package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"testing"
)

func TestDeleteHistoryScopeAndLiveGuard(t *testing.T) {
	for _, mode := range []string{"delete", "live", "symlink", "wrong-cwd", "stop-delete", "stale-instance", "stop-failed"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			project := filepath.Join(home, "projects", "test")
			os.MkdirAll(project, 0700)
			id := "11111111-2222-3333-4444-555555555555"
			path := filepath.Join(project, id+".jsonl")
			workspace := t.TempDir()
			keep := filepath.Join(workspace, "code.txt")
			os.WriteFile(keep, []byte("keep"), 0600)
			data, _ := json.Marshal(map[string]any{"type": "user", "sessionId": id, "cwd": workspace, "message": map[string]string{"role": "user", "content": "test"}})
			os.WriteFile(path, append(data, '\n'), 0600)
			agents := "[]"
			if mode == "live" || mode == "stop-delete" || mode == "stale-instance" || mode == "stop-failed" {
				b, _ := json.Marshal([]map[string]any{{"sessionId": id, "pid": os.Getpid(), "id": "test-owner", "kind": "background"}})
				agents = string(b)
			}
			bin := filepath.Join(home, "claude")
			os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s' '"+agents+"'\n"), 0700)
			if mode == "stop-delete" || mode == "stale-instance" || mode == "stop-failed" {
				script := "#!/bin/sh\nif [ \"$1\" = stop ]; then "
				if mode == "stop-failed" {
					script += "exit 1; "
				} else {
					script += "touch '" + home + "/stopped'; "
				}
				script += "fi\nif [ -f '" + home + "/stopped' ]; then echo '[]'; else printf '%s' '" + agents + "'; fi\n"
				os.WriteFile(bin, []byte(script), 0700)
			}
			if mode == "symlink" {
				os.Rename(path, path+".original")
				os.Symlink(path+".original", path)
			}
			cwd := workspace
			if mode == "wrong-cwd" {
				cwd = home
			}
			p := &Provider{profile: core.Profile{Home: home, Binary: bin}}
			action := core.Action{ID: "test", NativeID: id, Cwd: cwd}
			if mode == "stop-delete" || mode == "stale-instance" || mode == "stop-failed" {
				owners, err := p.agents(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				action.Decision = "stop_and_delete"
				action.ExecutionID = executionID(owners[0])
				if mode == "stale-instance" {
					action.ExecutionID = "stale"
				}
			}
			r, e := p.deleteHistory(context.Background(), action)
			if mode == "delete" || mode == "stop-delete" {
				if e != nil || r.Status != "accepted" {
					t.Fatalf("%+v %v", r, e)
				}
				if _, e = os.Stat(path); !os.IsNotExist(e) {
					t.Fatal("transcript remains")
				}
			} else {
				if e == nil {
					t.Fatal("unsafe delete accepted")
				}
				if _, e = os.Lstat(path); e != nil {
					t.Fatal("record lost")
				}
			}
			if b, e := os.ReadFile(keep); e != nil || string(b) != "keep" {
				t.Fatal("workspace changed")
			}
		})
	}
}
