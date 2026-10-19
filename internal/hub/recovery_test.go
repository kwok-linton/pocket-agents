package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/node"
	"pocketagents/internal/store"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Fault injection is confined to transport and a counted provider stub. Hub,
// Node.Handle, authentication and both durable SQLite action journals are real.
type recoveryProvider struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (*recoveryProvider) Discover(context.Context) ([]core.Session, error) { return nil, nil }
func (*recoveryProvider) History(context.Context, string, string, int) (core.History, error) {
	return core.History{}, nil
}
func (p *recoveryProvider) Execute(context.Context, core.Action) (core.Result, error) {
	if p.calls.Add(1) == 1 && p.entered != nil {
		close(p.entered)
		<-p.release
	}
	return core.Result{Status: "accepted", NativeID: "fixture-native", ExecutionID: "fixture-execution"}, nil
}
func (*recoveryProvider) Doctor(context.Context) core.Diagnostic { return core.Diagnostic{} }
func (*recoveryProvider) Close() error                           { return nil }

type recoveryRig struct {
	h         *Hub
	n         *node.Node
	ns        *store.Store
	server    *httptest.Server
	conn      *websocket.Conn
	done      chan struct{}
	closeOnce sync.Once
}

func openRecoveryRig(t *testing.T, dir string, p core.Provider, dropFirst bool) *recoveryRig {
	t.Helper()
	hc, err := config.Load(filepath.Join(dir, "hub"))
	if err != nil {
		t.Fatal(err)
	}
	nc, err := config.Load(filepath.Join(dir, "node"))
	if err != nil {
		t.Fatal(err)
	}
	nc.Roots = []string{nc.DataDir}
	hs, err := store.Open(filepath.Join(hc.DataDir, "hub.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	ns, err := store.Open(filepath.Join(nc.DataDir, "node.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(hc, hs)
	if err != nil {
		t.Fatal(err)
	}
	r := &recoveryRig{h: h, ns: ns, n: node.New(nc, ns, map[string]core.Provider{"codex:default": p}), done: make(chan struct{})}
	if err = h.Register(core.Device{ID: nc.NodeID, Name: "fixture-node"}, "fixture-node-token"); err != nil {
		t.Fatal(err)
	}
	r.server = httptest.NewServer(h.Handler(http.NotFoundHandler()))
	header := http.Header{"Authorization": {"Bearer fixture-node-token"}, "X-Pocket-Node": {nc.NodeID}}
	r.conn, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(r.server.URL, "http")+"/api/node/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(core.Snapshot{Device: core.Device{ID: nc.NodeID}, Sessions: []core.Session{}})
	if err = r.conn.WriteJSON(core.Wire{Type: "snapshot", Data: data}); err != nil {
		t.Fatal(err)
	}
	waitRecovery(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return h.devices[nc.NodeID].Online
	})
	go func() {
		defer close(r.done)
		for {
			var req core.Wire
			if r.conn.ReadJSON(&req) != nil {
				return
			}
			value, err := r.n.Handle(context.Background(), req)
			if dropFirst && req.Type == "action" {
				dropFirst = false // Node already persisted its result; discard only its reply.
				continue
			}
			wire := core.Wire{Type: "result", ID: req.ID}
			if err != nil {
				wire.Error = err.Error()
			} else {
				wire.Data, _ = json.Marshal(value)
			}
			if r.conn.WriteJSON(wire) != nil {
				return
			}
		}
	}()
	t.Cleanup(r.close)
	return r
}

func (r *recoveryRig) close() {
	r.closeOnce.Do(func() {
		_ = r.conn.Close()
		<-r.done
		r.h.Close()
		r.server.Close()
		_ = r.ns.Close()
		_ = r.h.S.Close()
	})
}

func waitRecovery(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("recovery condition did not become true")
}

func recoveryRequest(r *recoveryRig, method, path string, payload any) (core.Result, int, error) {
	var body io.Reader
	if payload != nil {
		data, _ := json.Marshal(payload)
		body = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, r.server.URL+path, body)
	req.Header.Set("Authorization", "Bearer "+r.h.Owner)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return core.Result{}, 0, err
	}
	defer res.Body.Close()
	var result core.Result
	err = json.NewDecoder(res.Body).Decode(&result)
	return result, res.StatusCode, err
}

func TestPersistentActionRecoversAfterBrowserLosesResponse(t *testing.T) {
	dir := t.TempDir()
	p := &recoveryProvider{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(p.release) }) }
	defer release()
	r := openRecoveryRig(t, dir, p, false)
	a := core.Action{ID: "browser-lost-response-0001", NodeID: r.n.C.NodeID, Provider: "codex", Profile: "default", Kind: "create", Cwd: r.n.C.DataDir, Text: "literal $(touch nope) 中文🙂"}
	data, _ := json.Marshal(a)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", r.server.URL+"/api/actions", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+r.h.Owner)
	clientDone := make(chan error, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if res != nil {
			res.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not reached")
	}
	cancel() // Browser disconnects before native acceptance is returned.
	if err := <-clientDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled client, got %v", err)
	}
	release()
	waitRecovery(t, func() bool {
		result, err := r.h.S.Result("hub:", a.ID)
		return err == nil && result.Status == "accepted"
	})
	r.close()
	r = openRecoveryRig(t, dir, p, false) // Reopen BOTH journals, not just in-memory maps.
	defer r.close()
	result, code, err := recoveryRequest(r, "GET", "/api/actions/"+a.ID, nil)
	if err != nil || code != 200 || result.Status != "accepted" || result.NativeID != "fixture-native" {
		t.Fatal("durable acknowledgement not recovered", result, code, err)
	}
	var wg sync.WaitGroup
	for range 8 { // Independent browser retries of the exact same logical action.
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, status, err := recoveryRequest(r, "POST", "/api/actions", a)
			if err != nil || status != 200 || !reflect.DeepEqual(got, result) {
				t.Errorf("duplicate did not replay acknowledgement: %+v %d %v", got, status, err)
			}
		}()
	}
	wg.Wait()
	wire, _ := json.Marshal(a)
	if _, err := r.n.Handle(context.Background(), core.Wire{Type: "action", Data: wire}); err != nil {
		t.Fatal("Node journal did not replay a delayed duplicate", err)
	}
	a.Text = "different payload"
	if _, code, err := recoveryRequest(r, "POST", "/api/actions", a); err != nil || code != 409 {
		t.Fatal("changed payload reused an action ID", code, err)
	}
	if p.calls.Load() != 1 {
		t.Fatal("duplicate native injection", p.calls.Load())
	}
}

func TestUnknownHubDeliveryRecoversFromNodeJournalAfterRestart(t *testing.T) {
	dir := t.TempDir()
	p := &recoveryProvider{}
	r := openRecoveryRig(t, dir, p, true)
	a := core.Action{ID: "lost-node-ack-00000001", NodeID: r.n.C.NodeID, Provider: "codex", Profile: "default", Kind: "create", Cwd: r.n.C.DataDir, Text: "deliver only once"}
	// Prime the same durable submission used by the HTTP handler, then use a
	// short RPC deadline to inject a missing Node acknowledgement without a 45s test.
	if _, fresh, err := r.h.S.Claim("hub:", a); err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	if err := r.h.S.Put("action:"+a.ID, a); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err := r.h.RPC(ctx, a.NodeID, "action", a)
	cancel()
	if err == nil || !strings.Contains(err.Error(), "delivery unknown") {
		t.Fatal("lost acknowledgement was not unknown", err)
	}
	if err = r.h.S.Finish("hub:", core.Result{ActionID: a.ID, Status: "unknown", Message: err.Error()}); err != nil {
		t.Fatal(err)
	}
	waitRecovery(t, func() bool {
		result, err := r.ns.Result("node:", a.ID)
		return err == nil && result.Status == "accepted"
	})
	r.close()
	r = openRecoveryRig(t, dir, p, false)
	defer r.close()
	result, code, err := recoveryRequest(r, "GET", "/api/actions/"+a.ID, nil)
	if err != nil || code != 200 || result.Status != "accepted" {
		t.Fatal("unknown result not reconciled from Node", result, code, err)
	}
	if result, err = r.h.S.Result("hub:", a.ID); err != nil || result.Status != "accepted" {
		t.Fatal("reconciled acknowledgement not durable at Hub", result, err)
	}
	if p.calls.Load() != 1 {
		t.Fatal("reconciliation re-executed the action", p.calls.Load())
	}
}
