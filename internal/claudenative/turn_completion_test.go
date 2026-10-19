package claudenative

import (
	"context"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
)

func TestInteractiveIdleRequiresLatestNativeEndTurn(t *testing.T) {
	end := map[string]any{"type": "assistant", "message": map[string]any{"content": "Done", "stop_reason": "end_turn"}}
	tool := map[string]any{"type": "assistant", "message": map[string]any{"content": "Still running", "stop_reason": "tool_use"}}
	apiError := map[string]any{"type": "assistant", "isApiErrorMessage": true, "message": map[string]any{"content": "API Error: unavailable", "stop_reason": "end_turn"}}
	for _, tc := range []struct {
		name               string
		rows               []map[string]any
		status, wait, want string
	}{
		{"end turn", []map[string]any{userTitleRow("Task"), end}, "idle", "", "completed"},
		{"no evidence", []map[string]any{userTitleRow("Task")}, "idle", "", "unknown"},
		{"new task after completed", []map[string]any{end, userTitleRow("Next task")}, "idle", "", "unknown"},
		{"tool work after completed", []map[string]any{end, tool}, "idle", "", "unknown"},
		{"new request after completed", []map[string]any{end, {"type": "api-request"}}, "idle", "", "unknown"},
		{"live busy overrides transcript", []map[string]any{end}, "busy", "", "working"},
		{"permission overrides transcript", []map[string]any{end}, "waiting", "permission", "waiting_approval"},
		{"error is not completion", []map[string]any{apiError}, "idle", "", "error"},
		{"metadata after completion", []map[string]any{end, {"type": "ai-title", "aiTitle": "Title"}}, "idle", "", "completed"},
		{"sidechain cannot complete parent", []map[string]any{userTitleRow("Task"), {"type": "assistant", "isSidechain": true, "message": map[string]any{"stop_reason": "end_turn"}}}, "idle", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := readMetadata(writeTitleFixture(t, tc.rows...), "default")
			if err != nil {
				t.Fatal(err)
			}
			applyAgent(&s, agent{Kind: "interactive", PID: os.Getpid(), Status: tc.status, WaitingFor: tc.wait})
			if s.TurnState != tc.want {
				t.Fatalf("got %s want %s", s.TurnState, tc.want)
			}
		})
	}
}

func TestHistoricalEndTurnDoesNotBecomeNewCompletion(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(`{"type":"assistant","cwd":"`+home+`","message":{"content":"Done","stop_reason":"end_turn"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(home, "fixture-cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '[]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	provider, err := New(core.Profile{Name: "fixture", Home: home, Binary: cli})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	sessions, err := provider.Discover(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("discover failed: %v", err)
	}
	if sessions[0].TurnState != "unknown" || sessions[0].ExecutionState != "exited" {
		t.Fatal("historical end_turn became a live completion")
	}
}

func TestMetadataGapCannotReviveOldCompletion(t *testing.T) {
	path := writeTitleFixture(t,
		map[string]any{"type": "assistant", "message": map[string]any{"stop_reason": "end_turn"}},
		userTitleRow(strings.Repeat("x", 600*1024)),
		map[string]any{"type": "ai-title", "aiTitle": "Latest title"},
	)
	s, err := readMetadata(path, "default")
	if err != nil {
		t.Fatal(err)
	}
	applyAgent(&s, agent{Kind: "interactive", PID: os.Getpid(), Status: "idle"})
	if s.TurnState != "unknown" {
		t.Fatal("completion from before omitted newer prompt was reused")
	}
}
