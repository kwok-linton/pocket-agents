package hub

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func savePushScopeSession(t *testing.T, h *Hub, session core.Session, metadata core.Metadata) {
	t.Helper()
	b, _ := json.Marshal(session)
	if _, err := h.S.DB.Exec("INSERT INTO sessions VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", session.ID, session.NodeID, b); err != nil {
		t.Fatal(err)
	}
	if err := h.S.SetMetadata(session.ID, metadata); err != nil {
		t.Fatal(err)
	}
}

func TestPushScopeUsesStoredSessionsAndIndependentMetadata(t *testing.T) {
	h := testHub(t)
	cases := []struct {
		name     string
		node     string
		cwd      string
		parent   string
		metadata core.Metadata
		all      bool
		focused  bool
	}{
		{name: "exact", node: "node-a", cwd: "/project", all: true, focused: true},
		{name: "descendant", node: "node-a", cwd: "/project/subdirectory", all: true, focused: true},
		{name: "normalized", node: "node-a", cwd: "/project/subdirectory/../", all: true, focused: true},
		{name: "adjacent-prefix", node: "node-a", cwd: "/project-other", all: true},
		{name: "outside", node: "node-a", cwd: "/elsewhere", all: true},
		{name: "same-path-wrong-node", node: "node-b", cwd: "/project", all: true},
		{name: "unknown-directory", node: "node-a", cwd: "", all: true},
		{name: "child", node: "node-a", cwd: "/project", parent: "parent-session"},
		{name: "hidden", node: "node-a", cwd: "/project", metadata: core.Metadata{Hidden: true}},
		{name: "done-can-request-attention-again", node: "node-a", cwd: "/project", metadata: core.Metadata{Done: true}, all: true, focused: true},
	}
	for _, tc := range cases {
		savePushScopeSession(t, h, core.Session{ID: tc.name, NodeID: tc.node, Cwd: tc.cwd, ParentID: tc.parent}, tc.metadata)
	}
	// No preferences row is the compatible all-workspaces mode. The Hub's
	// in-memory sessions map is deliberately empty: filtering reads the index.
	for _, tc := range cases {
		got, err := h.pushSessionAllowed(tc.name)
		if err != nil || got != tc.all {
			t.Fatal("default all scope", tc.name, got, err)
		}
	}
	if got, err := h.pushSessionAllowed("missing-session"); err != nil || got {
		t.Fatal("missing session guessed as eligible", got, err)
	}
	if err := h.S.Put(preferencesKey, preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: "node-a", Path: "/project"}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		got, err := h.pushSessionAllowed(tc.name)
		if err != nil || got != tc.focused {
			t.Fatal("focused scope", tc.name, got, err)
		}
	}
	if err := h.S.Put(preferencesKey, preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: "node-a", Path: "/"}}}); err != nil {
		t.Fatal(err)
	}
	if got, err := h.pushSessionAllowed("outside"); err != nil || !got {
		t.Fatal("Unix filesystem root did not match its descendant", got, err)
	}
	if err := h.S.Put(preferencesKey, preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{}}); err != nil {
		t.Fatal(err)
	}
	if got, err := h.pushSessionAllowed("exact"); err != nil || got {
		t.Fatal("empty focused selection unexpectedly included a project", got, err)
	}
}

func TestPushSuppressionAdvancesCursorWithoutDeletingEventsOrSending(t *testing.T) {
	h := testHub(t)
	if err := h.S.Put(preferencesKey, preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: "node-a", Path: "/project"}}}); err != nil {
		t.Fatal(err)
	}
	savePushScopeSession(t, h, core.Session{ID: "outside", NodeID: "node-a", Cwd: "/project-neighbor"}, core.Metadata{})
	savePushScopeSession(t, h, core.Session{ID: "child", NodeID: "node-a", Cwd: "/project", ParentID: "parent"}, core.Metadata{})
	savePushScopeSession(t, h, core.Session{ID: "hidden", NodeID: "node-a", Cwd: "/project"}, core.Metadata{Hidden: true})
	var lastID int64
	for _, id := range []string{"outside", "child", "hidden", "missing-session"} {
		ev, err := h.S.AddEvent("completed", id, "node-a", nil)
		if err != nil {
			t.Fatal(err)
		}
		lastID = ev.ID
	}
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := webpush.Subscription{Endpoint: "https://fcm.googleapis.com/fcm/send/SCOPE_TEST_ONLY", Keys: webpush.Keys{
		Auth:   base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 16)),
		P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)),
	}}
	v, err := h.keys()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := pushCaptureClient(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	h.pushSubscription(context.Background(), client, v, sub)
	var cursor int64
	if err := h.S.Get("pushcursor:"+config.Hash(sub.Endpoint), &cursor); err != nil || cursor != lastID || calls != 0 {
		t.Fatal("suppressed notification sent or cursor not advanced", cursor, calls, err)
	}
	events, err := h.S.Events(0, true)
	if err != nil || len(events) != 4 {
		t.Fatal("scope suppression deleted event history", len(events), err)
	}
	var indexed int
	if err = h.S.DB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&indexed); err != nil || indexed != 3 {
		t.Fatal("scope suppression changed the session index", indexed, err)
	}
	if err := h.S.Put(preferencesKey, preferences{WorkspaceScope: "all", Workspaces: []workspacePreference{}}); err != nil {
		t.Fatal(err)
	}
	h.pushSubscription(context.Background(), client, v, sub)
	if calls != 0 {
		t.Fatal("changing scope replayed notifications already intentionally suppressed")
	}
	savePushScopeSession(t, h, core.Session{ID: "done", NodeID: "node-a", Cwd: "/project"}, core.Metadata{Done: true})
	ev, err := h.S.AddEvent("waiting_input", "done", "node-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	h.pushSubscription(context.Background(), client, v, sub)
	if err := h.S.Get("pushcursor:"+config.Hash(sub.Endpoint), &cursor); err != nil || cursor != ev.ID || calls != 1 {
		t.Fatal("done metadata incorrectly silenced a new native request", cursor, calls, err)
	}
	// Corrupt/unreadable preferences are a retryable failure, not an implicit
	// all-workspaces fallback or an intentional suppression acknowledgment.
	if _, err = h.S.DB.Exec("UPDATE kv SET value=? WHERE key=?", []byte("not JSON"), preferencesKey); err != nil {
		t.Fatal(err)
	}
	if _, err = h.S.AddEvent("completed", "done", "node-a", nil); err != nil {
		t.Fatal(err)
	}
	h.pushSubscription(context.Background(), client, v, sub)
	if err := h.S.Get("pushcursor:"+config.Hash(sub.Endpoint), &cursor); err != nil || cursor != ev.ID || calls != 1 {
		t.Fatal("preference read failure sent or acknowledged an event", cursor, calls, err)
	}
}
