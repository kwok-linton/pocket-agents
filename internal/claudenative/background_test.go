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

	"pocketagents/internal/core"
)

func TestDiscoverBackgroundRequiresVerifiedNativePresence(t *testing.T) {
	// A real private socket and this test process exercise the existing identity
	// checks without starting Claude or touching a user's native session.
	socketDir, err := os.MkdirTemp("", "pa-bg-")
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
	var start string
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile("/proc/self/stat")
		if err != nil {
			t.Fatal(err)
		}
		start = strings.Fields(string(b)[strings.LastIndex(string(b), ")")+1:])[19]
	} else {
		cmd := exec.Command("ps", "-p", strconv.Itoa(os.Getpid()), "-o", "lstart=")
		cmd.Env = append(os.Environ(), "TZ=UTC")
		b, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		start = strings.TrimSpace(string(b))
	}
	for _, tc := range []struct {
		name, status, state, wait, presence, presenceWait, stop, identity, want string
	}{
		{name: "background shell", status: "busy", presence: "shell", stop: "end_turn", want: "background"},
		{name: "model resumes", status: "busy", presence: "busy", stop: "end_turn", want: "working"},
		{name: "background ends", status: "idle", presence: "idle", stop: "end_turn", want: "completed"},
		{name: "new tool work", status: "busy", presence: "shell", stop: "tool_use", want: "working"},
		{name: "explicit working", status: "busy", state: "working", presence: "shell", stop: "end_turn", want: "working"},
		{name: "approval wins", status: "waiting", wait: "permission", presence: "shell", stop: "end_turn", want: "waiting_approval"},
		{name: "registry wait", status: "busy", presence: "shell", presenceWait: "permission", stop: "end_turn", want: "working"},
		{name: "unsupported presence", status: "busy", presence: "future", stop: "end_turn", want: "working"},
		{name: "missing presence", status: "busy", identity: "missing", stop: "end_turn", want: "working"},
		{name: "reused pid", status: "busy", presence: "shell", identity: "stale", stop: "end_turn", want: "working"},
		{name: "other session", status: "busy", presence: "shell", identity: "other", stop: "end_turn", want: "working"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			write := func(path string, value any, mode os.FileMode) {
				t.Helper()
				b, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, '\n'), mode); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(home, "projects", "fixture", "test.jsonl"), map[string]any{
				"type": "assistant", "cwd": home,
				"message": map[string]string{"content": "Done", "stop_reason": tc.stop},
			}, 0600)
			presence := inboxRegistry{PID: os.Getpid(), SessionID: "test", Protocol: 1, Socket: socket, ProcStart: start, Status: tc.presence, WaitingFor: tc.presenceWait}
			if tc.identity == "stale" {
				presence.ProcStart = "previous process"
			}
			if tc.identity == "other" {
				presence.SessionID = "other"
			}
			if tc.identity != "missing" {
				write(filepath.Join(home, "sessions", strconv.Itoa(os.Getpid())+".json"), presence, 0600)
			}
			cli := filepath.Join(home, "cli")
			a := agent{Kind: "interactive", SessionID: "test", PID: os.Getpid(), Cwd: home, Status: tc.status, State: tc.state, WaitingFor: tc.wait}
			b, err := json.Marshal([]agent{a})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' "+shellQuote(string(b))+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			provider, err := New(core.Profile{Home: home, Binary: cli, DataDir: home})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			sessions, err := provider.Discover(context.Background())
			if err != nil || len(sessions) != 1 {
				t.Fatalf("discover: %v, sessions=%d", err, len(sessions))
			}
			if sessions[0].TurnState != tc.want {
				t.Fatalf("got %s, want %s", sessions[0].TurnState, tc.want)
			}
		})
	}
}
