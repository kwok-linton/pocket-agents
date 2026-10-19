package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/store"
	"strings"
	"testing"
	"time"
)

func testHub(t *testing.T) *Hub {
	t.Helper()
	c, e := config.Load(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s, e := store.Open(filepath.Join(c.DataDir, "hub.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	h, e := New(c, s)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.Close(); s.Close() })
	return h
}
func TestAuthOriginPairingAndRevocation(t *testing.T) {
	h := testHub(t)
	handler := h.Handler(http.NotFoundHandler())
	request := func(method, path, body, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if got := request("GET", "/api/devices", "", "", "").Code; got != 401 {
		t.Fatal(got)
	}
	if got := request("POST", "/api/actions", "{}", h.Owner, "https://evil.example").Code; got != 403 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/node/ws", "", "invalid", "").Code; got != 401 {
		t.Fatal(got)
	}
	w := request("POST", "/api/pairing", "{}", h.Owner, "")
	var pair struct{ Code string }
	if e := json.Unmarshal(w.Body.Bytes(), &pair); e != nil {
		t.Fatal(e)
	}
	d := core.Device{ID: "test-node", Name: "test"}
	token, e := h.Pair(pair.Code, d)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Pair(pair.Code, d); e == nil {
		t.Fatal("pair token reused")
	}
	if token == pair.Code {
		t.Fatal("pairing credential reused as node credential")
	}
	if request("DELETE", "/api/devices/test-node", "", h.Owner, "").Code != 200 {
		t.Fatal("revoke failed")
	}
	var got string
	if h.S.DB.QueryRow("SELECT token FROM devices WHERE id=?", d.ID).Scan(&got) == nil {
		t.Fatal("revoked token retained")
	}
}
func TestNativeEventReplayDedup(t *testing.T) {
	h := testHub(t)
	s := core.Session{ID: "session-one", NodeID: "node-one"}
	h.sessions[s.ID] = s
	e := core.Event{ID: 12, Kind: "completed", SessionID: s.ID, NodeID: s.NodeID, At: time.Now(), Data: json.RawMessage(`{}`)}
	for range 3 {
		if err := h.receiveEvent(s.NodeID, e); err != nil {
			t.Fatal(err)
		}
	}
	events, err := h.S.Events(0, true)
	if err != nil || len(events) != 1 {
		t.Fatal(len(events), err)
	}
}
func TestPushEndpointRejectsPrivateAndRedirectTargets(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1/internal", "http://web.push.apple.com/x", "https://web.push.apple.com.evil.example/x", "https://web.push.apple.com:8443/x"} {
		if validEndpoint(raw) {
			t.Fatal("accepted", raw)
		}
	}
	if !validEndpoint("https://web.push.apple.com/Q/test") {
		t.Fatal("valid endpoint rejected")
	}
}
func TestOfflineIsNotCompletionAndHistoryRemoval(t *testing.T) {
	h := testHub(t)
	d := core.Device{ID: "fixture", Name: "fixture device"}
	if e := h.Register(d, "token"); e != nil {
		t.Fatal(e)
	}
	s := core.Session{Provider: "codex", Profile: "work", NativeID: "same", TurnState: "working"}
	h.snapshot(d.ID, core.Snapshot{Device: d, Sessions: []core.Session{s}})
	sid := config.SessionID(d.ID, s.Provider, s.Profile, s.NativeID)
	if got, _ := h.getSession(sid); got.TurnState != "working" {
		t.Fatal("changed turn state")
	}
	h.snapshot(d.ID, core.Snapshot{Device: d, Sessions: []core.Session{}})
	if _, ok := h.getSession(sid); ok {
		t.Fatal("deleted native session retained as resumable")
	}
}
func TestScaleFixtureFiveNodesHundredSessions(t *testing.T) {
	h := testHub(t)
	for n := range 5 {
		id := string(rune('a' + n))
		d := core.Device{ID: id, Name: id}
		_ = h.Register(d, "fixture-token")
		var sessions []core.Session
		for i := range 20 {
			sessions = append(sessions, core.Session{Provider: "codex", Profile: "default", NativeID: string(rune('A' + i)), Title: "synthetic fixture", Cwd: "/fixture/project"})
		}
		h.snapshot(id, core.Snapshot{Device: d, Sessions: sessions})
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", bytes.NewReader(nil))
	h.listSessions(w, r)
	var rows []core.Session
	if json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != 100 {
		t.Fatal("cross-node isolation failed", len(rows))
	}
}
func TestRevokedNodeCannotReappearFromQueuedSnapshot(t *testing.T) {
	h := testHub(t)
	d := core.Device{ID: "revoked", Name: "test"}
	if e := h.Register(d, "token"); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("DELETE", "/api/devices/revoked", nil)
	r.SetPathValue("id", d.ID)
	h.revoke(httptest.NewRecorder(), r)
	h.snapshot(d.ID, core.Snapshot{Device: d, Sessions: []core.Session{{NativeID: "x", Provider: "codex", Profile: "default"}}})
	if _, ok := h.devices[d.ID]; ok {
		t.Fatal("revoked node resurrected")
	}
}
func TestVAPIDConcurrentInitialization(t *testing.T) {
	h := testHub(t)
	out := make(chan string, 10)
	for range 10 {
		go func() {
			v, e := h.keys()
			if e != nil {
				out <- ""
				return
			}
			out <- v.Public
		}()
	}
	first := <-out
	if first == "" {
		t.Fatal("key generation failed")
	}
	for range 9 {
		if <-out != first {
			t.Fatal("inconsistent VAPID keys")
		}
	}
}
