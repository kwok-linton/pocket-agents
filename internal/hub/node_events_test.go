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
	"testing"
	"time"
)

func fixtureEvent() core.Event {
	s := core.Session{NodeID: "fixture-node", Provider: "claude", Profile: "default", NativeID: "fixture-native", Cwd: "/fixture/project"}
	s.ID = config.SessionID(s.NodeID, s.Provider, s.Profile, s.NativeID)
	data, _ := json.Marshal(core.NotificationFor(s))
	return core.Event{ID: 1, Kind: "waiting_input", NodeID: s.NodeID, SessionID: s.ID, At: time.Now().UTC(), Data: data}
}

func TestIndependentEventBeforeSnapshotRestartDedupAndScope(t *testing.T) {
	h := testHub(t)
	ev := fixtureEvent()
	if err := h.Register(core.Device{ID: ev.NodeID}, "fixture-token"); err != nil {
		t.Fatal(err)
	}
	post := func(events []core.Event, token, origin string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(events)
		r := httptest.NewRequest("POST", "/api/node/events", bytes.NewReader(data))
		r.Header.Set("X-Pocket-Node", ev.NodeID)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.Handler(http.NotFoundHandler()).ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		token, origin string
		code          int
	}{
		{"wrong", "", 401}, {h.Owner, "", 401}, {"fixture-token", "https://browser.invalid", 403},
	} {
		if w := post([]core.Event{ev}, tc.token, tc.origin); w.Code != tc.code {
			t.Fatal("node authentication", w.Code)
		}
	}
	// Deliberately no websocket, session snapshot or online execution device.
	if w := post([]core.Event{ev}, "fixture-token", ""); w.Code != 200 {
		t.Fatal("event depended on snapshot", w.Code, w.Body.String())
	}
	if len(h.sessions) != 0 || h.devices[ev.NodeID].Online {
		t.Fatal("event fabricated online session state")
	}
	h.Close()
	if err := h.S.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(h.C.DataDir, "hub.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restarted, err := New(h.C, db)
	if err != nil {
		t.Fatal(err)
	}
	h = restarted
	defer h.Close()
	if w := post([]core.Event{ev}, "fixture-token", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	events, err := h.S.Events(0, true)
	if err != nil || len(events) != 1 {
		t.Fatal("replay duplicated or lost persisted event", len(events), err)
	}
	if ok, err := h.pushEventAllowed(events[0]); err != nil || !ok {
		t.Fatal("missing snapshot suppressed push", ok, err)
	}
	if err := h.S.Put(preferencesKey, preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: ev.NodeID, Path: "/other"}}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := h.pushEventAllowed(events[0]); err != nil || ok {
		t.Fatal("event context bypassed scope", ok, err)
	}
	if err := h.S.Put(preferencesKey, preferences{WorkspaceScope: "all"}); err != nil {
		t.Fatal(err)
	}
	if err := h.S.SetMetadata(ev.SessionID, core.Metadata{Hidden: true}); err != nil {
		t.Fatal(err)
	}
	if ok, err := h.pushEventAllowed(events[0]); err != nil || ok {
		t.Fatal("event context bypassed hidden metadata", ok, err)
	}
	bad := ev
	bad.ID, bad.SessionID = 2, "another-nodes-session"
	if w := post([]core.Event{bad}, "fixture-token", ""); w.Code == 200 {
		t.Fatal("foreign session event accepted")
	}
	if w := post([]core.Event{ev, ev}, "fixture-token", ""); w.Code != 400 {
		t.Fatal("out-of-order batch accepted")
	}
	if _, err := h.S.DB.Exec("DELETE FROM devices WHERE id=?", ev.NodeID); err != nil {
		t.Fatal(err)
	}
	if w := post([]core.Event{ev}, "fixture-token", ""); w.Code != 401 {
		t.Fatal("revoked node accepted")
	}
	if err := h.persistNodeEvent(ev.NodeID, "fixture-token", ev); err == nil {
		t.Fatal("in-flight revoked request accepted")
	}
}

func TestLegacyMissingSnapshotIsNotAcknowledgedAsDelivered(t *testing.T) {
	h := testHub(t)
	ev := fixtureEvent()
	ev.Data = json.RawMessage(`{"title":"legacy","provider":"claude"}`)
	if err := h.receiveEvent(ev.NodeID, ev); err == nil {
		t.Fatal("unknown session silently discarded")
	}
	h.sessions[ev.SessionID] = core.Session{ID: ev.SessionID, NodeID: ev.NodeID}
	if err := h.receiveEvent(ev.NodeID, ev); err != nil {
		t.Fatal("legacy websocket event compatibility", err)
	}
	delete(h.sessions, ev.SessionID)
	if err := h.receiveEvent(ev.NodeID, ev); err != nil {
		t.Fatal("dedup required removed session", err)
	}
}
