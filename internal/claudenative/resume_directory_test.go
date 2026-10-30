package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
)

func TestResumeValidatesResolvedWorkspace(t *testing.T) {
	for _, scenario := range []string{"same", "old-alias", "requested-alias", "different", "broken-alias", "shell-drift", "shell-drift-alias"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			real := t.TempDir()
			other := t.TempDir()
			alias := filepath.Join(home, "old-workspace")
			target := real
			if scenario == "broken-alias" {
				target = filepath.Join(home, "missing")
			}
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			recorded, requested := real, real
			switch scenario {
			case "old-alias", "broken-alias", "shell-drift-alias":
				recorded = alias
			case "requested-alias":
				requested = alias
			case "different":
				requested = other
			}
			id := "11111111-2222-3333-4444-555555555555"
			dir := filepath.Join(home, "projects", "workspace")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			line, _ := json.Marshal(map[string]any{"type": "user", "cwd": recorded, "message": map[string]any{"content": "fixture"}})
			if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(line, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "shell-drift" || scenario == "shell-drift-alias" {
				child := filepath.Join(real, "component")
				if err := os.Mkdir(child, 0700); err != nil {
					t.Fatal(err)
				}
				row, _ := json.Marshal(map[string]any{"type": "assistant", "cwd": child, "message": map[string]string{"content": "Done"}})
				f, err := os.OpenFile(filepath.Join(dir, id+".jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write(append(row, '\n'))
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			bin := filepath.Join(home, "fake-claude")
			script := "#!/bin/sh\nif [ \"$1\" = agents ]; then printf '[]'; else pwd; printf 'LAUNCH_REACHED'; exit 23; fi\n"
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			p := &Provider{profile: core.Profile{Home: home, Binary: bin}}
			_, err := p.start(context.Background(), core.Action{Kind: "resume", NativeID: id, Cwd: requested})
			if err == nil {
				t.Fatal("expected fixture error")
			}
			reject := scenario == "different" || scenario == "broken-alias"
			if reject {
				if !strings.Contains(err.Error(), "resume must use") {
					t.Fatalf("wrong rejection: %v", err)
				}
			} else {
				canonical, e := validDirectory(real)
				if e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(err.Error(), "LAUNCH_REACHED") || !strings.Contains(err.Error(), canonical) {
					t.Fatalf("resume did not launch in resolved workspace: %v", err)
				}
			}
		})
	}
}

// Explicit read-only probe for a reported transcript. Never starts or stops it.
func TestReadOnlyResumeDirectory(t *testing.T) {
	path := os.Getenv("POCKET_TEST_RESUME_HISTORY")
	if path == "" {
		t.Skip("explicit read-only transcript probe")
	}
	cwd, err := validDirectory(os.Getenv("POCKET_TEST_RESUME_CWD"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := readMetadata(path, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateResumeDirectory(path, cwd, meta.Cwd); err != nil {
		t.Fatal(err)
	}
	t.Log("reported native workspace passes resume validation without changing the session")
}
