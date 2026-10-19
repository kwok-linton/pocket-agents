package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
)

func TestOversizedTerminalAnswerDoesNotConsumePendingRequest(t *testing.T) {
	dir := t.TempDir()
	owner := agent{ID: "fixture", SessionID: "10000000-0000-4000-8000-000000000001", PID: os.Getpid(), Kind: "background", Status: "waiting", WaitingFor: "permission prompt"}
	agents, _ := json.Marshal([]agent{owner})
	cli := filepath.Join(dir, "fixture-claude")
	// A fixture native owner and output file exercise Execute's validation and
	// wait-token path without invoking any real agent or approval.
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' "+shellQuote(string(agents))+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	provider, err := New(core.Profile{Name: "fixture", Home: dir, Binary: cli})
	if err != nil {
		t.Fatal(err)
	}
	p := provider.(*Provider)
	defer p.Close()
	output, err := os.CreateTemp(dir, "native-input-*")
	if err != nil {
		t.Fatal(err)
	}
	generation := executionID(owner)
	if generation == "" {
		t.Fatal("fixture owner generation unavailable")
	}
	p.attachments[generation] = &attachment{terminalID: "fixture-terminal", file: output, cmd: &exec.Cmd{}}
	request := p.terminalRequest(generation)
	a := core.Action{ID: "oversized-answer-fixture", Kind: "terminal_input", TerminalID: "fixture-terminal", NativeID: owner.SessionID, ExecutionID: generation, RequestID: request, Text: strings.Repeat("x", 64*1024) + "\r"}
	if _, err = p.Execute(context.Background(), a); err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatal("oversized input not rejected", err)
	}
	if p.consumed[request] {
		t.Fatal("validation failure consumed a native wait token")
	}
	a.ID = "valid-answer-fixture"
	a.Text = "\r"
	result, err := p.Execute(context.Background(), a)
	if err != nil || result.Status != "submitted" || !p.consumed[request] {
		t.Fatal("corrected answer could not use the same native wait", result, err)
	}
	// Enter may toggle a checkbox without finishing the native question.
	if _, err = p.Execute(context.Background(), a); err == nil {
		t.Fatal("duplicate Enter on unchanged screen was accepted")
	}
	a.Text = "\x1b[B"
	if _, err = p.Execute(context.Background(), a); err != nil {
		t.Fatal("navigation after checkbox selection was blocked", err)
	}
	att := p.attachments[generation]
	att.output = []byte("Choose apps\r\n> 2. [ ] Second app")
	next := p.terminalRequest(generation)
	if next == request {
		t.Fatal("changed screen retained consumed token")
	}
	a.Text = "\r"
	if _, err = p.Execute(context.Background(), a); err == nil {
		t.Fatal("stale screen submission accepted")
	}
	a.RequestID = next
	if _, err = p.Execute(context.Background(), a); err != nil {
		t.Fatal("next selection in same native question rejected", err)
	}
	// Arrows pressed in a row carry a token from an older screen; they cannot
	// submit, so they go through. Enter with that old token still does not.
	a.RequestID = request
	a.Text = "\x1b[B\x1b[B"
	result, err = p.Execute(context.Background(), a)
	if err != nil {
		t.Fatal("repeated navigation with an older screen token rejected", err)
	}
	var shown struct {
		Screen    string `json:"screen"`
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(result.Data, &shown) != nil || !strings.Contains(shown.Screen, "Second app") || shown.RequestID != next {
		t.Fatal("key response does not carry the redrawn screen and its token", string(result.Data))
	}
	a.Text = "\r"
	if _, err = p.Execute(context.Background(), a); err == nil {
		t.Fatal("Enter with an older screen token accepted")
	}
	for _, text := range []string{"\x1b[B\r", "\x1b[Bx", "\x1b", ""} {
		if terminalNavigation(text) {
			t.Fatalf("%q treated as navigation", text)
		}
	}
	data, err := os.ReadFile(output.Name())
	if err != nil || string(data) != "\r\x1b[B\r\x1b[B\x1b[B" {
		t.Fatal("unexpected native input", len(data), err)
	}
}
