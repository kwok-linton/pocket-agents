package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/store"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func eventNode(t *testing.T) *Node {
	t.Helper()
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(c.DataDir, "node.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c.NodeToken = "fixture-token"
	return New(c, s, nil)
}

func TestEventAcknowledgementLossRestartAndBatchValidation(t *testing.T) {
	n := eventNode(t)
	for range 35 {
		if _, err := n.S.AddEvent("completed", "fixture", n.C.NodeID, nil); err != nil {
			t.Fatal(err)
		}
	}
	var batches [][]int64
	mode := "lost"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node/events" || r.Header.Get("X-Pocket-Node") != n.C.NodeID || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("incorrect notification channel/auth")
		}
		var events []core.Event
		if json.NewDecoder(r.Body).Decode(&events) != nil {
			t.Error("invalid batch")
			return
		}
		ids := []int64{}
		for _, ev := range events {
			ids = append(ids, ev.ID)
		}
		batches = append(batches, ids)
		switch mode {
		case "lost":
			// Receiver persisted the IDs, but its acknowledgement was lost.
			w.WriteHeader(503)
		case "invalid":
			json.NewEncoder(w).Encode(map[string]int64{"ack": 9999})
		case "partial":
			json.NewEncoder(w).Encode(map[string]int64{"ack": events[4].ID})
		default:
			json.NewEncoder(w).Encode(map[string]int64{"ack": events[len(events)-1].ID})
		}
	}))
	defer server.Close()
	n.C.HubURL = server.URL
	if _, err := n.sendEvents(context.Background(), server.Client()); err == nil {
		t.Fatal("lost ack accepted")
	}
	// Reopen the actual journal, rather than relying on an in-memory cursor.
	if err := n.S.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(n.C.DataDir, "node.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n = New(n.C, s, nil)
	mode = "invalid"
	if _, err := n.sendEvents(context.Background(), server.Client()); err == nil {
		t.Fatal("future ack skipped unsent events")
	}
	mode = "partial"
	if _, err := n.sendEvents(context.Background(), server.Client()); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 3 || len(batches[0]) != 32 || !reflect.DeepEqual(batches[0], batches[1]) || !reflect.DeepEqual(batches[1], batches[2]) {
		t.Fatal("unacknowledged batch lost on restart")
	}
	mode = "ok"
	if _, err := n.sendEvents(context.Background(), server.Client()); err != nil {
		t.Fatal(err)
	}
	if len(batches[3]) != 30 || batches[3][0] != 6 || batches[3][29] != 35 {
		t.Fatal("partial acknowledgement skipped events")
	}
	if more, err := n.sendEvents(context.Background(), server.Client()); err != nil || more {
		t.Fatal("acknowledged events replayed", err)
	}
}

type largeHistoryProvider struct {
	testProvider
	returned chan struct{}
}

func (p *largeHistoryProvider) History(context.Context, string, string, int) (core.History, error) {
	defer close(p.returned)
	// Larger than the TCP send buffer, so a non-reading peer blocks WriteJSON.
	return core.History{Messages: []core.Message{{Text: strings.Repeat("x", 8<<20)}}}, nil
}

func TestNotificationsProgressWhileHistorySocketCannotDrain(t *testing.T) {
	n := eventNode(t)
	p := &largeHistoryProvider{returned: make(chan struct{})}
	n.Providers = map[string]core.Provider{"codex:default": p}
	snapshot := make(chan struct{})
	requestHistory := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	eventReceived := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/node/events" {
			var events []core.Event
			if json.NewDecoder(r.Body).Decode(&events) != nil || len(events) == 0 {
				t.Error("no events")
				return
			}
			json.NewEncoder(w).Encode(map[string]int64{"ack": events[len(events)-1].ID})
			eventReceived <- struct{}{}
			return
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var initial core.Wire
		if c.ReadJSON(&initial) != nil {
			return
		}
		close(snapshot)
		<-requestHistory
		data, _ := json.Marshal(map[string]string{"provider": "codex", "profile": "default"})
		if c.WriteJSON(core.Wire{Type: "history", ID: "fixture-history", Data: data}) != nil {
			return
		}
		<-release // never read the large history response or the next snapshot
	}))
	defer func() { once.Do(func() { close(release) }); server.Close() }()
	n.C.HubURL = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectionDone := make(chan error, 1)
	go func() { connectionDone <- n.connect(ctx) }()
	select {
	case <-snapshot:
	case <-time.After(3 * time.Second):
		t.Fatal("socket not connected")
	}
	close(requestHistory)
	select {
	case <-p.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("history not requested")
	}
	if _, err := n.S.AddEvent("waiting_input", "fixture", n.C.NodeID, nil); err != nil {
		t.Fatal(err)
	}
	// No websocket read is needed to persist the acknowledgement.
	sendctx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	if _, err := n.sendEvents(sendctx, server.Client()); err != nil {
		t.Fatal("history blocked notification", err)
	}
	select {
	case <-eventReceived:
	default:
		t.Fatal("event did not reach independent channel")
	}
	var ack int64
	if err := n.S.Get("event-ack", &ack); err != nil || ack == 0 {
		t.Fatal("notification acknowledgement blocked", err)
	}
	once.Do(func() { close(release) })
	cancel()
	select {
	case <-connectionDone:
	case <-time.After(3 * time.Second):
		t.Fatal("connection did not close")
	}
}
