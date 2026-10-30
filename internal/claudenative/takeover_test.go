package claudenative

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"pocketagents/internal/core"
)

// A disposable sleep process is the old owner; a fake CLI simulates native
// resume. No test sends text to, signals, or approves a real Claude session.
func TestPhoneTakeover(t *testing.T) {
	for _, scenario := range []string{"resume", "busy", "waiting", "stale", "unconfirmed", "draft", "wrong-workspace", "peer-send", "launch-failure", "stop-unconfirmed", "shell-drift"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			cwd := t.TempDir()
			child := exec.Command("sleep", "30")
			if scenario == "stop-unconfirmed" {
				child = exec.Command("sh", "-c", "trap '' TERM; exec sleep 30")
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _ = child.Wait(); close(done) }()
			t.Cleanup(func() { _ = child.Process.Kill(); <-done })
			id := "11111111-2222-4333-8444-555555555555"
			owner := agent{ID: "interactive", SessionID: id, Kind: "interactive", PID: child.Process.Pid, Cwd: cwd, Status: "idle"}
			generation := executionID(owner)
			if generation == "" {
				t.Fatal("no fixture generation")
			}
			socketDir, err := os.MkdirTemp("", "pa-take-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(socketDir)
			socket := filepath.Join(socketDir, "s")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := os.Chmod(socket, 0600); err != nil {
				t.Fatal(err)
			}
			start := ""
			if runtime.GOOS == "linux" {
				b, err := os.ReadFile("/proc/" + strconv.Itoa(owner.PID) + "/stat")
				if err != nil {
					t.Fatal(err)
				}
				start = strings.Fields(string(b)[strings.LastIndex(string(b), ")")+1:])[19]
			} else {
				cmd := exec.Command("ps", "-p", strconv.Itoa(owner.PID), "-o", "lstart=")
				cmd.Env = append(os.Environ(), "TZ=UTC")
				b, err := cmd.Output()
				if err != nil {
					t.Fatal(err)
				}
				start = strings.TrimSpace(string(b))
			}
			writeJSON := func(path string, v any) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			presence := inboxRegistry{PID: owner.PID, SessionID: id, Protocol: 1, Socket: socket, ProcStart: start, Status: "idle"}
			switch scenario {
			case "busy":
				presence.Status = "busy" // stale agents listing must not win
			case "waiting":
				presence.WaitingFor = "permission"
			case "stale":
				presence.ProcStart = "old process"
			}
			writeJSON(filepath.Join(home, "sessions", strconv.Itoa(owner.PID)+".json"), presence)
			writeJSON(filepath.Join(home, "projects", "fixture", id+".jsonl"), map[string]any{"type": "assistant", "cwd": cwd, "message": map[string]string{"content": "Done", "stop_reason": "end_turn"}})
			if scenario == "shell-drift" {
				childDir := filepath.Join(cwd, "component")
				if err := os.Mkdir(childDir, 0700); err != nil {
					t.Fatal(err)
				}
				row, _ := json.Marshal(map[string]any{"type": "assistant", "cwd": childDir, "message": map[string]string{"content": "Done"}})
				f, e := os.OpenFile(filepath.Join(home, "projects", "fixture", id+".jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
				if e != nil {
					t.Fatal(e)
				}
				_, e = f.Write(append(row, '\n'))
				f.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
			agentsPath := filepath.Join(home, "agents.json")
			writeJSON(agentsPath, []agent{owner})
			next := agent{ID: "abcdef12", SessionID: id, Kind: "background", Cwd: cwd, PID: os.Getpid(), State: "done"}
			nextJSON, _ := json.Marshal([]agent{next})
			argsPath := filepath.Join(home, "launch-args")
			cli := filepath.Join(home, "claude")
			script := "#!/bin/sh\nif [ \"$1\" = agents ]; then cat " + shellQuote(agentsPath) + "; exit; fi\nprintf '%s\\n' \"$@\" > " + shellQuote(argsPath) + "\n"
			if scenario == "launch-failure" {
				script += "exit 23\n"
			} else {
				script += "printf '%s' " + shellQuote(string(nextJSON)) + " > " + shellQuote(agentsPath) + "\nprintf 'backgrounded · abcdef12\\n'\n"
			}
			if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			provider, err := New(core.Profile{Home: home, Binary: cli, DataDir: home, ClaudeCreatePermissionMode: "bypassPermissions"})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			a := core.Action{ID: "test-takeover", Kind: "takeover", NativeID: id, ExecutionID: generation, Cwd: cwd, Decision: "restart"}
			switch scenario {
			case "unconfirmed":
				a.Decision = ""
			case "draft":
				a.Text = "OK"
			case "wrong-workspace":
				a.Cwd = home
			case "peer-send":
				a.Kind = "send"
				a.Text = "OK"
			}
			timeout := 12 * time.Second
			if scenario == "stop-unconfirmed" {
				timeout = 500 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			r, err := provider.Execute(ctx, a)
			if scenario == "stop-unconfirmed" {
				if err != nil || r.Status != "unknown" || executionID(owner) != generation {
					t.Fatalf("unconfirmed stop: %+v %v", r, err)
				}
				if _, e := os.Stat(argsPath); !os.IsNotExist(e) {
					t.Fatal("started second writer before old owner exited")
				}
				return
			}

			if scenario != "resume" && scenario != "shell-drift" && scenario != "launch-failure" {
				if err == nil || r.Status == "accepted" {
					t.Fatalf("unsafe takeover accepted: %+v %v", r, err)
				}
				if executionID(owner) != generation {
					t.Fatal("rejected action stopped owner")
				}
				if _, err := os.Stat(argsPath); !os.IsNotExist(err) {
					t.Fatal("rejected action launched a writer")
				}
				return
			}
			if executionID(owner) != "" {
				t.Fatal("original writer still live")
			}
			if scenario == "launch-failure" {
				if r.Status == "accepted" || !strings.Contains(r.Message, "history is preserved") {
					t.Fatalf("failure concealed: %+v %v", r, err)
				}
				return
			}
			if err != nil || r.Status != "accepted" || r.NativeID != id || r.ExecutionID == generation {
				t.Fatalf("resume failed: %+v %v", r, err)
			}
			args, err := os.ReadFile(argsPath)
			if err != nil || string(args) != "--bg\n--resume\n"+id+"\n" {
				t.Fatalf("resume added prompt or permissions: %q %v", args, err)
			}
		})
	}
}
