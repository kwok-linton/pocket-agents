package claudenative

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketagents/internal/core"
)

func terminalFixture(t *testing.T) (*Provider, agent, *attachment) {
	t.Helper()
	dir := t.TempDir()
	owner := agent{ID: "fixture", SessionID: "10000000-0000-4000-8000-000000000001", PID: os.Getpid(), Kind: "background", Status: "idle", State: "done"}
	rows, _ := json.Marshal([]agent{owner})
	cli := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nif [ \"$1\" != agents ]; then printf unexpected >> " + shellQuote(filepath.Join(dir, "unexpected")) + "; exit 99; fi\nprintf '%s\\n' " + shellQuote(string(rows)) + "\n"
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	v, err := New(core.Profile{Name: "fixture", Home: dir, Binary: cli})
	if err != nil {
		t.Fatal(err)
	}
	p := v.(*Provider)
	t.Cleanup(func() { p.Close() })
	f, err := os.CreateTemp(dir, "input")
	if err != nil {
		t.Fatal(err)
	}
	generation := executionID(owner)
	att := &attachment{terminalID: "view", file: f, cmd: &exec.Cmd{}, output: []byte("idle input")}
	p.attachments[generation] = att
	return p, owner, att
}

func TestTerminalReadsAndStaleKeysNeverConnect(t *testing.T) {
	p, owner, att := terminalFixture(t)
	a := core.Action{Provider: "claude", Kind: "terminal", NativeID: owner.SessionID, ExecutionID: executionID(owner), TerminalID: "view"}
	att.dead = true
	for _, terminal := range []string{"view", "old", ""} {
		a.TerminalID = terminal
		r, err := p.Execute(context.Background(), a)
		if err != nil || !strings.Contains(string(r.Data), `"state":"disconnected"`) {
			t.Fatal(r, err)
		}
		a.Kind = "terminal_input"
		a.Text = "\x1b[B"
		if _, err = p.Execute(context.Background(), a); err == nil {
			t.Fatal("stale navigation accepted")
		}
		a.Kind = "terminal"
	}
	if _, err := os.Stat(filepath.Join(p.profile.Home, "unexpected")); !os.IsNotExist(err) {
		t.Fatal("read or stale key started a native client")
	}
}

func TestIdleScreenAndConnectionIdentityAreRequired(t *testing.T) {
	p, owner, att := terminalFixture(t)
	generation := executionID(owner)
	token := p.terminalRequest(generation)
	if token == "" {
		t.Fatal("idle screen has no token")
	}
	a := core.Action{Kind: "terminal_input", NativeID: owner.SessionID, ExecutionID: generation, TerminalID: "view", Text: "\r"}
	for _, key := range []string{"\r", "text", "\x1b", "\x03"} {
		a.Text = key
		if _, err := p.Execute(context.Background(), a); err == nil {
			t.Fatalf("unguarded idle input %q", key)
		}
	}
	a.Text = "\x1b[B"
	a.TerminalID = "old"
	a.RequestID = token
	if _, err := p.Execute(context.Background(), a); err == nil {
		t.Fatal("old connection could navigate")
	}
	a.TerminalID = "view"
	if _, err := p.Execute(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(att.file.Name())
	if string(b) != "\x1b[B" {
		t.Fatal("rejected input was written")
	}
}

func TestTerminalOpenReusesConnectionAndReadReturnsSameScreenToken(t *testing.T) {
	p, owner, att := terminalFixture(t)
	a := core.Action{Kind: "terminal", NativeID: owner.SessionID, ExecutionID: executionID(owner), Data: json.RawMessage(`{"open":true}`)}
	for range 2 {
		r, err := p.Execute(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			TerminalID string
			RequestID  string
			Screen     string
		}
		if err := json.Unmarshal(r.Data, &data); err != nil || data.TerminalID != att.terminalID || data.RequestID != terminalToken(a.ExecutionID, att.terminalID, data.Screen) {
			t.Fatal("open did not reuse connection and screen token", string(r.Data), err)
		}
	}
	a.Data = nil
	a.TerminalID = att.terminalID
	if _, err := p.Execute(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.profile.Home, "unexpected")); !os.IsNotExist(err) {
		t.Fatal("opening existing connection started another native client")
	}
}

func TestNativeExitRequiresNewExactRecordAndSuccessfulClient(t *testing.T) {
	p, owner, _ := terminalFixture(t)
	dir := filepath.Join(p.profile.Home, "projects", "fixture")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, owner.SessionID+".jsonl")
	since := time.Now().Add(-time.Second)
	canonical := "<command-name>/exit</command-name>\n <command-message>exit</command-message>\n <command-args></command-args>"
	for _, tc := range []struct {
		name, content, id, kind string
		at                      time.Time
		want                    bool
	}{
		{"fresh", canonical, owner.SessionID, "user", time.Now(), true},
		{"old", canonical, owner.SessionID, "user", since.Add(-time.Second), false},
		{"quoted", "Please explain " + canonical, owner.SessionID, "user", time.Now(), false},
		{"different session", canonical, "other", "user", time.Now(), false},
		{"assistant", canonical, owner.SessionID, "assistant", time.Now(), false},
		{"plain exit", "/exit", owner.SessionID, "user", time.Now(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(map[string]any{"type": tc.kind, "sessionId": tc.id, "timestamp": tc.at, "message": map[string]any{"role": tc.kind, "content": tc.content}})
			os.WriteFile(path, append(b, '\n'), 0600)
			if got := p.nativeExitSince(owner.SessionID, since); got != tc.want {
				t.Fatalf("intent=%v", got)
			}
			if _, ok := p.finishNativeExit(context.Background(), owner, executionID(owner), since, errors.New("client disconnected")); ok {
				t.Fatal("client failure stopped worker")
			}
		})
	}
}

func testWorker(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	return cmd
}

func TestStopRequestsOnlyOriginalWorkerAndReconcileNeverRepeats(t *testing.T) {
	p, owner, _ := terminalFixture(t)
	aWorker, bWorker := testWorker(t), testWorker(t)
	owner.PID = aWorker.Process.Pid
	generation := executionID(owner)
	bGeneration := executionID(agent{SessionID: "B", PID: bWorker.Process.Pid})
	marker := filepath.Join(p.profile.Home, "stops")
	script := "#!/bin/sh\n[ \"$1\" = stop ] && [ \"$2\" = fixture ] || exit 99\nprintf stopped >> " + shellQuote(marker) + "\nkill -TERM " + strings.Split(generation, ":")[2] + "\n"
	os.WriteFile(p.profile.Binary, []byte(script), 0700)
	a := core.Action{Kind: "terminate", NativeID: owner.SessionID, ExecutionID: generation}
	r, err := p.stopExecution(context.Background(), a, owner)
	if err != nil || r.Status != "submitted" {
		t.Fatal(r, err)
	}
	if executionID(agent{SessionID: "B", PID: bWorker.Process.Pid}) != bGeneration {
		t.Fatal("other worker changed")
	}
	deadline := time.Now().Add(time.Second)
	for !executionExited(a.NativeID, generation) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// Even if the same native conversation now has a new worker, reconciling
	// an old action only confirms that the old generation ended.
	for i := 0; i < 2; i++ {
		r, err = p.ReconcileResult(context.Background(), a, core.Result{Status: "unknown"})
		if err != nil || r.Status != "accepted" {
			t.Fatal(r, err)
		}
	}
	b, _ := os.ReadFile(marker)
	if string(b) != "stopped" {
		t.Fatal("stop was repeated", string(b))
	}
	owner.PID = bWorker.Process.Pid
	if _, err = p.stopExecution(context.Background(), a, owner); err == nil {
		t.Fatal("replacement generation stopped")
	}
}

func TestStopAcknowledgementReturnsSubmittedUntilReconciled(t *testing.T) {
	p, owner, _ := terminalFixture(t)
	os.WriteFile(p.profile.Binary, []byte("#!/bin/sh\nexit 0\n"), 0700)
	ctx := context.Background()
	a := core.Action{Kind: "terminate", NativeID: owner.SessionID, ExecutionID: executionID(owner)}
	r, err := p.stopExecution(ctx, a, owner)
	if err != nil || r.Status != "submitted" {
		t.Fatal(r, err)
	}
	r, err = p.ReconcileResult(context.Background(), a, r)
	if err != nil || r.Status != "unknown" {
		t.Fatal(r, err)
	}
}

func TestExitInputCorrelatesOnlyThisClientsSubmittedCommand(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		input []string
		want  bool
	}{
		{[]string{"/e", "xit", "\r"}, true},
		{[]string{"/exit\r\n"}, true},
		{[]string{"\x1b[200~", "/exit", "\x1b[201~\r"}, true},
		{[]string{"/exiX\b", "t\r"}, true},
		{[]string{"/exit"}, false},
		{[]string{"explain /exit\r"}, false},
		{[]string{"/exit\x1a"}, false},
		{[]string{"/exit\x03\r"}, false},
		{[]string{"/exit\x1b[D\r"}, false},
		{[]string{}, false},
	} {
		var input exitInput
		for _, s := range tc.input {
			input.observe([]byte(s), at)
		}
		if got := !input.submittedAt().IsZero(); got != tc.want {
			t.Fatalf("input %q intent %v", tc.input, got)
		}
	}
	p, owner, _ := terminalFixture(t)
	// No locally submitted command: do not read another client's exit record or
	// invoke stop even when its attach finishes with exit code zero.
	if _, ok := p.finishNativeExit(context.Background(), owner, executionID(owner), time.Time{}, nil); ok {
		t.Fatal("observer initiated stop")
	}
}

func TestFailedWriteWithoutBytesDoesNotConsumeScreen(t *testing.T) {
	p, owner, att := terminalFixture(t)
	generation := executionID(owner)
	token := p.terminalRequest(generation)
	_ = att.file.Close()
	_, err := p.Execute(context.Background(), core.Action{Kind: "terminal_input", NativeID: owner.SessionID, ExecutionID: generation, TerminalID: "view", RequestID: token, Text: "\r"})
	if err == nil || p.consumed[token] {
		t.Fatal("failed input consumed screen", err)
	}
}
