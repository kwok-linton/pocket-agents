package store

import (
	"path/filepath"
	"pocketagents/internal/core"
	"testing"
)

func TestTurnAndEventCommitTogetherAcrossRestartAndWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	working := core.Session{ID: "fixture", NodeID: "fixture-node", Provider: "claude", Profile: "default", NativeID: "native", TurnState: "working", TurnID: "first"}
	completed := working
	completed.TurnState = "completed"
	if err := s.RecordTurn(working, core.Session{}, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_event BEFORE INSERT ON events BEGIN SELECT RAISE(FAIL, 'fixture disk failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTurn(completed, working, true, true); err == nil {
		t.Fatal("event write failure acknowledged")
	}
	if _, err := s.DB.Exec("DROP TRIGGER fail_event"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTurn(completed, working, true, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// last-sessions was not saved before the simulated crash.
	if err := s.RecordTurn(completed, working, true, true); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(0, true)
	if err != nil || len(events) != 1 {
		t.Fatal("failed commit lost event or restart duplicated it", len(events), err)
	}
	working.TurnID = "second"
	if err := s.RecordTurn(working, completed, true, false); err != nil {
		t.Fatal(err)
	}
	completed.TurnID = "second"
	if err := s.RecordTurn(completed, working, true, true); err != nil {
		t.Fatal(err)
	}
	events, err = s.Events(0, true)
	if err != nil || len(events) != 2 {
		t.Fatal("second completion incorrectly deduplicated", len(events), err)
	}
}

func TestCompletionDoesNotReplayOnRecoveryOrStateFlap(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "node.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	session := core.Session{ID: "session", NodeID: "node", TurnID: "one", TurnState: "working", ExecutionState: "alive"}
	record := func(state, turn string) {
		t.Helper()
		previous := session
		session.TurnState = state
		session.TurnID = turn
		if err := s.RecordTurn(session, previous, true, state == "completed"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordTurn(session, core.Session{}, false, false); err != nil {
		t.Fatal(err)
	}
	record("completed", "one")
	record("unknown", "")
	record("completed", "one")
	record("working", "one")
	record("completed", "one")
	events, _ := s.Events(0, true)
	if len(events) != 1 {
		t.Fatalf("replayed completion: %d", len(events))
	}
	record("working", "two")
	record("completed", "two")
	events, _ = s.Events(0, true)
	if len(events) != 2 {
		t.Fatalf("new turn lost: %d", len(events))
	}
	session.ExecutionState = "exited"
	record("completed", "historical")
	events, _ = s.Events(0, true)
	if len(events) != 2 {
		t.Fatalf("historical completion notified: %d", len(events))
	}
}
