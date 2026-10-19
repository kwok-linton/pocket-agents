package node

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/network"
	"pocketagents/internal/store"
	"pocketagents/internal/workspacefiles"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Node struct {
	history    historyPages
	C          *config.Config
	S          *store.Store
	Providers  map[string]core.Provider
	mu         sync.Mutex
	cached     map[string][]core.Session
	diagnostic []core.Diagnostic
	tailnetID  string
	last       map[string]core.Session
	persisted  time.Time
}

func New(c *config.Config, s *store.Store, providers map[string]core.Provider) *Node {
	n := &Node{C: c, S: s, Providers: providers, cached: map[string][]core.Session{}, last: map[string]core.Session{}}
	_ = s.Get("last-sessions", &n.last)
	// Persisted sessions are useful history while a provider is still starting
	// or unavailable. They are never evidence that an execution remains live.
	for _, saved := range n.last {
		key := saved.Provider + ":" + saved.Profile
		if providers[key] == nil || saved.NativeID == "" || saved.NodeID != c.NodeID {
			continue
		}
		saved.ExecutionState = "unknown"
		saved.TurnState = "unknown"
		saved.Pending = nil
		saved.Error = "Provider not yet verified after Node restart"
		saved.Capabilities = core.Capabilities{Discover: true, Reason: saved.Error}
		n.cached[key] = append(n.cached[key], saved)
	}
	return n
}
func (n *Node) Prepare(ctx context.Context) {
	tailnetID := network.Inspect(ctx).SelfID
	diagnostics := []core.Diagnostic{}
	for _, p := range n.Providers {
		diagnostics = append(diagnostics, p.Doctor(ctx))
	}
	n.mu.Lock()
	n.tailnetID, n.diagnostic = tailnetID, diagnostics
	n.mu.Unlock()
}
func (n *Node) Run(ctx context.Context) error {
	if e := config.CheckHub(n.C.HubURL); e != nil {
		return e
	}
	if n.C.NodeToken == "" {
		return errors.New("node not paired: run pocket pair")
	}
	ctx, cancel := context.WithCancel(ctx)
	prepared := make(chan struct{})
	eventsDone := make(chan struct{})
	defer func() {
		cancel()
		<-prepared
		<-eventsDone
		for _, p := range n.Providers {
			_ = p.Close()
		}
	}()
	// Connectivity must not wait for native CLI diagnostics or discovery.
	go func() {
		defer close(prepared)
		n.Prepare(ctx)
		n.scanLoop(ctx)
	}()
	go func() {
		defer close(eventsDone)
		n.forwardEvents(ctx)
	}()
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		if e := n.connect(ctx); e != nil && ctx.Err() == nil {
			log.Printf("Hub connection: %v; retrying", e)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if delay < 15*time.Second {
			delay *= 2
		}
	}
}
func (n *Node) scanLoop(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		n.scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (n *Node) scan(ctx context.Context) {
	turnsChanged := false
	for key, p := range n.Providers {
		scanctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		ss, e := p.Discover(scanctx)
		cancel()
		if e != nil {
			log.Printf("discover %s: %v", key, e)
			n.mu.Lock()
			for i := range n.cached[key] {
				s := &n.cached[key][i]
				s.ExecutionState = "unknown"
				s.TurnState = "unknown"
				s.Pending = nil
				s.Error = "Provider unavailable: " + e.Error()
				s.Capabilities = core.Capabilities{Discover: true, Reason: s.Error}
			}
			n.mu.Unlock()
			continue
		}
		n.mu.Lock()
		seen := make(map[string]bool, len(ss))
		for i := range ss {
			s := &ss[i]
			s.NodeID = n.C.NodeID
			s.DeviceName = n.C.Name
			s.ID = config.SessionID(n.C.NodeID, s.Provider, s.Profile, s.NativeID)
			seen[s.ID] = true
			old, exists := n.last[s.ID]
			if !exists || old.TurnState != s.TurnState || old.TurnID != s.TurnID {
				if err := n.S.RecordTurn(*s, old, exists, needsAttention(s.TurnState)); err != nil {
					log.Printf("node event persistence: %v", err)
					continue // retry observation; never acknowledge a lost event
				}
				turnsChanged = true
			}
			n.last[s.ID] = *s
		}
		// Sessions the provider no longer lists would otherwise return as
		// ghost "unknown" entries after every Node restart.
		for id, old := range n.last {
			if old.Provider+":"+old.Profile == key && !seen[id] {
				delete(n.last, id)
				turnsChanged = true
			}
		}
		n.cached[key] = ss
		n.mu.Unlock()
	}
	// The saved map exceeds 1 MB with a few hundred sessions. Rewriting it every
	// second only refreshes titles and times; turn changes are what restart
	// detection needs, so save those promptly and the rest once a minute.
	n.mu.Lock()
	if turnsChanged || time.Since(n.persisted) > time.Minute {
		if err := n.S.Put("last-sessions", n.last); err != nil {
			log.Printf("node observation persistence: %v", err)
		} else {
			n.persisted = time.Now()
		}
	}
	n.mu.Unlock()
}
func needsAttention(s string) bool {
	return s == "completed" || s == "waiting_approval" || s == "waiting_input" || s == "error" || s == "interrupted"
}
func (n *Node) Snapshot() core.Snapshot {
	n.mu.Lock()
	defer n.mu.Unlock()
	ss := []core.Session{}
	for _, v := range n.cached {
		ss = append(ss, v...)
	}
	// Stable order lets the sender detect an unchanged snapshot.
	sort.Slice(ss, func(i, j int) bool { return ss[i].ID < ss[j].ID })
	return core.Snapshot{Device: core.Device{ID: n.C.NodeID, Name: n.C.Name, OS: runtime.GOOS + "/" + runtime.GOARCH, Online: true, LastSeen: time.Now().UTC(), Diagnostics: n.diagnostic, Roots: n.C.Roots, TailnetID: n.tailnetID}, Sessions: ss}
}
func (n *Node) connect(ctx context.Context) error {
	u, _ := url.Parse(n.C.HubURL)
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/api/node/ws"
	head := http.Header{}
	head.Set("Authorization", "Bearer "+n.C.NodeToken)
	head.Set("X-Pocket-Node", n.C.NodeID)
	dialer := *websocket.DefaultDialer
	dialer.EnableCompression = true
	conn, _, e := dialer.DialContext(ctx, u.String(), head)
	if e != nil {
		return e
	}
	defer conn.Close()
	conn.SetReadLimit(16 << 20)
	_ = conn.SetCompressionLevel(flate.BestSpeed)
	var writeMu sync.Mutex
	write := func(v core.Wire) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		conn.EnableWriteCompression(len(v.Data) >= 1024)
		return conn.WriteJSON(v)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		// A full snapshot is about 1 MB with a few hundred sessions and the Hub
		// diffs every session in it. Send it when it changed (and periodically
		// as a safety net); otherwise a heartbeat keeps the device online.
		var sent [32]byte
		var full time.Time
		for {
			snap := n.Snapshot()
			snap.Device.LastSeen = time.Time{} // the Hub stamps its own receive time
			b, _ := json.Marshal(snap)
			msg := core.Wire{Type: "heartbeat"}
			if sum := sha256.Sum256(b); sum != sent || time.Since(full) > 30*time.Second {
				msg = core.Wire{Type: "snapshot", Data: b}
				sent, full = sum, time.Now()
			}
			if write(msg) != nil {
				_ = conn.Close()
				return
			}
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return
			case <-done:
				return
			case <-tick.C:
			}
		}
	}()
	sem := make(chan struct{}, 16)
	readCtx, stopReads := context.WithCancel(ctx)
	defer stopReads()
	var reads sync.Map
	for {
		var req core.Wire
		if e = conn.ReadJSON(&req); e != nil {
			return e
		}
		if req.Type == "cancel_read" {
			if cancel, ok := reads.Load(req.ID); ok {
				cancel.(context.CancelFunc)()
			}
			continue
		}
		select {
		case sem <- struct{}{}:
		default:
			_ = write(core.Wire{Type: "result", ID: req.ID, Error: "node busy; retry query"})
			continue
		}
		isRead := req.Type == "history" || req.Type == "history_sync"
		parent := ctx
		if isRead {
			parent = readCtx
		}
		c, cancel := context.WithTimeout(parent, 90*time.Second)
		if isRead {
			reads.Store(req.ID, cancel)
		}
		go func(req core.Wire) {
			defer func() { <-sem }()
			defer cancel()
			if isRead {
				defer reads.Delete(req.ID)
			}
			v, e := n.Handle(c, req)
			res := core.Wire{Type: "result", ID: req.ID}
			if e != nil {
				res.Error = e.Error()
			} else {
				res.Data, _ = json.Marshal(v)
			}
			if !isRead || c.Err() == nil {
				_ = write(res)
			}
		}(req)
	}
}
func (n *Node) Handle(ctx context.Context, req core.Wire) (any, error) {
	switch req.Type {
	case "history_sync":
		return n.syncHistory(ctx, req)
	case "action":
		var a core.Action
		if e := json.Unmarshal(req.Data, &a); e != nil {
			return nil, e
		}
		return n.execute(ctx, a)
	case "files_list", "files_read", "files_write":
		var v workspacefiles.Request
		if err := json.Unmarshal(req.Data, &v); err != nil {
			return nil, err
		}
		cwd, err := config.ValidatePath(n.C.Roots, v.Cwd, false)
		if err != nil {
			return nil, err
		}
		switch req.Type {
		case "files_list":
			return workspacefiles.List(cwd, v.Path)
		case "files_read":
			content, err := workspacefiles.Read(cwd, v.Path)
			return map[string]any{"content": content}, err
		default:
			return workspacefiles.Write(cwd, v.ID, v.Name, v.Content)
		}
	case "history":
		var v struct {
			Provider, Profile, NativeID, Cursor string
			Limit                               int
		}
		if e := json.Unmarshal(req.Data, &v); e != nil {
			return nil, e
		}
		p := n.Providers[v.Provider+":"+v.Profile]
		if p == nil {
			return nil, errors.New("provider profile unavailable")
		}
		return p.History(ctx, v.NativeID, v.Cursor, v.Limit)
	case "action_result":
		var v struct{ ID string }
		if e := json.Unmarshal(req.Data, &v); e != nil {
			return nil, e
		}
		result, e := n.S.Result("node:", v.ID)
		if e != nil {
			return result, e
		}
		var data struct {
			StartupID string `json:"startupId"`
		}
		_ = json.Unmarshal(result.Data, &data)
		if result.Status == "submitted" && data.StartupID != "" {
			var a core.Action
			if n.S.Get("node-action:"+v.ID, &a) == nil {
				a.Kind = "startup_status"
				a.RequestID = data.StartupID
				p := n.Providers[a.Provider+":"+a.Profile]
				if p != nil {
					next, e := p.Execute(ctx, a)
					if e == nil {
						next.ActionID = v.ID
						result = next
						_ = n.S.Finish("node:", result)
					}
				}
			}
		}
		if (result.Status == "submitted" || result.Status == "unknown") && data.StartupID == "" {
			var a core.Action
			if n.S.Get("node-action:"+v.ID, &a) == nil {
				if p, ok := n.Providers[a.Provider+":"+a.Profile].(interface {
					ReconcileResult(context.Context, core.Action, core.Result) (core.Result, error)
				}); ok {
					if next, err := p.ReconcileResult(ctx, a, result); err == nil {
						result = next
						_ = n.S.Finish("node:", result)
					}
				}
			}
		}
		return result, nil
	case "directories":
		var v struct {
			Path   string
			Create bool
		}
		if e := json.Unmarshal(req.Data, &v); e != nil {
			return nil, e
		}
		if v.Path == "" {
			return map[string]any{"roots": n.C.Roots, "path": "", "entries": []any{}}, nil
		}
		path, e := config.ValidatePath(n.C.Roots, v.Path, v.Create)
		if e != nil {
			return nil, e
		}
		entries, e := os.ReadDir(path)
		if e != nil {
			return nil, e
		}
		out := []map[string]string{}
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				out = append(out, map[string]string{"name": entry.Name(), "path": path + string(os.PathSeparator) + entry.Name()})
				if len(out) >= 500 {
					break
				}
			}
		}
		return map[string]any{"roots": n.C.Roots, "path": path, "entries": out}, nil
	default:
		return nil, errors.New("unsupported node message")
	}
}
func (n *Node) execute(ctx context.Context, a core.Action) (core.Result, error) {
	r := core.Result{ActionID: a.ID, Status: "rejected"}
	if a.NodeID != n.C.NodeID {
		return r, errors.New("wrong target node")
	}
	if len(a.ID) < 16 {
		return r, errors.New("stable action id required")
	}
	p := n.Providers[a.Provider+":"+a.Profile]
	if p == nil {
		return r, errors.New("provider profile unavailable")
	}
	if a.Kind == "startup_status" {
		return p.Execute(ctx, a)
	}
	if a.Kind == "terminal" {
		if a.NativeID == "" || (a.Provider != "codex" && a.ExecutionID == "") {
			return r, errors.New("native session and execution generation required")
		}
		return p.Execute(ctx, a)
	}
	claimed, fresh, e := n.S.Claim("node:", a)
	if e != nil {
		return r, e
	}
	if !fresh {
		return claimed, nil
	}
	finish := func(r core.Result) (core.Result, error) {
		r.ActionID = a.ID
		if e := n.S.Finish("node:", r); e != nil {
			return core.Result{ActionID: a.ID, Status: "unknown", Message: "native result received but durable acknowledgement failed"}, nil
		}
		return r, nil
	}
	switch a.Kind {
	case "startup_input":
		if a.RequestID == "" || a.ExecutionID == "" {
			r.Message = "startup id and generation required"
			return finish(r)
		}
	case "models":
	case "rename", "delete":
		if a.NativeID == "" {
			r.Message = "native session ID required"
			return finish(r)
		}
	case "create", "resume":
		path, e := config.ValidatePath(n.C.Roots, a.Cwd, false)
		if e != nil {
			r.Message = e.Error()
			return finish(r)
		}
		a.Cwd = path
	case "send", "approve", "interrupt", "terminate", "terminal", "terminal_input":
		if a.NativeID == "" || a.ExecutionID == "" {
			r.Message = "native session and execution generation required"
			return finish(r)
		}
	default:
		r.Message = "unsupported action"
		return finish(r)
	}
	if e := n.prepareFiles(&a); e != nil {
		r.Message = e.Error()
		return finish(r)
	}
	// Reconciliation must compare the exact prompt sent to the provider,
	// including attachment references, rather than the original browser draft.
	if e := n.S.Put("node-action:"+a.ID, a); e != nil {
		r.Message = e.Error()
		return finish(r)
	}
	native, e := p.Execute(ctx, a)
	if e != nil {
		if native.Status == "" {
			native.Status = "rejected"
		}
		native.Message = e.Error()
	}
	if native.Status == "" {
		native.Status = "unknown"
		native.Message = "provider did not acknowledge delivery"
	}
	return finish(native)
}
func Pair(ctx context.Context, c *config.Config, hubURL, code string) error {
	if e := config.CheckHub(hubURL); e != nil {
		return e
	}
	v := map[string]any{"code": code, "device": core.Device{ID: c.NodeID, Name: c.Name, OS: runtime.GOOS + "/" + runtime.GOARCH}}
	b, _ := json.Marshal(v)
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(hubURL, "/")+"/api/pair", bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("pairing redirects refused") }}
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	var out struct{ Token, Error string }
	if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
		return e
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("pairing failed: %s", out.Error)
	}
	if out.Token == "" {
		return errors.New("pairing returned no credential")
	}
	c.HubURL = strings.TrimRight(hubURL, "/")
	c.NodeToken = out.Token
	return c.Save()
}
