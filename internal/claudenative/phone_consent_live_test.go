package claudenative

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"

	"pocketagents/internal/core"
)

func TestRealPhoneConsent(t *testing.T) {
	mode := os.Getenv("POCKET_TEST_PHONE_CONSENT")
	if mode != "1" && mode != "interactive" {
		t.Skip("opt-in disposable native conversation; no existing session IDs accepted")
	}
	cwd := os.Getenv("POCKET_TEST_PHONE_CWD")
	if cwd == "" {
		t.Fatal("explicit test working directory required")
	}
	provider, err := New(core.Profile{Binary: os.Getenv("POCKET_TEST_CLAUDE_BINARY")})
	if err != nil {
		t.Fatal(err)
	}
	p := provider.(*Provider)
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	prompt := "This is a disposable Pocket phone-input regression test. Do not use any tools, change files, or take external actions. Reply exactly POCKET_CONSENT_READY. If my next message is OK, reply exactly POCKET_CONSENT_CONFIRMED. This confirmation only requests that text response."
	var r core.Result
	if mode == "interactive" {
		id := uuid.NewString()
		cmd := p.command(ctx, "--session-id", id, "--name", "Pocket consent regression fixture", "--", prompt)
		cmd.Dir = cwd
		terminal, e := pty.Start(cmd)
		if e != nil {
			t.Fatal(e)
		}
		waited := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, terminal) }()
		go func() { _ = cmd.Wait(); close(waited) }()
		defer func() { _ = cmd.Process.Kill(); _ = terminal.Close(); <-waited }()
		r = core.Result{NativeID: id, Status: "accepted"}
	} else {
		r, err = p.Execute(ctx, core.Action{ID: "phone-consent-create", Kind: "create", Cwd: cwd, Text: prompt})
	}
	if r.NativeID != "" {
		defer func() {
			cleanCtx, done := context.WithTimeout(context.Background(), 20*time.Second)
			defer done()
			result, e := p.Execute(cleanCtx, core.Action{ID: "phone-consent-cleanup", Kind: "delete", NativeID: r.NativeID, ExecutionID: r.ExecutionID, Cwd: cwd, Decision: "stop_and_delete"})
			if e != nil || result.Status != "accepted" {
				t.Errorf("disposable session cleanup failed: %+v %v", result, e)
			}
		}()
	}
	if err != nil || r.Status != "accepted" {
		t.Fatalf("create: %+v %v", r, err)
	}
	wait := func(token string) {
		t.Helper()
		for {
			h, e := p.History(ctx, r.NativeID, "", 30)
			if e == nil {
				for _, m := range h.Messages {
					if m.Role == "assistant" && strings.TrimSpace(m.Text) == token {
						return
					}
				}
			}
			select {
			case <-ctx.Done():
				t.Fatal("missing native reply", token)
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	wait("POCKET_CONSENT_READY")
	if mode == "interactive" {
		owners, e := p.agents(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, owner := range owners {
			if owner.SessionID == r.NativeID {
				r.ExecutionID = executionID(owner)
			}
		}
		beforeID := r.NativeID
		next, e := p.Execute(ctx, core.Action{ID: "phone-consent-takeover", Kind: "takeover", NativeID: r.NativeID, ExecutionID: r.ExecutionID, Cwd: cwd, Decision: "restart"})
		if e != nil || next.Status != "accepted" || next.NativeID != beforeID || next.ExecutionID == r.ExecutionID {
			t.Fatalf("takeover: %+v %v", next, e)
		}
		r = next
	}

	sent, err := p.Execute(ctx, core.Action{ID: "phone-consent-ok", Kind: "send", NativeID: r.NativeID, ExecutionID: r.ExecutionID, Text: "OK"})
	if err != nil || (sent.Status != "accepted" && sent.Status != "submitted") {
		t.Fatalf("send: %+v %v", sent, err)
	}
	wait("POCKET_CONSENT_CONFIRMED")
	path, err := p.findPath(r.NativeID)
	if err != nil {
		t.Fatal(err)
	}
	// Read the native transcript directly, bypassing Pocket's phone-attribution
	// display transform. A peer envelope cannot satisfy this assertion.
	h, err := readHistory(ctx, path, "", 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range h.Messages {
		if m.Role == "user" && m.Text == "OK" && m.Kind != "peer_message" {
			t.Log("native transcript records literal user OK; model returned confirmation; disposable session cleaned up")
			return
		}
	}
	t.Fatal("OK missing from native user input")
}
