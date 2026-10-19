package claudenative

import (
	"context"
	"encoding/json"
	"github.com/creack/pty"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"pocketagents/internal/core"
)

// Opt-in, dedicated idle sessions only. No model request or business-session
// input is sent. The caller supplies a pre-trusted dedicated test directory.
func TestNativeTerminalLifecycle(t *testing.T) {
	if os.Getenv("POCKET_CLAUDE_LIFECYCLE") != "1" {
		t.Skip("explicit native lifecycle test opt-in required")
	}
	cwd := os.Getenv("POCKET_CLAUDE_CWD")
	if cwd == "" {
		t.Fatal("dedicated POCKET_CLAUDE_CWD required")
	}
	v, err := New(core.Profile{Name: "lifecycle-test", Binary: os.Getenv("POCKET_CLAUDE_BINARY")})
	if err != nil {
		t.Fatal(err)
	}
	p := v.(*Provider)
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	create := func(name string) agent {
		t.Helper()
		cmd := p.command(ctx, "--bg", "--safe-mode", "--name", name)
		cmd.Dir = cwd
		// exec.Cmd's nil stdin is /dev/null, never the test runner's stdin.
		out, err := cmd.Output()
		if err != nil {
			t.Fatal("native test launch", err)
		}
		m := backgroundIDPattern.FindStringSubmatch(terminalScreen(out))
		if len(m) != 2 {
			t.Fatal("test launch ID absent")
		}
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = p.command(cleanup, "stop", m[1]).Run()
			if err := p.command(cleanup, "rm", m[1]).Run(); err != nil {
				t.Error("test-only history cleanup", err)
			}
		})
		aa, err := p.agents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range aa {
			if a.ID == m[1] {
				return a
			}
		}
		t.Fatal("new test worker absent")
		return agent{}
	}
	a, b := create("pocket-terminal-exit-test"), create("pocket-terminal-detach-test")
	generationB := executionID(b)
	open := func(owner agent) (core.Action, core.Result) {
		t.Helper()
		action := core.Action{Provider: "claude", Kind: "terminal", NativeID: owner.SessionID, ExecutionID: executionID(owner), Data: json.RawMessage(`{"open":true}`)}
		r, err := p.Execute(ctx, action)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			TerminalID string `json:"terminalId"`
			RequestID  string `json:"requestId"`
		}
		if json.Unmarshal(r.Data, &data) != nil || data.TerminalID == "" {
			t.Fatal("missing terminal identity")
		}
		action.TerminalID = data.TerminalID
		action.RequestID = data.RequestID
		action.Data = nil
		return action, r
	}
	// Opening another page reuses the same attach. Losing that client leaves
	// the worker running; explicit reopen creates a new connection identity.
	viewB, _ := open(b)
	sharedB, _ := open(b)
	if sharedB.TerminalID != viewB.TerminalID {
		t.Fatal("second view did not reuse attach")
	}
	p.attachments[generationB].disconnect()
	if executionID(b) != generationB {
		t.Fatal("detach stopped its worker")
	}
	_, _ = open(b)
	viewB.Kind = "terminal_input"
	viewB.Text = "\t"
	if _, err = p.Execute(ctx, viewB); err == nil {
		t.Fatal("old connection wrote input")
	}
	// Another native attach client can detach with /exit without granting our
	// observing connection permission to stop the worker.
	external := p.command(ctx, "attach", b.ID)
	output, input, err := startSingleTargetPTY(external, &pty.Winsize{Rows: 32, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	defer input.Close()
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, output); close(drained) }()
	time.Sleep(time.Second)
	if _, err = input.Write([]byte("/exit\r")); err != nil {
		t.Fatal(err)
	}
	if err = external.Wait(); err != nil {
		t.Fatal(err)
	}
	_ = output.Close()
	<-drained
	time.Sleep(500 * time.Millisecond)
	if executionID(b) != generationB {
		t.Fatal("observer stopped another client's detached worker")
	}
	// A real native /exit record plus normal attach completion stops A only.
	action, _ := open(a)
	action.Kind = "terminal_input"
	action.Text = "/exit\r"
	r, err := p.Execute(ctx, action)
	if err != nil || r.Status != "submitted" {
		t.Fatal(r, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for !executionExited(a.SessionID, action.ExecutionID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !executionExited(a.SessionID, action.ExecutionID) {
		t.Fatal("native /exit did not stop original worker")
	}
	if executionID(b) != generationB {
		t.Fatal("exit changed sentinel execution")
	}
	action.Kind = "terminal"
	action.Text = ""
	r, err = p.Execute(ctx, action)
	if err != nil || !strings.Contains(string(r.Data), `"state":"exited"`) {
		t.Fatal("exited read", r, err)
	}
	t.Log("native /exit confirmed; shared attach, other worker and detach/reopen generation preserved")
}
