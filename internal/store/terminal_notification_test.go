package store

import (
	"path/filepath"
	"pocketagents/internal/core"
	"testing"
)

func TestTerminalNotificationsDeduplicateAcrossFlapsAndRestart(t *testing.T) {
	for _, state := range []string{"interrupted", "error"} {
		t.Run(state, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "node.sqlite")
			s, e := Open(path)
			if e != nil {
				t.Fatal(e)
			}
			next := core.Session{ID: "session", NodeID: "node", TurnID: "one", TurnState: "working", ExecutionState: "alive"}
			record := func(state string) {
				t.Helper()
				old := next
				next.TurnState = state
				if e := s.RecordTurn(next, old, true, state != "working" && state != "unknown"); e != nil {
					t.Fatal(e)
				}
			}
			record("working")
			record(state)
			record("unknown")
			record("working")
			record(state)
			if e := s.Close(); e != nil {
				t.Fatal(e)
			}
			s, e = Open(path)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			record("working")
			record(state)
			events, e := s.Events(0, true)
			if e != nil || len(events) != 1 {
				t.Fatalf("duplicate terminal notification: %d %v", len(events), e)
			}
			next.TurnID = "two"
			record("working")
			record(state)
			events, e = s.Events(0, true)
			if e != nil || len(events) != 2 {
				t.Fatalf("new turn suppressed: %d %v", len(events), e)
			}
		})
	}
}
