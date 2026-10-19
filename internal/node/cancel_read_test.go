package node

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"pocketagents/internal/core"
	"sync"
	"testing"
	"time"
)

type cancelHistoryProvider struct {
	testProvider
	entered, stopped chan struct{}
}

func (p *cancelHistoryProvider) History(ctx context.Context, _ string, _ string, _ int) (core.History, error) {
	close(p.entered)
	<-ctx.Done()
	close(p.stopped)
	return core.History{}, ctx.Err()
}
func TestCanceledHistoryReleasesNativeReadWithoutDisconnecting(t *testing.T) {
	n := eventNode(t)
	p := &cancelHistoryProvider{entered: make(chan struct{}), stopped: make(chan struct{})}
	n.Providers = map[string]core.Provider{"codex:default": p}
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{EnableCompression: true}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var message core.Wire
		if c.ReadJSON(&message) != nil {
			return
		}
		data, _ := json.Marshal(historyRequest{Provider: "codex", Profile: "default", NativeID: "fixture"})
		if c.WriteJSON(core.Wire{Type: "history", ID: "read-to-cancel", Data: data}) != nil {
			return
		}
		select {
		case <-p.entered:
		case <-r.Context().Done():
			return
		}
		if c.WriteJSON(core.Wire{Type: "cancel_read", ID: "read-to-cancel"}) != nil {
			return
		}
		<-release
	}))
	defer func() { once.Do(func() { close(release) }); server.Close() }()
	n.C.HubURL = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- n.connect(ctx) }()
	select {
	case <-p.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("native history ignored cancellation")
	}
	select {
	case err := <-done:
		t.Fatal("canceled read disconnected session socket", err)
	default:
	}
	once.Do(func() { close(release) })
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture did not stop")
	}
}
