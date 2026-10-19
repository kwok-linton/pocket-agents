package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/store"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type testProvider struct{ calls atomic.Int32 }

func (p *testProvider) Discover(context.Context) ([]core.Session, error) { return nil, nil }
func (p *testProvider) History(context.Context, string, string, int) (core.History, error) {
	return core.History{}, nil
}

type recoveryDiscoveryProvider struct {
	testProvider
	sessions []core.Session
	err      error
}

func (p *recoveryDiscoveryProvider) Discover(context.Context) ([]core.Session, error) {
	return p.sessions, p.err
}

func TestRestartWithUnavailableProviderPreservesUnknownHistory(t *testing.T) {
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(c.DataDir, "node.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	saved := core.Session{NodeID: c.NodeID, Provider: "codex", Profile: "default", NativeID: "fixture-native", ExecutionID: "old-generation", ExecutionState: "alive", TurnState: "waiting_approval", Cwd: c.DataDir, Capabilities: core.Capabilities{Discover: true, Attach: true, Send: true, Approve: true, Resume: true, Terminal: true, Interrupt: true, Terminate: true}, Pending: []core.Approval{{ID: "old-request", ExecutionID: "old-generation"}}}
	saved.ID = config.SessionID(c.NodeID, saved.Provider, saved.Profile, saved.NativeID)
	if err = s.Put("last-sessions", map[string]core.Session{saved.ID: saved}); err != nil {
		t.Fatal(err)
	}
	p := &recoveryDiscoveryProvider{err: errors.New("native socket not yet available")}
	n := New(c, s, map[string]core.Provider{"codex:default": p})
	assertUnknown := func() {
		t.Helper()
		ss := n.Snapshot().Sessions
		if len(ss) != 1 || ss[0].ID != saved.ID || ss[0].NativeID != saved.NativeID {
			t.Fatal("restart lost the native history index", ss)
		}
		got := ss[0]
		want := core.Capabilities{Discover: true, Reason: got.Error}
		if got.ExecutionState != "unknown" || got.TurnState != "unknown" || len(got.Pending) != 0 || got.Capabilities != want {
			t.Fatal("persisted live state or stale controls survived restart", got)
		}
	}
	assertUnknown()
	n.scan(context.Background())
	assertUnknown()
	p.err = nil
	fresh := saved
	fresh.ExecutionID = "verified-generation"
	fresh.TurnState = "completed"
	fresh.Pending = nil
	p.sessions = []core.Session{fresh}
	n.scan(context.Background())
	ss := n.Snapshot().Sessions
	if len(ss) != 1 || ss[0].ExecutionID != fresh.ExecutionID || ss[0].TurnState != "completed" || !ss[0].Capabilities.Send {
		t.Fatal("successful native discovery did not restore verified state", ss)
	}
}
func (p *testProvider) Execute(context.Context, core.Action) (core.Result, error) {
	p.calls.Add(1)
	return core.Result{Status: "accepted"}, nil
}
func (p *testProvider) Doctor(context.Context) core.Diagnostic { return core.Diagnostic{} }
func (p *testProvider) Close() error                           { return nil }
func TestNodeRejectsMisrouteAndReplaysAckWithoutExecuting(t *testing.T) {
	c, e := config.Load(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c.Roots = []string{c.DataDir}
	s, e := store.Open(filepath.Join(c.DataDir, "node.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := &testProvider{}
	n := New(c, s, map[string]core.Provider{"codex:default": p})
	a := core.Action{ID: "stable-test-action-0001", NodeID: c.NodeID, Provider: "codex", Profile: "default", Kind: "create", Cwd: c.DataDir, Text: "literal `$(touch nope)` 你好"}
	for range 2 {
		r, e := n.execute(context.Background(), a)
		if e != nil || r.Status != "accepted" {
			t.Fatal(r, e)
		}
	}
	if p.calls.Load() != 1 {
		t.Fatal("duplicate provider execution")
	}
	a.NodeID = "another-device"
	if _, e = n.execute(context.Background(), a); e == nil {
		t.Fatal("wrong node accepted")
	}
	a.NodeID = c.NodeID
	a.ID = "stable-test-action-0002"
	a.Cwd = filepath.Join(c.DataDir, "missing")
	r, e := n.execute(context.Background(), a)
	if e != nil || r.Status != "rejected" || p.calls.Load() != 1 {
		t.Fatal("missing directory fallback", r, e)
	}
	data, _ := json.Marshal(map[string]string{"id": "stable-test-action-0001"})
	result, e := n.Handle(context.Background(), core.Wire{Type: "action_result", Data: data})
	if e != nil || result.(core.Result).Status != "accepted" {
		t.Fatal(result, e)
	}
}

type slowDoctorProvider struct{ testProvider }

func (p *slowDoctorProvider) Doctor(ctx context.Context) core.Diagnostic {
	<-ctx.Done()
	return core.Diagnostic{}
}

func TestNodeConnectsBeforeNativeDiagnosticsFinish(t *testing.T) {
	connected := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var message core.Wire
		if c.ReadJSON(&message) == nil && message.Type == "snapshot" {
			connected <- struct{}{}
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.HubURL, cfg.NodeToken = server.URL, "test-token"
	s, err := store.Open(filepath.Join(cfg.DataDir, "node.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n := New(cfg, s, map[string]core.Provider{"claude:default": &slowDoctorProvider{}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("node did not stop")
		}
	}()
	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("native diagnostics blocked device connection")
	}
}
