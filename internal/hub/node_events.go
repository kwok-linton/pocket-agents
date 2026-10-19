package hub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
)

// nodeEvents is independent of the snapshot/history websocket. Authenticate
// every request; persistence rechecks the credential inside the transaction so
// a queued request cannot resurrect notifications after device revocation.
func (h *Hub) nodeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		fail(w, 403, "node notifications do not accept browser origins")
		return
	}
	id, token := r.Header.Get("X-Pocket-Node"), bearer(r)
	var want string
	if id == "" || token == "" || h.S.DB.QueryRow("SELECT token FROM devices WHERE id=?", id).Scan(&want) != nil || !equal(want, config.Hash(token)) {
		fail(w, 401, "node authentication required")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	var events []core.Event
	if decoder.Decode(&events) != nil || len(events) == 0 || len(events) > 32 || decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid node event batch")
		return
	}
	var previous int64
	for _, ev := range events {
		if ev.ID <= previous || ev.NodeID != id || !attention(ev.Kind) {
			fail(w, 400, "invalid node event batch")
			return
		}
		previous = ev.ID
	}
	var ack int64
	for _, ev := range events {
		if err := h.persistNodeEvent(id, token, ev); err != nil {
			if ack == 0 {
				fail(w, 503, "node event not persisted; retry")
				return
			}
			break
		}
		ack = ev.ID
	}
	reply(w, map[string]int64{"ack": ack})
}

func notificationSession(ev core.Event) (core.Session, bool) {
	var data core.NotificationContext
	if json.Unmarshal(ev.Data, &data) != nil || data.Provider == "" || data.Profile == "" || data.NativeID == "" {
		return core.Session{}, false
	}
	if config.SessionID(ev.NodeID, data.Provider, data.Profile, data.NativeID) != ev.SessionID {
		return core.Session{}, false
	}
	return core.Session{ID: ev.SessionID, NodeID: ev.NodeID, Provider: data.Provider, Profile: data.Profile, NativeID: data.NativeID, Cwd: data.Cwd, ParentID: data.ParentID}, true
}

var errEventIdentity = errors.New("node event session identity unavailable")
