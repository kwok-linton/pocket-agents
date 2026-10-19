// Package codexnative controls the user's official shared Codex app-server.
// It never starts a second inference process or writes native history files.
package codexnative

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"pocketagents/internal/core"
)

type nativeThread struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Preview  string `json:"preview"`
	Cwd      string `json:"cwd"`
	Updated  int64  `json:"updatedAt"`
	Parent   string `json:"parentThreadId"`
	CanInput *bool  `json:"canAcceptDirectInput"`
	Status   struct {
		Type  string   `json:"type"`
		Flags []string `json:"activeFlags"`
	} `json:"status"`
	Turns []nativeTurn `json:"turns"`
}
type nativeTurn struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Started   int64             `json:"startedAt"`
	Completed int64             `json:"completedAt"`
	Items     []json.RawMessage `json:"items"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}
type pendingRequest struct {
	Approval  core.Approval
	RPCID     json.RawMessage
	Params    map[string]json.RawMessage
	TurnID    string
	Responded bool
	Resolved  chan struct{}
}
type Provider struct {
	profile    core.Profile
	socket     string
	connect    sync.Mutex
	actions    sync.Mutex
	mu         sync.Mutex
	rpc        *rpcClient
	epoch      string
	sessions   map[string]core.Session
	pending    map[string]*pendingRequest
	subscribed map[string]bool
	lastRepair time.Time
	listed     map[string]nativeThread // last thread listing, see threads
	listedAt   time.Time
}

func New(profile core.Profile) (core.Provider, error) {
	if profile.Name == "" {
		profile.Name = "default"
	}
	if profile.Binary == "" {
		profile.Binary = "codex"
	}
	if profile.Home == "" {
		h, e := os.UserHomeDir()
		if e != nil {
			return nil, e
		}
		profile.Home = filepath.Join(h, ".codex")
	}
	abs, e := filepath.Abs(profile.Home)
	if e != nil {
		return nil, e
	}
	profile.Home = abs
	socket := strings.TrimPrefix(profile.Endpoint, "unix://")
	if socket == "" {
		socket = filepath.Join(profile.Home, "app-server-control", "app-server-control.sock")
	}
	if !filepath.IsAbs(socket) {
		return nil, errors.New("Codex endpoint must be an absolute local Unix socket path")
	}
	return &Provider{profile: profile, socket: socket, sessions: map[string]core.Session{}, pending: map[string]*pendingRequest{}, subscribed: map[string]bool{}}, nil
}
func (p *Provider) client(ctx context.Context) (*rpcClient, error) {
	p.connect.Lock()
	defer p.connect.Unlock()
	p.mu.Lock()
	r := p.rpc
	p.mu.Unlock()
	if r != nil {
		select {
		case <-r.done:
		default:
			return r, nil
		}
	}
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		return nil, e
	}
	p.mu.Lock()
	p.epoch = hex.EncodeToString(b)
	p.pending = map[string]*pendingRequest{}
	p.subscribed = map[string]bool{}
	p.mu.Unlock()
	c, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	r, e := dialRPC(c, p.socket, p.event)
	if e != nil {
		return nil, e
	}
	p.mu.Lock()
	p.rpc = r
	p.mu.Unlock()
	return r, nil
}
func (p *Provider) Close() error {
	p.connect.Lock()
	defer p.connect.Unlock()
	p.mu.Lock()
	r := p.rpc
	p.rpc = nil
	p.mu.Unlock()
	if r != nil {
		r.close()
	}
	return nil
}
func baseCaps() core.Capabilities {
	return core.Capabilities{Discover: true, Create: true, Resume: true, Attach: true, Send: true, Approve: true, Interrupt: true, Terminal: true, Persistent: true}
}
func (p *Provider) execution(id string) string {
	// Native ownership removes the lock on release. Both inode lifetimes stay
	// unchanged when Pocket reconnects and change when native ownership restarts.
	socket, e1 := os.Stat(p.socket)
	lock, e2 := os.Stat(filepath.Join(p.profile.Home, "thread-writer-locks", id+".lock"))
	if e1 != nil || e2 != nil {
		return ""
	}
	a, ok1 := socket.Sys().(*syscall.Stat_t)
	b, ok2 := lock.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%d:%d:%d:%d", id, a.Dev, a.Ino, socket.ModTime().UnixNano(), b.Dev, b.Ino, lock.ModTime().UnixNano())))
	return "codex:" + hex.EncodeToString(sum[:16])
}
func (p *Provider) command(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, p.profile.Binary, args...)
	c.Env = append(os.Environ(), p.profile.Env...)
	c.Env = append(c.Env, "CODEX_HOME="+p.profile.Home)
	return c
}
func (p *Provider) Doctor(ctx context.Context) core.Diagnostic {
	d := core.Diagnostic{Provider: "codex", Profile: p.profile.Name, Capabilities: baseCaps(), LoggedIn: "unknown"}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	v, e := p.command(c, "--version").CombinedOutput()
	d.Version = strings.TrimSpace(string(v))
	if e != nil {
		d.Details = "Install official Codex CLI and ensure its binary is in the service PATH: " + e.Error()
		return d
	}
	login, e := p.command(c, "login", "status").CombinedOutput()
	if e == nil {
		d.LoggedIn = strings.TrimSpace(string(login))
	} else {
		d.LoggedIn = "not logged in; run codex login"
	}
	_, e = p.client(c)
	if e != nil {
		d.Details = e.Error()
		return d
	}
	d.Available = true
	dv, _ := p.command(c, "app-server", "daemon", "version").CombinedOutput()
	d.Details = "Shared local native daemon. Endpoint: " + p.socket + ". " + strings.TrimSpace(string(dv)) + ". Execution IDs combine native daemon and writer-lock inode lifetimes; reconnect invalidates pending approvals without stopping native work."
	return d
}

// writerHeld reads the native advisory lock without creating or changing it.
// The native daemon also enforces this lock when resuming; this check gives a
// useful read-only capability reason before the user attempts an action.
func (p *Provider) writerHeld(id string) (bool, error) {
	if !validID(id) {
		return false, errors.New("invalid native thread ID")
	}
	f, e := os.Open(filepath.Join(p.profile.Home, "thread-writer-locks", id+".lock"))
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer f.Close()
	e = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if e == syscall.EWOULDBLOCK || e == syscall.EAGAIN {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

// Shared-daemon discovery cannot inspect the turn of a Desktop/private writer.
// An exclusive native lock still proves a live owner, without authorizing a
// second writer or inventing that owner's execution generation.
func (p *Provider) externalWriterState(s core.Session) core.Session {
	held, err := p.writerHeld(s.NativeID)
	if !held && err == nil {
		return s
	}
	s.ExecutionState = "unknown"
	if held && err == nil {
		s.ExecutionState = "alive"
	}
	s.ExecutionID = ""
	s.TurnState = "unknown"
	s.Pending = nil
	s.Capabilities = core.Capabilities{
		Discover:   true,
		Persistent: true,
		Reason:     "A live native writer outside the shared daemon owns this thread; its current turn is unknown and control is read-only. Return to its original app or terminal.",
	}
	if err != nil {
		s.Capabilities.Reason = "Cannot verify the native writer; control is read-only: " + err.Error()
	}
	return s
}
func validID(s string) bool {
	if len(s) < 8 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
func turnState(s string) string {
	switch s {
	case "inProgress":
		return "working"
	case "completed":
		return "completed"
	case "interrupted":
		return "interrupted"
	case "failed":
		return "error"
	}
	return "unknown"
}
func (p *Provider) merge(t nativeThread, loaded bool) core.Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.sessions[t.ID]
	s := core.Session{Provider: "codex", Profile: p.profile.Name, NativeID: t.ID, Cwd: t.Cwd, Title: t.Name, UpdatedAt: time.Unix(t.Updated, 0), ParentID: t.Parent, ExecutionState: "exited", TurnState: old.TurnState, TurnID: old.TurnID, Error: old.Error, Capabilities: baseCaps()}
	if s.Title == "" {
		s.Title = t.Preview
	}
	if s.Title == "" {
		s.Title = "Codex session"
	}
	rr := []rune(s.Title)
	if len(rr) > 120 {
		s.Title = string(rr[:120]) + "…"
	}
	if s.TurnState == "" {
		s.TurnState = "unknown"
	}
	if loaded {
		s.ExecutionState = "alive"
		s.ExecutionID = p.execution(t.ID)
		if s.ExecutionID == "" {
			s.ExecutionState = "unknown"
			s.Capabilities.Send = false
			s.Capabilities.Interrupt = false
			s.Capabilities.Approve = false
			s.Capabilities.Reason = "Cannot verify native writer generation"
		}
		s.Capabilities.Resume = false
		if t.CanInput != nil && !*t.CanInput {
			s.Capabilities.Send = false
			s.Capabilities.Reason = "Native sub-agent does not accept direct input"
		}
		switch t.Status.Type {
		case "active":
			s.TurnState = "working"
			for _, f := range t.Status.Flags {
				if f == "waitingOnApproval" {
					s.TurnState = "waiting_approval"
				}
				if f == "waitingOnUserInput" {
					s.TurnState = "waiting_input"
				}
			}
		case "systemError":
			s.TurnState = "error"
		}
		s.Pending = p.approvalsLocked(t.ID)
	} else {
		s.Capabilities.Attach = false
		s.Capabilities.Send = false
		s.Capabilities.Approve = false
		s.Capabilities.Interrupt = false
	}
	if len(t.Turns) > 0 {
		last := t.Turns[len(t.Turns)-1]
		s.TurnID = last.ID
		s.TurnState = turnState(last.Status)
		if last.Error != nil {
			s.Error = last.Error.Message
		}
	}
	p.sessions[t.ID] = s
	return s
}
func (p *Provider) approvalsLocked(id string) []core.Approval {
	out := []core.Approval{}
	for _, r := range p.pending {
		if r.Approval.NativeID == id && !r.Responded {
			out = append(out, r.Approval)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (p *Provider) read(ctx context.Context, r *rpcClient, id string) (nativeThread, error) {
	var out struct {
		Thread nativeThread `json:"thread"`
	}
	e := r.call(ctx, "thread/read", map[string]any{"threadId": id}, &out)
	return out.Thread, e
}
func (p *Provider) loaded(ctx context.Context, r *rpcClient) (map[string]bool, error) {
	out := map[string]bool{}
	cursor := ""
	for {
		var page struct {
			Data []string `json:"data"`
			Next string   `json:"nextCursor"`
		}
		a := map[string]any{"limit": 200}
		if cursor != "" {
			a["cursor"] = cursor
		}
		if e := r.call(ctx, "thread/loaded/list", a, &page); e != nil {
			return nil, e
		}
		for _, id := range page.Data {
			out[id] = true
		}
		if page.Next == "" {
			return out, nil
		}
		cursor = page.Next
	}
}

type threadPage struct {
	Data []nativeThread `json:"data"`
	Next string         `json:"nextCursor"`
}

// Listing every thread each second costs several RPC pages (over a thousand
// threads in practice) for a handful of changes. Between full listings only
// the first page, sorted by update time, is read and merged into the previous
// listing. A full listing every 30 seconds catches deletions; if the whole
// first page changed, later pages may have too, so list everything at once.
const fullListing = 30 * time.Second

func (p *Provider) threads(fetch func(cursor string, repair bool) (threadPage, error)) (list []nativeThread, fresh map[string]bool, e error) {
	p.mu.Lock()
	cached, at := p.listed, p.listedAt
	repair := time.Since(p.lastRepair) > time.Minute
	p.mu.Unlock()
	fresh = map[string]bool{}
	all := map[string]nativeThread{}
	if cached != nil && !repair && time.Since(at) < fullListing {
		first, e := fetch("", false)
		if e != nil {
			return nil, nil, e
		}
		changed := 0
		for _, t := range first.Data {
			if old, ok := cached[t.ID]; !ok || !reflect.DeepEqual(old, t) {
				changed++
			}
		}
		if first.Next != "" && len(first.Data) > 0 && changed < len(first.Data) {
			for id, t := range cached {
				all[id] = t
			}
			for _, t := range first.Data {
				all[t.ID], fresh[t.ID] = t, true
			}
			p.mu.Lock()
			p.listed = all
			p.mu.Unlock()
			return sortedThreads(all), fresh, nil
		}
		if first.Next == "" {
			// The first page was the whole listing.
			for _, t := range first.Data {
				all[t.ID], fresh[t.ID] = t, true
			}
			p.mu.Lock()
			p.listed, p.listedAt = all, time.Now()
			p.mu.Unlock()
			return sortedThreads(all), fresh, nil
		}
	}
	cursor := ""
	for {
		page, e := fetch(cursor, repair && cursor == "")
		if e != nil {
			return nil, nil, e
		}
		for _, t := range page.Data {
			all[t.ID], fresh[t.ID] = t, true
		}
		if page.Next == "" {
			break
		}
		if page.Next == cursor {
			return nil, nil, errors.New("native pagination cursor did not advance")
		}
		cursor = page.Next
	}
	p.mu.Lock()
	p.listed, p.listedAt = all, time.Now()
	if repair {
		p.lastRepair = time.Now()
	}
	p.mu.Unlock()
	return sortedThreads(all), fresh, nil
}
func sortedThreads(m map[string]nativeThread) []nativeThread {
	out := make([]nativeThread, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Updated != out[j].Updated {
			return out[i].Updated > out[j].Updated
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Thread IDs that currently have a native writer lock file. Checking the
// directory once replaces an open+flock attempt for every unloaded thread.
func (p *Provider) lockedThreads() (map[string]bool, bool) {
	entries, e := os.ReadDir(filepath.Join(p.profile.Home, "thread-writer-locks"))
	if os.IsNotExist(e) {
		return map[string]bool{}, true
	}
	if e != nil {
		return nil, false
	}
	out := map[string]bool{}
	for _, entry := range entries {
		if id, ok := strings.CutSuffix(entry.Name(), ".lock"); ok {
			out[id] = true
		}
	}
	return out, true
}
func (p *Provider) Discover(ctx context.Context) ([]core.Session, error) {
	r, e := p.client(ctx)
	if e != nil {
		return nil, e
	}
	loaded, e := p.loaded(ctx, r)
	if e != nil {
		return nil, e
	}
	threads, fresh, e := p.threads(func(cursor string, repair bool) (threadPage, error) {
		var page threadPage
		a := map[string]any{"limit": 200, "sortKey": "updated_at", "useStateDbOnly": !repair, "sourceKinds": []string{"cli", "vscode", "exec", "appServer", "unknown"}}
		if cursor != "" {
			a["cursor"] = cursor
		}
		return page, r.call(ctx, "thread/list", a, &page)
	})
	if e != nil {
		return nil, e
	}
	locks, locksKnown := p.lockedThreads()
	var out []core.Session
	seen := map[string]bool{}
	for _, t := range threads {
		seen[t.ID] = true
		if loaded[t.ID] && !fresh[t.ID] {
			// Live threads must show approvals and waits promptly; a cached
			// listing entry may be up to one full-listing interval old.
			if current, err := p.read(ctx, r, t.ID); err == nil {
				t = current
			}
		}
		s := p.merge(t, loaded[t.ID])
		if !loaded[t.ID] && (!locksKnown || locks[t.ID]) {
			s = p.externalWriterState(s)
			s = p.offerExternalQueue(ctx, r, s)
		}
		out = append(out, s)
	}
	for id := range loaded {
		if !seen[id] {
			if t, err := p.read(ctx, r, id); err == nil {
				out = append(out, p.merge(t, true))
			}
		}
	}
	// Read reliable last-turn states for loaded sessions, including ones created
	// from a separate native TUI. Idle alone never means a completed turn.
	for i := range out {
		if !loaded[out[i].NativeID] {
			continue
		}
		var page struct {
			Data []nativeTurn `json:"data"`
		}
		if r.call(ctx, "thread/turns/list", map[string]any{"threadId": out[i].NativeID, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"}, &page) == nil && len(page.Data) > 0 {
			last := page.Data[0]
			out[i].TurnID = last.ID
			if out[i].TurnState != "waiting_approval" && out[i].TurnState != "waiting_input" {
				out[i].TurnState = turnState(last.Status)
			}
			if last.Error != nil {
				out[i].Error = last.Error.Message
			}
		}
	}
	// Only reads are automatic. A live thread is subscribed through explicit attach,
	// send or opening History; no discovery step resumes an unloaded thread.
	p.mu.Lock()
	for id := range p.sessions {
		if !seen[id] && !loaded[id] {
			delete(p.sessions, id)
		}
	}
	for _, s := range out {
		p.sessions[s.NativeID] = s
	}
	p.mu.Unlock()
	return out, nil
}
func cleanCwd(path string) (string, error) {
	if path == "" {
		return "", errors.New("working directory is required")
	}
	a, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	a, e = filepath.EvalSymlinks(a)
	if e != nil {
		return "", fmt.Errorf("working directory is unavailable: %w", e)
	}
	st, e := os.Stat(a)
	if e != nil || !st.IsDir() {
		return "", errors.New("working directory must exist and be a directory")
	}
	return a, nil
}
func (p *Provider) join(ctx context.Context, r *rpcClient, id string, allowResume bool) (nativeThread, error) {
	if !validID(id) {
		return nativeThread{}, errors.New("invalid native thread ID")
	}
	loaded, e := p.loaded(ctx, r)
	if e != nil {
		return nativeThread{}, e
	}
	if !loaded[id] {
		if !allowResume {
			return nativeThread{}, errors.New("thread is no longer loaded; explicitly resume it before sending")
		}
		held, e := p.writerHeld(id)
		if e != nil {
			return nativeThread{}, e
		}
		if held {
			return nativeThread{}, errors.New("native thread has a live writer outside this shared daemon; refusing a second writer")
		}
	}
	t, e := p.read(ctx, r, id)
	if e != nil {
		return t, e
	}
	if _, e = cleanCwd(t.Cwd); e != nil {
		return t, e
	}
	var out struct {
		Thread nativeThread `json:"thread"`
	}
	if e = r.call(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &out); e != nil {
		return t, e
	}
	p.mu.Lock()
	p.subscribed[id] = true
	p.mu.Unlock()
	p.merge(out.Thread, true)
	return out.Thread, nil
}
func (p *Provider) Execute(ctx context.Context, a core.Action) (core.Result, error) {
	p.actions.Lock()
	defer p.actions.Unlock()
	// Create, rename and delete must show up on the next scan, not after the
	// incremental listing's next full refresh.
	defer func() {
		p.mu.Lock()
		p.listedAt = time.Time{}
		p.mu.Unlock()
	}()
	res := core.Result{ActionID: a.ID, NativeID: a.NativeID, Status: "rejected"}
	if a.Kind == "delete" {
		return p.deleteHistory(ctx, a)
	}
	r, e := p.client(ctx)
	if e != nil {
		return res, e
	}
	if a.Kind == "models" {
		return p.modelOptions(ctx, r, a)
	}
	if a.Model != "" || a.Effort != "" {
		if err := p.validateModel(ctx, r, a); err != nil {
			return res, err
		}
	}
	if a.Kind == "rename" {
		name := strings.TrimSpace(a.Text)
		if !validID(a.NativeID) || name == "" || len([]rune(name)) > 120 || strings.ContainsAny(name, "\r\n\x00") {
			return res, errors.New("valid native ID and single-line name (1–120 characters) required")
		}
		// Metadata operation only: do not resume, send a turn, or acquire a writer.
		if _, err := p.read(ctx, r, a.NativeID); err != nil {
			return res, err
		}
		if err := r.call(ctx, "thread/name/set", map[string]string{"threadId": a.NativeID, "name": name}, nil); err != nil {
			return delivery(res, err)
		}
		thread, err := p.read(ctx, r, a.NativeID)
		if err != nil || thread.Name != name {
			res.Status = "unknown"
			res.Message = "native rename requested but name could not be verified; refresh before retrying"
			return res, nil
		}
		res.Status = "accepted"
		res.Data, _ = json.Marshal(map[string]string{"name": thread.Name})
		return res, nil
	}
	if a.Kind == "send" && strings.HasPrefix(a.ExecutionID, "codex-queue:") {
		if strings.TrimSpace(a.Text) == "" {
			return res, errors.New("message is empty")
		}
		return p.sendExternalQueue(ctx, r, a)
	}
	if a.Kind == "terminal" {
		if !validID(a.NativeID) {
			return res, errors.New("invalid native thread ID")
		}
		cmd := "CODEX_HOME=" + shellQuote(p.profile.Home) + " " + shellQuote(p.profile.Binary) + " --remote " + shellQuote("unix://"+p.socket) + " resume " + shellQuote(a.NativeID)
		res.Status = "accepted"
		res.Data, _ = json.Marshal(map[string]string{"command": cmd, "description": "Connect the official Codex TUI to this same shared native daemon. External desktop-owned sessions must be opened in their original app."})
		return res, nil
	}
	if a.Kind == "terminate" {
		return res, errors.New("ending a shared daemon execution is not supported safely; use interrupt to stop only the current turn, or the native terminal to manage lifecycle")
	}
	if a.Kind == "create" {
		cwd, e := cleanCwd(a.Cwd)
		if e != nil {
			return res, e
		}
		var out struct {
			Thread nativeThread `json:"thread"`
		}
		if e = r.call(ctx, "thread/start", newThreadParams(cwd, p.profile.CodexCreateWithoutApproval || p.profile.CodexWithoutApproval), &out); e != nil {
			return delivery(res, e)
		}
		a.NativeID = out.Thread.ID
		res.NativeID = a.NativeID
		p.mu.Lock()
		p.subscribed[a.NativeID] = true
		p.mu.Unlock()
		p.merge(out.Thread, true)
		if strings.TrimSpace(a.Text) == "" {
			res.Status = "accepted"
			res.ExecutionID = p.execution(a.NativeID)
			return res, nil
		}
	} else if a.Kind == "resume" || a.Kind == "attach" {
		t, e := p.join(ctx, r, a.NativeID, a.Kind == "resume")
		if e != nil {
			return delivery(res, e)
		}
		res.NativeID = t.ID
		res.Status = "accepted"
		res.ExecutionID = p.execution(t.ID)
		if a.Kind != "resume" || (strings.TrimSpace(a.Text) == "" && len(a.Files) == 0) {
			return res, nil
		}
		res.Status = "rejected"
		// Resume and submit the first message as one durable action. Never drop
		// the user's text after successfully loading the saved native thread.
	} else {
		expected := p.execution(a.NativeID)
		if a.ExecutionID == "" || a.ExecutionID != expected {
			return res, errors.New("execution generation changed; refresh before acting")
		}
		if a.Kind == "approve" {
			return p.approve(ctx, r, a, res)
		}
		if _, e = p.join(ctx, r, a.NativeID, false); e != nil {
			return delivery(res, e)
		}
	}
	res.ExecutionID = p.execution(a.NativeID)
	switch a.Kind {
	case "create", "resume", "send":
		if strings.TrimSpace(a.Text) == "" {
			return res, errors.New("message is empty")
		}
		input := nativeInput(a)
		var out struct {
			Turn nativeTurn `json:"turn"`
		}
		params := map[string]any{"threadId": a.NativeID, "input": input}
		applyTurnPermissionPolicy(params, p.profile.CodexWithoutApproval)
		if a.Model != "" {
			params["model"] = a.Model
		}
		if a.Effort != "" {
			params["effort"] = a.Effort
		}
		if a.ID != "" {
			params["clientUserMessageId"] = a.ID
		}
		if e = r.call(ctx, "turn/start", params, &out); e != nil {
			return delivery(res, e)
		}
		p.mu.Lock()
		s := p.sessions[a.NativeID]
		s.TurnID = out.Turn.ID
		s.TurnState = turnState(out.Turn.Status)
		s.UpdatedAt = time.Now()
		p.sessions[a.NativeID] = s
		p.mu.Unlock()
		res.Status = "accepted"
		res.Data, _ = json.Marshal(map[string]string{"turnId": out.Turn.ID})
		return res, nil
	case "interrupt":
		var page struct {
			Data []nativeTurn `json:"data"`
		}
		if e = r.call(ctx, "thread/turns/list", map[string]any{"threadId": a.NativeID, "limit": 1, "sortDirection": "desc"}, &page); e != nil {
			return delivery(res, e)
		}
		if len(page.Data) == 0 || page.Data[0].Status != "inProgress" {
			return res, errors.New("there is no current active turn to interrupt")
		}
		if e = r.call(ctx, "turn/interrupt", map[string]string{"threadId": a.NativeID, "turnId": page.Data[0].ID}, nil); e != nil {
			return delivery(res, e)
		}
		res.Status = "accepted"
		return res, nil
	default:
		return res, fmt.Errorf("unsupported Codex action %q", a.Kind)
	}
}
func delivery(res core.Result, e error) (core.Result, error) {
	if strings.Contains(e.Error(), "delivery unknown") {
		res.Status = "unknown"
		res.Message = e.Error()
		return res, nil
	}
	return res, e
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Explicit per-profile opt-in applies only when creating a new thread.
func newThreadParams(cwd string, withoutApproval bool) map[string]any {
	params := map[string]any{"cwd": cwd, "ephemeral": false}
	if withoutApproval {
		params["approvalPolicy"] = "never"
		params["sandbox"] = "danger-full-access"
	}
	return params
}

func applyTurnPermissionPolicy(params map[string]any, enabled bool) {
	if enabled {
		params["approvalPolicy"] = "never"
		params["sandboxPolicy"] = map[string]any{"type": "dangerFullAccess"}
	}
}
