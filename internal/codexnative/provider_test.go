package codexnative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"pocketagents/internal/core"
)

func TestNativeWriterOwnershipAndGeneration(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "thread-writer-locks")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	socket := filepath.Join(home, "socket")
	if e := os.WriteFile(socket, nil, 0600); e != nil {
		t.Fatal(e)
	}
	p := &Provider{profile: core.Profile{Home: home}, socket: socket}
	id := "pocket-test-native-id"
	lock := filepath.Join(dir, id+".lock")
	f, e := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	held, e := p.writerHeld(id)
	if e != nil || !held {
		t.Fatalf("external native writer not detected: %v %v", held, e)
	}
	before := p.execution(id)
	p.epoch = "reconnected"
	if before == "" || p.execution(id) != before {
		t.Fatal("client reconnection must not change native execution identity")
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	held, e = p.writerHeld(id)
	if e != nil || held {
		t.Fatalf("released writer still held: %v %v", held, e)
	}
	f.Close()
	os.Remove(lock)
	if p.execution(id) != "" {
		t.Fatal("missing native lock must produce unknown identity")
	}
	if e = os.WriteFile(lock, nil, 0600); e != nil {
		t.Fatal(e)
	}
	future := time.Now().Add(time.Second)
	os.Chtimes(lock, future, future)
	if p.execution(id) == before {
		t.Fatal("new native writer must change generation")
	}
	if _, e = p.writerHeld("../../outside"); e == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestExternalWriterIsAliveReadOnlyWithoutInventedExecution(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "thread-writer-locks")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := &Provider{profile: core.Profile{Home: home}, sessions: map[string]core.Session{}}
	id := "external-native-thread"
	file, err := os.OpenFile(filepath.Join(dir, id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	// Simulate the stale shared-daemon state preceding an external owner.
	p.sessions[id] = core.Session{TurnState: "working", TurnID: "old-turn"}
	unloaded := p.merge(nativeThread{ID: id}, false)
	held := p.externalWriterState(unloaded)
	if held.ExecutionState != "alive" || held.TurnState != "unknown" || held.ExecutionID != "" || len(held.Pending) != 0 {
		t.Fatalf("external ownership misclassified: %+v", held)
	}
	want := core.Capabilities{Discover: true, Persistent: true, Reason: held.Capabilities.Reason}
	if !reflect.DeepEqual(held.Capabilities, want) {
		t.Fatalf("external writer gained a control capability: %+v", held.Capabilities)
	}
	if locked, e := p.writerHeld(id); e != nil || !locked {
		t.Fatalf("classification disturbed the native lock: %v %v", locked, e)
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	// The mere presence of a leftover lock file does not prove liveness and
	// must not change the existing unloaded/resume behavior or capabilities.
	stale := p.externalWriterState(unloaded)
	if !reflect.DeepEqual(stale, unloaded) || stale.ExecutionState != "exited" {
		t.Fatalf("unheld lock falsely became a live writer: %+v", stale)
	}
	if err = os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
	if absent := p.externalWriterState(unloaded); !reflect.DeepEqual(absent, unloaded) {
		t.Fatalf("absent lock changed unloaded capabilities: %+v", absent)
	}
	// A failed ownership check cannot authorize resume or claim the owner died.
	invalid := unloaded
	invalid.NativeID = "../invalid"
	uncertain := p.externalWriterState(invalid)
	if uncertain.ExecutionState != "unknown" || uncertain.ExecutionID != "" || uncertain.TurnState != "unknown" {
		t.Fatalf("failed ownership check did not remain unknown: %+v", uncertain)
	}
	want.Reason = uncertain.Capabilities.Reason
	if !reflect.DeepEqual(uncertain.Capabilities, want) {
		t.Fatalf("failed ownership check allowed native control: %+v", uncertain.Capabilities)
	}
}
func TestApprovalResolutionAndOldConnectionIsolation(t *testing.T) {
	p := &Provider{epoch: "epoch-a", sessions: map[string]core.Session{}, pending: map[string]*pendingRequest{}}
	params := json.RawMessage(`{"threadId":"native-test-id","turnId":"turn-1","command":"printf ok","availableDecisions":["accept","cancel"]}`)
	m := rpcMessage{ID: json.RawMessage(`7`), Method: "item/commandExecution/requestApproval", Params: params}
	p.event(m)
	p.event(m)
	if len(p.pending) != 1 {
		t.Fatal("native request duplicated")
	}
	req := p.pending["epoch-a:n:7"]
	if len(req.Approval.Options) != 2 || req.Approval.Options[1] != "cancel" {
		t.Fatal("native offered options not preserved")
	}
	p.event(rpcMessage{Method: "serverRequest/resolved", Params: json.RawMessage(`{"threadId":"native-test-id","requestId":7}`)})
	if len(p.pending) != 0 {
		t.Fatal("locally answered request remained available")
	}
	select {
	case <-req.Resolved:
	default:
		t.Fatal("resolution not signalled")
	}
	_, e := p.approve(context.Background(), nil, core.Action{RequestID: req.Approval.ID, NativeID: "native-test-id"}, core.Result{})
	if e == nil {
		t.Fatal("old approval accepted")
	}
	p.epoch = "epoch-b"
	p.event(m)
	if _, ok := p.pending[req.Approval.ID]; ok {
		t.Fatal("reused RPC request ID collided across connection generations")
	}
}
func TestHistoryKeepsNativeKindsAndBoundaries(t *testing.T) {
	user := renderItem(json.RawMessage(`{"type":"userMessage","id":"user-1","content":[{"type":"text","text":"你好\n🙂"}]}`))
	if user.Text != "你好\n🙂" || user.Role != "user" {
		t.Fatalf("unicode lost: %#v", user)
	}
	command := renderItem(json.RawMessage(`{"type":"commandExecution","id":"tool-1","command":"printf ok","aggregatedOutput":"ok"}`))
	if command.Role != "tool" || !strings.Contains(command.Text, "ok") {
		t.Fatal("tool output missing")
	}
	compaction := renderItem(json.RawMessage(`{"type":"contextCompaction","id":"boundary-1"}`))
	if compaction.Role != "system" {
		t.Fatal("native compaction boundary hidden")
	}
	for _, test := range []struct{ native, want string }{{"completed", "completed"}, {"interrupted", "interrupted"}, {"failed", "error"}, {"inProgress", "working"}, {"idle", "unknown"}} {
		if got := turnState(test.native); got != test.want {
			t.Fatalf("%s became %s", test.native, got)
		}
	}
}
func TestMissingDirectoryNeverFallsBack(t *testing.T) {
	if _, e := cleanCwd(filepath.Join(t.TempDir(), "does-not-exist")); e == nil {
		t.Fatal("missing workspace accepted")
	}
	if _, e := cleanCwd(""); e == nil {
		t.Fatal("empty workspace accepted")
	}
}
