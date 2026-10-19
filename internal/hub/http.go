package hub

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/httpencoding"
	"pocketagents/internal/network"
	"sort"
	"strconv"
	"strings"
	"time"
)

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Lists and history are re-read after every change event. Let the client
// revalidate with its last ETag and compress large bodies: the full session
// list exceeds 1 MB once a few hundred native sessions are indexed.
func replyConditional(w http.ResponseWriter, r *http.Request, v any) {
	body, e := json.Marshal(v)
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	sum := sha256.Sum256(body)
	tag := `"` + hex.EncodeToString(sum[:12]) + `"`
	replyBytes(w, r, body, tag)
}
func replyVersioned(w http.ResponseWriter, r *http.Request, v any, tag string) {
	body, err := json.Marshal(v)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	replyBytes(w, r, body, tag)
}
func replyBytes(w http.ResponseWriter, r *http.Request, body []byte, tag string) {
	w.Header().Set("ETag", tag)
	w.Header().Add("Vary", "Accept-Encoding")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if len(body) < 1024 || httpencoding.Best(r.Header.Get("Accept-Encoding"), "gzip") == "" {
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	z, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	_, _ = z.Write(body)
	_ = z.Close()
}
func fail(w http.ResponseWriter, status int, s string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": s})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	if e := d.Decode(v); e != nil {
		fail(w, 400, "invalid JSON: "+e.Error())
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "one JSON object expected")
		return false
	}
	return true
}
func (h *Hub) origin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, e := url.Parse(o)
	if e != nil {
		return false
	}
	if u.Host == r.Host && u.Scheme == "http" && loopbackHost(r.Host) {
		return true
	}
	if h.C.PublicURL != "" {
		return strings.TrimRight(o, "/") == strings.TrimRight(h.C.PublicURL, "/")
	}
	return u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}
func (h *Hub) authenticated(r *http.Request) bool {
	if b := bearer(r); b != "" && equal(config.Hash(b), config.Hash(h.Owner)) {
		return true
	}
	c, e := r.Cookie("pocket_session")
	if e != nil {
		return false
	}
	var expires time.Time
	ok := h.S.Get("auth:"+config.Hash(c.Value), &expires) == nil && time.Now().Before(expires)
	if ok {
		h.touchClient(r, config.Hash(c.Value), expires)
	}
	return ok
}
func (h *Hub) Handler(static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"authenticated": h.authenticated(r)})
	})
	mux.HandleFunc("POST /api/login", h.login)
	mux.HandleFunc("POST /api/login/qr/redeem", h.redeemLoginQR)
	mux.HandleFunc("POST /api/pair", h.pair)
	mux.HandleFunc("GET /api/node/ws", h.nodeWS)
	mux.HandleFunc("POST /api/node/events", h.nodeEvents)
	api := http.NewServeMux()
	api.HandleFunc("POST /api/login/qr", h.createLoginQR)
	api.HandleFunc("POST /api/setup/link", h.setupLink)
	api.HandleFunc("GET /api/clients", h.listClients)
	api.HandleFunc("PATCH /api/clients/{id}", h.changeClient)
	api.HandleFunc("DELETE /api/clients/{id}", h.changeClient)
	api.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, e := r.Cookie("pocket_session"); e == nil {
			_, _ = h.S.DB.Exec("DELETE FROM kv WHERE key=?", "auth:"+config.Hash(c.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: "pocket_session", Value: "", MaxAge: -1, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		reply(w, map[string]bool{"ok": true})
	})
	api.HandleFunc("GET /api/devices", h.listDevices)
	api.HandleFunc("PATCH /api/devices/{id}", h.rename)
	api.HandleFunc("DELETE /api/devices/{id}", h.revoke)
	api.HandleFunc("GET /api/preferences", h.getPreferences)
	api.HandleFunc("PUT /api/preferences", h.putPreferences)
	api.HandleFunc("GET /api/sessions", h.listSessions)
	api.HandleFunc("GET /api/sessions/{id}", h.sessionDetail)
	api.HandleFunc("GET /api/sessions/{id}/history", h.history)
	api.HandleFunc("PATCH /api/sessions/{id}/metadata", h.metadata)
	api.HandleFunc("GET /api/sessions/{id}/files", h.filesList)
	api.HandleFunc("GET /api/sessions/{id}/file", h.fileRead)
	api.HandleFunc("POST /api/sessions/{id}/files", h.fileWrite)
	api.HandleFunc("POST /api/actions", h.action)
	api.HandleFunc("GET /api/actions/{id}", h.actionResult)
	api.HandleFunc("POST /api/devices/{id}/directories", h.directories)
	api.HandleFunc("POST /api/pairing", h.pairing)
	api.HandleFunc("GET /api/candidates", h.candidates)
	api.HandleFunc("GET /api/events/ws", h.eventWS)
	api.HandleFunc("GET /api/events", h.events)
	api.HandleFunc("GET /api/notifications", h.events)
	api.HandleFunc("GET /api/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"version": "0.1.0", "tailscale": network.Inspect(r.Context()), "publicUrl": h.C.PublicURL, "listen": h.C.Listen, "note": "Node online and Tailscale online are independent. Provider diagnostics are per-device."})
	})
	api.HandleFunc("GET /api/push", h.pushInfo)
	api.HandleFunc("POST /api/push/tests", h.queuePushCheck)
	api.HandleFunc("POST /api/push/subscriptions", h.subscribe)
	api.HandleFunc("DELETE /api/push/subscriptions", h.unsubscribe)
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.origin(r) {
			fail(w, 403, "origin rejected")
			return
		}
		if !h.authenticated(r) {
			fail(w, 401, "owner authentication required")
			return
		}
		api.ServeHTTP(w, r)
	}))
	mux.Handle("/", static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	})
}
func (h *Hub) login(w http.ResponseWriter, r *http.Request) {
	if !h.origin(r) {
		fail(w, 403, "origin rejected")
		return
	}
	var v struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &v) {
		return
	}
	if !equal(config.Hash(v.Token), config.Hash(h.Owner)) {
		fail(w, 401, "owner token invalid")
		return
	}
	token := config.Random()
	if e := h.S.Put("auth:"+config.Hash(token), time.Now().Add(30*24*time.Hour)); e != nil {
		fail(w, 500, e.Error())
		return
	}
	h.touchClient(r, config.Hash(token), time.Now().Add(30*24*time.Hour))
	h.sessionCookie(w, r, token)
	reply(w, map[string]bool{"authenticated": true})
}
func (h *Hub) pairing(w http.ResponseWriter, r *http.Request) {
	code := config.Random()
	expiry := time.Now().Add(10 * time.Minute)
	if e := h.S.Put("pair:"+config.Hash(code), expiry); e != nil {
		fail(w, 500, e.Error())
		return
	}
	origin := h.C.PublicURL
	if origin == "" {
		origin = "http://" + h.C.Listen
	}
	reply(w, map[string]any{"code": code, "expiresAt": expiry, "command": fmt.Sprintf("pocket pair --hub %s --code %s", quote(origin), quote(code))})
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (h *Hub) pair(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		fail(w, 403, "use pocket pair from the node terminal")
		return
	}
	var v struct {
		Code   string      `json:"code"`
		Device core.Device `json:"device"`
	}
	if !decode(w, r, &v) {
		return
	}
	token, e := h.Pair(v.Code, v.Device)
	if e != nil {
		fail(w, 403, e.Error())
		return
	}
	h.Emit("device", "", v.Device.ID, nil)
	reply(w, map[string]string{"token": token})
}
func (h *Hub) listDevices(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	list := []core.Device{}
	for _, d := range h.devices {
		d.Online = d.Online && time.Since(d.LastSeen) < 30*time.Second
		list = append(list, d)
	}
	h.mu.RUnlock()
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name || list[i].Name == list[j].Name && list[i].ID < list[j].ID
	})
	replyConditional(w, r, list)
}
func (h *Hub) listSessions(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	node := r.URL.Query().Get("node")
	provider := r.URL.Query().Get("provider")
	h.mu.RLock()
	list := []core.Session{}
	for _, s := range h.sessions {
		if node != "" && s.NodeID != node || provider != "" && s.Provider != provider {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.Title+" "+s.Cwd+" "+s.DeviceName+" "+s.Provider), q) {
			continue
		}
		list = append(list, s)
	}
	h.mu.RUnlock()
	metadata, e := h.S.AllMetadata()
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	for i := range list {
		list[i].Metadata = metadata[list[i].ID]
	}
	// Ties are common (sessions read from one scan); without a total order the
	// map iteration changes the bytes and the ETag on every request.
	sort.Slice(list, func(i, j int) bool {
		if !list[i].UpdatedAt.Equal(list[j].UpdatedAt) {
			return list[i].UpdatedAt.After(list[j].UpdatedAt)
		}
		return list[i].ID < list[j].ID
	})
	replyConditional(w, r, list)
}
func (h *Hub) rename(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &v) {
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len(v.Name) > 128 {
		fail(w, 400, "name required (max 128 bytes)")
		return
	}
	id := r.PathValue("id")
	h.mu.Lock()
	d, ok := h.devices[id]
	if ok {
		d.Name = v.Name
		h.devices[id] = d
		for sid, s := range h.sessions {
			if s.NodeID == id {
				s.DeviceName = v.Name
				h.sessions[sid] = s
			}
		}
	}
	h.mu.Unlock()
	if !ok {
		fail(w, 404, "device missing")
		return
	}
	if _, e := h.S.DB.Exec("UPDATE devices SET name=? WHERE id=?", v.Name, id); e != nil {
		fail(w, 500, e.Error())
		return
	}
	h.Emit("device", "", id, nil)
	reply(w, d)
}
func (h *Hub) revoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.mu.Lock()
	_, e := h.S.DB.Exec("DELETE FROM devices WHERE id=?", id)
	if e != nil {
		h.mu.Unlock()
		fail(w, 500, e.Error())
		return
	}
	p := h.peers[id]
	delete(h.peers, id)
	delete(h.devices, id)
	for sid, s := range h.sessions {
		if s.NodeID == id {
			delete(h.sessions, sid)
		}
	}
	_, e = h.S.DB.Exec("DELETE FROM sessions WHERE node=?", id)
	h.mu.Unlock()
	if p != nil {
		_ = p.conn.Close()
	}
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	h.Emit("device", "", id, nil)
	reply(w, map[string]bool{"ok": true})
}

func (h *Hub) history(w http.ResponseWriter, r *http.Request) {
	s, ok := h.getSession(r.PathValue("id"))
	if !ok {
		fail(w, 404, "native session no longer indexed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	started := time.Now()
	input := map[string]any{"provider": s.Provider, "profile": s.Profile, "nativeId": s.NativeID, "cursor": r.URL.Query().Get("cursor"), "limit": limit}
	typ := "history"
	if r.URL.Query().Get("sync") == "1" {
		typ = "history_sync"
		input["known"] = r.Header.Get("If-None-Match")
	}
	res, e := h.RPC(ctx, s.NodeID, typ, input)
	// Rolling upgrades keep old execution devices usable.
	if typ == "history_sync" && e != nil && res.Error == "unsupported node message" {
		res, e = h.RPC(ctx, s.NodeID, "history", input)
		typ = "history"
	}
	w.Header().Set("Server-Timing", fmt.Sprintf("node;dur=%.1f", float64(time.Since(started).Microseconds())/1000))
	if e != nil {
		if r.Context().Err() == nil {
			fail(w, 502, e.Error())
		}
		return
	}
	if typ == "history_sync" {
		var page core.HistorySync
		if json.Unmarshal(res.Data, &page) != nil {
			fail(w, 502, "invalid JSON: history response")
			return
		}
		w.Header().Set("ETag", page.Version)
		if page.Unchanged {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if page.Base != "" {
			w.Header().Set("X-Pocket-History-Delta", "1")
			replyVersioned(w, r, page, page.Version)
		} else {
			replyVersioned(w, r, page.History, page.Version)
		}
		return
	}
	replyConditional(w, r, res.Data)
}
func (h *Hub) metadata(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.getSession(id); !ok {
		fail(w, 404, "session missing")
		return
	}
	m := h.S.Metadata(id)
	var patch map[string]json.RawMessage
	if !decode(w, r, &patch) {
		return
	}
	b, _ := json.Marshal(m)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(b, &merged)
	for k, v := range patch {
		switch k {
		case "alias", "favorite", "done", "hidden", "readAt":
			merged[k] = v
		default:
			fail(w, 400, "unknown metadata field")
			return
		}
	}
	b, _ = json.Marshal(merged)
	if e := json.Unmarshal(b, &m); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if len(m.Alias) > 256 {
		fail(w, 400, "alias too long")
		return
	}
	if e := h.S.SetMetadata(id, m); e != nil {
		fail(w, 500, e.Error())
		return
	}
	h.Emit("metadata", id, "", nil)
	reply(w, m)
}
func (h *Hub) action(w http.ResponseWriter, r *http.Request) {
	var a core.Action
	if !decode(w, r, &a) {
		return
	}
	if len(a.ID) < 16 || len(a.ID) > 128 || a.NodeID == "" || a.Profile == "" {
		fail(w, 400, "stable action id, node and profile required")
		return
	}
	switch a.Kind {
	case "models", "delete", "rename", "create", "resume", "send", "approve", "interrupt", "terminate", "terminal", "terminal_input", "startup_status", "startup_input":
	default:
		fail(w, 400, "unsupported action")
		return
	}
	if a.Provider != "claude" && a.Provider != "codex" {
		fail(w, 400, "unknown provider")
		return
	}
	// Return a durable prior acknowledgement even if its execution has since exited.
	var prior core.Action
	if a.Kind != "terminal" && h.S.Get("action:"+a.ID, &prior) == nil {
		normalized := a
		if normalized.Cwd == "" {
			normalized.Cwd = prior.Cwd
		}
		oldBytes, _ := json.Marshal(prior)
		newBytes, _ := json.Marshal(normalized)
		if string(oldBytes) != string(newBytes) {
			fail(w, 409, "action ID was already used with a different payload")
			return
		}
		result, e := h.S.Result("hub:", a.ID)
		if e != nil {
			fail(w, 500, e.Error())
			return
		}
		reply(w, result)
		return
	}
	h.mu.RLock()
	d, exists := h.devices[a.NodeID]
	h.mu.RUnlock()
	if !exists {
		fail(w, 404, "node missing or revoked")
		return
	}
	if a.Kind != "models" && a.Kind != "create" && a.Kind != "startup_input" && a.Kind != "startup_status" {
		sid := config.SessionID(a.NodeID, a.Provider, a.Profile, a.NativeID)
		s, ok := h.getSession(sid)
		if !ok {
			fail(w, 404, "native session no longer indexed")
			return
		}
		if a.Cwd != "" && a.Cwd != s.Cwd {
			fail(w, 409, "working directory mismatch")
			return
		}
		a.Cwd = s.Cwd
		if a.Kind != "resume" && a.Kind != "rename" && a.Kind != "delete" && a.Kind != "terminal" && (a.ExecutionID == "" || a.ExecutionID != s.ExecutionID) {
			fail(w, 409, "execution changed; refresh before acting")
			return
		}
	}
	if a.Kind == "terminal" || a.Kind == "startup_status" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		wire, e := h.RPC(ctx, a.NodeID, "action", a)
		if e != nil {
			fail(w, 502, e.Error())
			return
		}
		reply(w, wire.Data)
		return
	}
	result, fresh, e := h.S.Claim("hub:", a)
	if e != nil {
		fail(w, 409, e.Error())
		return
	}
	if !fresh {
		reply(w, result)
		return
	}
	if e = h.S.Put("action:"+a.ID, a); e != nil {
		result.Status = "rejected"
		result.Message = e.Error()
		_ = h.S.Finish("hub:", result)
		reply(w, result)
		return
	}
	if !d.Online || time.Since(d.LastSeen) >= 30*time.Second {
		result.Status = "rejected"
		result.Message = "node offline; no action delivered"
		_ = h.S.Finish("hub:", result)
		reply(w, result)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	wire, e := h.RPC(ctx, a.NodeID, "action", a)
	if e != nil {
		result.Status = "unknown"
		result.Message = e.Error()
	} else if e = json.Unmarshal(wire.Data, &result); e != nil {
		result.Status = "unknown"
		result.Message = "invalid node acknowledgement"
	}
	result.ActionID = a.ID
	_ = h.S.Finish("hub:", result)
	h.Emit("action", "", a.NodeID, result)
	reply(w, result)
}
func (h *Hub) actionResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, e := h.S.Result("hub:", id)
	if e != nil {
		fail(w, 404, "action unknown")
		return
	}
	if result.Status == "submitted" || result.Status == "unknown" {
		var a core.Action
		if h.S.Get("action:"+id, &a) == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			wire, e := h.RPC(ctx, a.NodeID, "action_result", map[string]string{"id": id})
			if e == nil {
				var nodeResult core.Result
				if json.Unmarshal(wire.Data, &nodeResult) == nil && nodeResult.ActionID == id {
					result = nodeResult
					_ = h.S.Finish("hub:", result)
				}
			}
		}
	}
	reply(w, result)
}
func (h *Hub) directories(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Path   string `json:"path"`
		Create bool   `json:"create"`
	}
	if !decode(w, r, &v) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, e := h.RPC(ctx, r.PathValue("id"), "directories", v)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	reply(w, res.Data)
}
func (h *Hub) candidates(w http.ResponseWriter, r *http.Request) {
	s := network.Inspect(r.Context())
	h.mu.RLock()
	for i, p := range s.Peers {
		for _, d := range h.devices {
			if p.ID == d.TailnetID {
				s.Peers[i].Registered = true
			}
		}
	}
	h.mu.RUnlock()
	sort.Slice(s.Peers, func(i, j int) bool { return s.Peers[i].Name < s.Peers[j].Name })
	reply(w, s)
}
func (h *Hub) events(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	var v []core.Event
	var e error
	if strings.HasSuffix(r.URL.Path, "notifications") && after == 0 {
		v, e = h.S.Inbox(200)
	} else {
		v, e = h.S.Events(after, strings.HasSuffix(r.URL.Path, "notifications"))
	}
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	replyConditional(w, r, v)
}
