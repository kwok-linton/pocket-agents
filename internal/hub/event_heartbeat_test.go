package hub

import (
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIdleEventStreamSendsHeartbeatWithoutCreatingEvent(t *testing.T) {
	h := testHub(t)
	server := httptest.NewServer(h.Handler(http.NotFoundHandler()))
	defer server.Close()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+h.Owner)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/events/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(18 * time.Second))
	var message map[string]any
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "heartbeat" || message["id"] != nil {
		t.Fatal("heartbeat confused with a persisted event")
	}
	events, err := h.S.Events(0, false)
	if err != nil || len(events) != 0 {
		t.Fatal("heartbeat wrote an inbox event")
	}
}
