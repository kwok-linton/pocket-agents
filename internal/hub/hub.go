package hub

import (
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/store"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type peer struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	pending sync.Map
}

func (p *peer) write(v core.Wire) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	p.conn.EnableWriteCompression(len(v.Data) >= 1024)
	return p.conn.WriteJSON(v)
}

type Hub struct {
	clientMu        sync.Mutex
	vapidMu         sync.Mutex
	C               *config.Config
	S               *store.Store
	Owner           string
	mu              sync.RWMutex
	peers           map[string]*peer
	sessions        map[string]core.Session
	sessionObserved map[string]time.Time
	devices         map[string]core.Device
	wakeMu          sync.Mutex
	wake            chan struct{}
	pairMu          sync.Mutex
	pushWake        chan struct{}
}

func New(c *config.Config, s *store.Store) (*Hub, error) {
	owner, e := config.Secret(c.DataDir, "owner-token")
	if e != nil {
		return nil, e
	}
	h := &Hub{C: c, S: s, Owner: owner, peers: map[string]*peer{}, sessions: map[string]core.Session{}, sessionObserved: map[string]time.Time{}, devices: map[string]core.Device{}, wake: make(chan struct{}), pushWake: make(chan struct{}, 1)}
	rows, e := s.DB.Query("SELECT id,name,data FROM devices")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id, name string
		var b []byte
		var d core.Device
		if e = rows.Scan(&id, &name, &b); e != nil {
			rows.Close()
			return nil, e
		}
		if json.Unmarshal(b, &d) == nil {
			d.ID = id
			d.Name = name
			d.Online = false
			h.devices[id] = d
		}
	}
	rows.Close()
	rows, e = s.DB.Query("SELECT id,data FROM sessions")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id string
		var b []byte
		var v core.Session
		if e = rows.Scan(&id, &b); e != nil {
			rows.Close()
			return nil, e
		}
		if json.Unmarshal(b, &v) == nil {
			h.sessions[id] = v
		}
	}
	rows.Close()
	return h, nil
}

// Every open browser event stream waits on the same channel; closing it wakes
// all of them (a buffered send would wake only one, the rest a second later).
func (h *Hub) changes() <-chan struct{} {
	h.wakeMu.Lock()
	defer h.wakeMu.Unlock()
	return h.wake
}
func (h *Hub) signal(kind string) {
	h.wakeMu.Lock()
	close(h.wake)
	h.wake = make(chan struct{})
	h.wakeMu.Unlock()
	if !attention(kind) {
		return
	}
	select {
	case h.pushWake <- struct{}{}:
	default:
	}
}
func (h *Hub) Emit(kind, sid, node string, v any) {
	ev, e := h.S.AddEvent(kind, sid, node, v)
	if e != nil {
		log.Printf("event persistence: %v", e)
		return
	}
	// Snapshot events only tell browsers to reload; a busy workspace records
	// thousands a day. Keep a recent window for cursor catch-up.
	if kind == "snapshot" && ev.ID%500 == 0 {
		_, _ = h.S.DB.Exec("DELETE FROM events WHERE kind='snapshot' AND id<?", ev.ID-2000)
	}
	h.signal(kind)
}
func (h *Hub) Register(d core.Device, token string) error {
	b, _ := json.Marshal(d)
	_, e := h.S.DB.Exec("INSERT INTO devices VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET token=excluded.token", d.ID, config.Hash(token), d.Name, b)
	if e != nil {
		return e
	}
	h.mu.Lock()
	if _, ok := h.devices[d.ID]; !ok {
		h.devices[d.ID] = d
	}
	h.mu.Unlock()
	return nil
}
func (h *Hub) RPC(ctx context.Context, node, typ string, data any) (core.Wire, error) {
	h.mu.RLock()
	p := h.peers[node]
	d := h.devices[node]
	h.mu.RUnlock()
	if p == nil || !d.Online {
		return core.Wire{}, errors.New("node is offline; no action delivered")
	}
	id := config.Random()
	ch := make(chan core.Wire, 1)
	p.pending.Store(id, ch)
	defer p.pending.Delete(id)
	b, _ := json.Marshal(data)
	if e := p.write(core.Wire{Type: typ, ID: id, Data: b}); e != nil {
		return core.Wire{}, fmt.Errorf("delivery unknown: %w", e)
	}
	select {
	case r := <-ch:
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		return r, nil
	case <-ctx.Done():
		if typ == "history" || typ == "history_sync" {
			go func() { _ = p.write(core.Wire{Type: "cancel_read", ID: id}) }()
		}
		return core.Wire{}, errors.New("delivery unknown: response timeout; inspect native state before resending")
	}
}
func (h *Hub) nodeWS(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Pocket-Node")
	token := bearer(r)
	var want string
	if id == "" || token == "" || h.S.DB.QueryRow("SELECT token FROM devices WHERE id=?", id).Scan(&want) != nil || !equal(want, config.Hash(token)) {
		fail(w, 401, "node authentication required")
		return
	}
	if r.Header.Get("Origin") != "" {
		fail(w, 403, "node sockets do not accept browser origins")
		return
	}
	conn, e := (&websocket.Upgrader{EnableCompression: true, CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
	if e != nil {
		return
	}
	conn.SetReadLimit(16 << 20)
	_ = conn.SetCompressionLevel(flate.BestSpeed)
	p := &peer{conn: conn}
	h.mu.Lock()
	var verified string
	if _, ok := h.devices[id]; !ok {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	if h.S.DB.QueryRow("SELECT token FROM devices WHERE id=?", id).Scan(&verified) != nil || !equal(verified, config.Hash(token)) {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	old := h.peers[id]
	h.peers[id] = p
	h.mu.Unlock()
	if old != nil {
		_ = old.conn.Close()
	}
	defer func() {
		_ = conn.Close()
		h.mu.Lock()
		if h.peers[id] == p {
			delete(h.peers, id)
			if d, ok := h.devices[id]; ok {
				d.Online = false
				h.devices[id] = d
				// Snapshots no longer persist LastSeen; keep "最近连接" accurate
				// across Hub restarts by saving it when the connection ends.
				if b, e := json.Marshal(d); e == nil {
					_, _ = h.S.DB.Exec("UPDATE devices SET data=? WHERE id=?", b, id)
				}
			}
		}
		h.mu.Unlock()
		h.Emit("device", "", id, nil)
	}()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		var m core.Wire
		if conn.ReadJSON(&m) != nil {
			return
		}
		switch m.Type {
		case "snapshot":
			var snap core.Snapshot
			if json.Unmarshal(m.Data, &snap) != nil || snap.Device.ID != id {
				continue
			}
			h.snapshot(id, snap)
		case "events":
			var events []core.Event
			if json.Unmarshal(m.Data, &events) == nil {
				var ack int64
				for _, ev := range events {
					if err := h.persistNodeEvent(id, token, ev); err != nil {
						break
					}
					ack = ev.ID
				}
				if ack > 0 {
					b, _ := json.Marshal(ack)
					_ = p.write(core.Wire{Type: "events_ack", Data: b})
				}
			}
		case "heartbeat":
			h.mu.Lock()
			if d, ok := h.devices[id]; ok {
				d.Online = true
				d.LastSeen = time.Now().UTC()
				h.devices[id] = d
			}
			h.mu.Unlock()
		default:
			if c, ok := p.pending.Load(m.ID); ok {
				select {
				case c.(chan core.Wire) <- m:
				default:
				}
			}
		}
	}
}
func (h *Hub) snapshot(id string, snap core.Snapshot) {
	h.mu.Lock()
	if _, registered := h.devices[id]; !registered {
		h.mu.Unlock()
		return
	}
	previousDevice := h.devices[id]
	snap.Device.ID = id
	snap.Device.Name = previousDevice.Name
	snap.Device.Online = true
	snap.Device.LastSeen = time.Now().UTC()
	h.devices[id] = snap.Device
	// LastSeen alone changes on every snapshot; persisting it is a disk write
	// per tick that nothing reads back (online state is rebuilt on connect).
	before, after := previousDevice, snap.Device
	before.LastSeen, after.LastSeen, before.Online = time.Time{}, time.Time{}, true
	pb, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	var e error
	if string(pb) != string(b) {
		b, _ = json.Marshal(snap.Device)
		if _, e = h.S.DB.Exec("UPDATE devices SET data=? WHERE id=?", b, id); e != nil {
			log.Printf("device update: %v", e)
		}
	}
	seen := map[string]bool{}
	changed := false
	var changedIDs []string
	for _, s := range snap.Sessions {
		if s.NativeID == "" || s.Provider == "" || s.Profile == "" {
			continue
		}
		s.NodeID = id
		s.DeviceName = snap.Device.Name
		s.ID = config.SessionID(id, s.Provider, s.Profile, s.NativeID)
		s.Metadata = core.Metadata{}
		seen[s.ID] = true
		old := h.sessions[s.ID]
		ob, _ := json.Marshal(old)
		nb, _ := json.Marshal(s)
		if string(ob) == string(nb) {
			continue
		}
		h.sessions[s.ID] = s
		h.sessionObserved[s.ID] = time.Now()
		changedIDs = append(changedIDs, s.ID)
		changed = true
		_, e = h.S.DB.Exec("INSERT INTO sessions VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", s.ID, id, nb)
		if e != nil {
			log.Printf("session update: %v", e)
		}

	}
	for sid, s := range h.sessions {
		if s.NodeID == id && !seen[sid] {
			delete(h.sessions, sid)
			delete(h.sessionObserved, sid)
			changedIDs = append(changedIDs, sid)
			_, _ = h.S.DB.Exec("DELETE FROM sessions WHERE id=?", sid)
			changed = true
		}
	}
	h.mu.Unlock()
	if changed {
		var detail any
		if len(changedIDs) <= 100 {
			detail = map[string]any{"sessionIds": changedIDs}
		}
		h.Emit("snapshot", "", id, detail)
	}
	if !previousDevice.Online {
		h.Emit("device", "", id, nil)
	}

}
func attention(s string) bool {
	return s == "completed" || s == "waiting_approval" || s == "waiting_input" || s == "error" || s == "interrupted"
}
func (h *Hub) eventWS(w http.ResponseWriter, r *http.Request) {
	conn, e := (&websocket.Upgrader{CheckOrigin: h.origin}).Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1024)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, e := conn.ReadMessage(); e != nil {
				return
			}
		}
	}()
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	lastWrite := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if !h.authenticated(r) {
			return
		}
		wake := h.changes() // taken before the read so no signal is missed
		events, e := h.S.Events(after, false)
		if e != nil {
			return
		}
		for _, v := range events {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if conn.WriteJSON(v) != nil {
				return
			}
			after = v.ID
			lastWrite = time.Now()
		}
		if time.Since(lastWrite) >= 15*time.Second {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if conn.WriteJSON(map[string]string{"type": "heartbeat"}) != nil {
				return
			}
			lastWrite = time.Now()
		}
		if len(events) == 500 {
			continue
		}
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-wake:
		}
	}
}
func (h *Hub) getSession(id string) (core.Session, bool) {
	h.mu.RLock()
	s, ok := h.sessions[id]
	h.mu.RUnlock()
	return s, ok
}
func (h *Hub) Pair(code string, d core.Device) (string, error) {
	h.pairMu.Lock()
	defer h.pairMu.Unlock()
	var expires time.Time
	if e := h.S.Get("pair:"+config.Hash(code), &expires); e != nil || time.Now().After(expires) {
		return "", errors.New("pairing code invalid, expired, or already used")
	}
	if d.ID == "" || len(d.ID) > 128 || d.Name == "" || len(d.Name) > 128 {
		return "", errors.New("invalid node identity")
	}
	h.mu.RLock()
	_, exists := h.devices[d.ID]
	h.mu.RUnlock()
	if exists {
		return "", errors.New("node already registered; revoke association before pairing again")
	}
	token := config.Random()
	tx, e := h.S.DB.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	b, _ := json.Marshal(d)
	if _, e = tx.Exec("INSERT INTO devices VALUES(?,?,?,?)", d.ID, config.Hash(token), d.Name, b); e != nil {
		return "", e
	}
	if _, e = tx.Exec("DELETE FROM kv WHERE key=?", "pair:"+config.Hash(code)); e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	h.mu.Lock()
	h.devices[d.ID] = d
	h.mu.Unlock()
	return token, nil
}
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.peers {
		_ = p.conn.Close()
	}
}

// Persist native node events and their deduplication key in one transaction.
func (h *Hub) receiveEvent(node string, ev core.Event) error {
	return h.persistNodeEvent(node, "", ev)
}

func (h *Hub) persistNodeEvent(node, token string, ev core.Event) error {
	if ev.ID <= 0 || ev.NodeID != node || !attention(ev.Kind) {
		return errors.New("invalid node event")
	}
	legacyKnown := false
	if _, valid := notificationSession(ev); !valid {
		h.mu.RLock()
		s, known := h.sessions[ev.SessionID]
		legacyKnown = known && s.NodeID == node
		h.mu.RUnlock()
	}
	tx, e := h.S.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if token != "" {
		var want string
		if tx.QueryRow("SELECT token FROM devices WHERE id=?", node).Scan(&want) != nil || !equal(want, config.Hash(token)) {
			return errors.New("node credential revoked")
		}
	}
	key := fmt.Sprintf("incoming:%s:%d", node, ev.ID)
	r, e := tx.Exec("INSERT OR IGNORE INTO kv VALUES(?,?)", key, []byte("true"))
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n > 0 {
		if _, valid := notificationSession(ev); !valid {
			// Legacy nodes carry only title/provider. Keep accepting their
			// events after the session snapshot, but never acknowledge and
			// discard an event just because its snapshot has not arrived.
			if !legacyKnown {
				return errEventIdentity
			}
		}
		_, e = tx.Exec("INSERT INTO events(kind,session,node,at,data) VALUES(?,?,?,?,?)", ev.Kind, ev.SessionID, node, ev.At.Format(time.RFC3339Nano), ev.Data)
		if e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if n > 0 {
		h.signal(ev.Kind)
	}
	return nil
}
