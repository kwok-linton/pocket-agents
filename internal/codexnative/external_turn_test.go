package codexnative

import (
	"fmt"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"testing"
)

func TestExternalTurnLifecycleWithoutTakingControl(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout.jsonl")
	id := "external-thread"
	write := func(text string) {
		t.Helper()
		f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if _, e = f.WriteString(text); e != nil {
			t.Fatal(e)
		}
	}
	write(`{"type":"session_meta","payload":{"id":"external-thread"}}` + "\n")
	p := &Provider{profile: core.Profile{Home: home}}
	base := core.Session{NativeID: id, ExecutionState: "alive", TurnState: "unknown", Capabilities: core.Capabilities{Discover: true, Persistent: true}}
	event := func(kind, turn string) string {
		return fmt.Sprintf("{\"timestamp\":\"2026-10-19T12:00:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":%q,\"turn_id\":%q}}\n", kind, turn)
	}
	check := func(state, turn string) {
		t.Helper()
		s := p.observeExternalTurn(base, path)
		if s.TurnState != state || s.TurnID != turn {
			t.Fatalf("got %+v", s)
		}
		if s.ExecutionID != "" || s.Capabilities.Resume || s.Capabilities.Send || s.Capabilities.Approve || s.Capabilities.Interrupt {
			t.Fatalf("gained control: %+v", s)
		}
	}
	check("unknown", "")
	write(event("task_started", "one"))
	check("working", "one")
	write(event("task_complete", "one"))
	check("completed", "one")
	write(event("task_started", "two"))
	check("working", "two")
	write(event("task_complete", "one"))
	check("working", "two")
	end := event("turn_aborted", "two")
	write(end[:len(end)-1])
	check("working", "two")
	write("\n")
	check("interrupted", "two")
	check("interrupted", "two") // unchanged polls preserve native evidence
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	write(`{"type":"session_meta","payload":{"id":"external-thread"}}` + "\n")
	check("unknown", "") // replaced/truncated history must not retain stale state
	write(event("task_started", "three"))
	check("working", "three")
}

func TestExternalTurnRejectsUnrelatedOrUnverifiableHistory(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	os.Mkdir(dir, 0700)
	outside := filepath.Join(t.TempDir(), "rollout.jsonl")
	content := []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"another-thread\"}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"one\"}}\n")
	if e := os.WriteFile(outside, content, 0600); e != nil {
		t.Fatal(e)
	}
	inside := filepath.Join(dir, "mismatch.jsonl")
	os.WriteFile(inside, content, 0600)
	link := filepath.Join(dir, "link.jsonl")
	os.Symlink(outside, link)
	p := &Provider{profile: core.Profile{Home: home}}
	for _, path := range []string{outside, inside, link, filepath.Join(dir, "missing")} {
		s := p.observeExternalTurn(core.Session{NativeID: "expected-thread", ExecutionState: "alive", TurnState: "unknown"}, path)
		if s.TurnState != "unknown" || s.TurnID != "" {
			t.Fatalf("unverified history accepted: %+v", s)
		}
	}
	s := p.observeExternalTurn(core.Session{NativeID: "another-thread", ExecutionState: "exited", TurnState: "unknown"}, inside)
	if s.TurnState != "unknown" {
		t.Fatal("dead owner treated as live")
	}
}
