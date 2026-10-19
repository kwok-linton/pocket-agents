// Package claudenative uses the installed Claude Code CLI and its native
// background supervisor. It never implements or replaces the agent runtime.
package claudenative

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pocketagents/internal/core"

	"github.com/creack/pty"
	"github.com/google/uuid"
)

type Provider struct {
	receiptMu     sync.Mutex
	receiptMisses map[string]string
	startups      map[string]*startup
	profile       core.Profile
	mu            sync.Mutex // serializes the local controller, including writes and attach
	cache         map[string]cachedSession
	attachments   map[string]*attachment
	consumed      map[string]bool
	apiErrors     map[string]apiErrorCheck
}

// Result of reading a blocked+idle session's latest reply, valid while the
// transcript's update time is unchanged.
type apiErrorCheck struct {
	updated time.Time
	failed  bool
}
type cachedSession struct {
	size    int64
	mod     time.Time
	session core.Session
}
type agent struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
	PID        int    `json:"pid"`
	StartedAt  int64  `json:"startedAt"`
}
type attachment struct {
	exitInput  exitInput
	terminalID string
	input      *os.File
	cmd        *exec.Cmd
	file       *os.File
	mu         sync.Mutex
	output     []byte
	offset     int64
	dead       bool
	ready      chan struct{}
	once       sync.Once
	size       pty.Winsize
}

func New(profile core.Profile) (core.Provider, error) {
	if profile.Name == "" {
		profile.Name = "default"
	}
	profile.Provider = "claude"
	if profile.Home == "" {
		profile.Home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if profile.Home == "" {
		h, e := os.UserHomeDir()
		if e != nil {
			return nil, e
		}
		profile.Home = filepath.Join(h, ".claude")
	}
	var e error
	profile.Home, e = filepath.Abs(profile.Home)
	if e != nil {
		return nil, e
	}
	if profile.Binary == "" {
		profile.Binary = "claude"
	}
	if path, e := exec.LookPath(profile.Binary); e == nil {
		profile.Binary, _ = filepath.Abs(path)
	}
	return &Provider{profile: profile, cache: map[string]cachedSession{}, attachments: map[string]*attachment{}, consumed: map[string]bool{}}, nil
}
func (p *Provider) command(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, p.profile.Binary, args...)
	c.Env = append(os.Environ(), p.profile.Env...)
	// Setting CLAUDE_CONFIG_DIR to ~/.claude changes the location of the
	// account/trust file from ~/.claude.json to ~/.claude/.claude.json. Preserve
	// the user's unset default rather than silently selecting a new profile.
	h, _ := os.UserHomeDir()
	if p.profile.Home != filepath.Join(h, ".claude") || os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		c.Env = append(c.Env, "CLAUDE_CONFIG_DIR="+p.profile.Home)
	}
	return c
}
func (p *Provider) agents(ctx context.Context) ([]agent, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	b, e := p.command(ctx, "agents", "--json", "--all").Output()
	if e != nil {
		return nil, fmt.Errorf("Claude agents --json: %w", e)
	}
	var a []agent
	if e = json.Unmarshal(b, &a); e != nil {
		return nil, fmt.Errorf("Claude agents JSON: %w (requires supported background CLI)", e)
	}
	return a, nil
}
func baseCapabilities() core.Capabilities {
	return core.Capabilities{Discover: true, Create: true, Resume: true, Persistent: true, Reason: "Native background sessions use Claude's supervisor; permissions/questions use the native terminal. Unmanaged interactive instances are read-only."}
}
func (p *Provider) Doctor(ctx context.Context) core.Diagnostic {
	d := core.Diagnostic{CreatePermissionMode: p.profile.ClaudeCreatePermissionMode, Provider: "claude", Profile: p.profile.Name, Capabilities: baseCapabilities(), LoggedIn: "unknown"}
	c, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	b, e := p.command(c, "--version").Output()
	d.Version = strings.TrimSpace(string(b))
	d.Available = e == nil
	if !d.Available {
		d.Details = "Install Claude Code and set the profile binary to its absolute path: " + e.Error()
		d.Capabilities = core.Capabilities{Discover: true}
		return d
	}
	b, e = p.command(c, "auth", "status").Output()
	if e == nil {
		var v struct {
			LoggedIn bool `json:"loggedIn"`
		}
		if json.Unmarshal(b, &v) == nil {
			d.LoggedIn = strconv.FormatBool(v.LoggedIn)
		}
	}
	_, e = p.agents(c)
	if e != nil {
		d.Capabilities = core.Capabilities{Discover: true}
		d.Details = e.Error()
	} else {
		d.Details = "binary=" + p.profile.Binary + "; config=" + p.profile.Home + "; native supervisor; transcript fallback parser tested with 2.1.302. Authentication status alone does not prove a request succeeds."
	}
	return d
}
func executionID(a agent) string {
	if a.PID <= 0 {
		return ""
	}
	b, e := exec.Command("ps", "-p", strconv.Itoa(a.PID), "-o", "lstart=").Output()
	if e != nil || len(bytes.TrimSpace(b)) == 0 {
		return ""
	}
	h := sha256.Sum256(bytes.TrimSpace(b))
	return fmt.Sprintf("claude:%s:%d:%s", a.SessionID, a.PID, hex.EncodeToString(h[:6]))
}
func applyAgent(s *core.Session, a agent) {
	transcriptState := s.TurnState
	s.Cwd = a.Cwd
	// Supervisor names identify running instances and can be generated names
	// such as project-15. Preserve the native conversation's title or prompt.
	if a.Name != "" && (s.Title == "" || s.Title == "Claude session") {
		s.Title = a.Name
	}
	s.ExecutionID = executionID(a)
	s.ExecutionState = "exited"
	if s.ExecutionID != "" {
		s.ExecutionState = "alive"
	}
	s.TurnState = "unknown"
	switch a.State {
	case "working":
		s.TurnState = "working"
	case "blocked":
		s.TurnState = "waiting_input"
	case "done":
		s.TurnState = "completed"
	case "failed":
		s.TurnState = "error"
	case "stopped":
		s.TurnState = "interrupted"
	}
	if a.Status == "busy" {
		s.TurnState = "working"
	}
	if a.Status == "waiting" {
		s.TurnState = "waiting_input"
		if strings.Contains(a.WaitingFor, "permission") || strings.Contains(a.WaitingFor, "sandbox") {
			s.TurnState = "waiting_approval"
		}
	}
	if a.Kind == "interactive" && a.State == "" && a.Status == "idle" && a.WaitingFor == "" && s.ExecutionState == "alive" && (transcriptState == "completed" || transcriptState == "error") {
		s.TurnState = transcriptState
	}
	if s.ExecutionState != "alive" {
		// Native background listings retain the worker's last state after it
		// exits (including blocked/working). Those are not current requests.
		switch s.TurnState {
		case "working", "waiting_input", "waiting_approval":
			s.TurnState = "unknown"
		}
		s.Pending = nil
	}
	s.Capabilities = baseCapabilities()
	s.Capabilities.Create = false
	s.Capabilities.Resume = s.ExecutionState == "exited"
	if a.Kind == "background" && s.ExecutionID != "" {
		s.Capabilities.Attach = true
		s.Capabilities.Send = true
		s.Capabilities.Interrupt = true
		s.Capabilities.Terminate = true
		s.Capabilities.Terminal = true
		s.Capabilities.Reason = "Attached to native background instance. Approvals and questions require native terminal; local and phone terminal share its input buffer."
	} else if s.ExecutionID != "" {
		s.Capabilities.Reason = "Existing interactive CLI has no supported attach channel. Read-only while it owns the session; use the one-time Pocket shell integration for future launches."
	}
	if a.WaitingFor != "" && s.ExecutionState == "alive" {
		s.Error = a.WaitingFor + " — open native terminal"
	}
}
func (p *Provider) Discover(ctx context.Context) ([]core.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	files, e := filepath.Glob(filepath.Join(p.profile.Home, "projects", "*", "*.jsonl"))
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	byID := map[string]core.Session{}
	transcriptStates := map[string]string{}
	for _, path := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		st, e := os.Stat(path)
		if e != nil {
			continue
		}
		seen[path] = true
		c, ok := p.cache[path]
		if !ok || c.size != st.Size() || !c.mod.Equal(st.ModTime()) {
			s, e := readMetadata(path, p.profile.Name)
			if e != nil {
				continue
			}
			s.UpdatedAt = st.ModTime()
			c = cachedSession{st.Size(), st.ModTime(), s}
			p.cache[path] = c
		}
		s := c.session
		transcriptStates[s.NativeID] = s.TurnState
		// Transcript completion is only an overlay for a verified idle native
		// instance, never a new completion alert for unowned historical files.
		s.TurnState = "unknown"
		if old, ok := byID[s.NativeID]; ok && old.Cwd != s.Cwd {
			s.Capabilities.Resume = false
			s.Error = "Duplicate native session ID in multiple projects; recovery requires manual resolution"
		}
		byID[s.NativeID] = s
	}
	for path := range p.cache {
		if !seen[path] {
			delete(p.cache, path)
		}
	}
	a, liveErr := p.agents(ctx)
	if liveErr == nil {
		for _, v := range a {
			if v.SessionID == "" {
				continue
			}
			s, ok := byID[v.SessionID]
			if !ok && executionID(v) == "" {
				continue
			}
			if !ok {
				s = core.Session{Provider: "claude", Profile: p.profile.Name, NativeID: v.SessionID, Title: v.Name, UpdatedAt: time.UnixMilli(v.StartedAt)}
			}
			s.TurnState = transcriptStates[v.SessionID]
			applyAgent(&s, v)
			if v.State == "blocked" && v.Status == "idle" && v.WaitingFor == "" && p.latestReplyFailed(ctx, v.SessionID, s.UpdatedAt) {
				s.TurnState = "error"
			}
			if v.Kind == "interactive" && s.ExecutionState == "alive" {
				if presence, err := p.inbox(v); err == nil {
					// agents --json collapses native "shell" into "busy". The
					// verified per-process registry distinguishes an idle main
					// turn with a background Bash task from active model work.
					// This says nothing about cron jobs or their next trigger.
					if s.TurnState == "working" && v.State == "" && v.WaitingFor == "" &&
						presence.Status == "shell" && presence.WaitingFor == "" && transcriptStates[v.SessionID] == "completed" {
						s.TurnState = "background"
					}
					s.Capabilities.Send = true
					s.Capabilities.InputMode = "inbox"
					if _, bridgeErr := p.bridge(s.ExecutionID); bridgeErr == nil {
						s.Capabilities.InputMode = "bridge"
					}
					s.Capabilities.Reason = "Connected to the running native inbox. Messages reach this same process under its inbound rules; approvals still require the native terminal."
					if s.Capabilities.InputMode == "bridge" {
						s.Capabilities.Reason = "Connected through this native session own command hook. Explicit inbound hold/refuse and native tool permissions remain enforced."
					}
				}
			}
			byID[v.SessionID] = s
		}
	} else {
		for id, s := range byID {
			s.ExecutionState = "unknown"
			s.Capabilities.Resume = false
			s.Capabilities.Reason = "Cannot verify native live owner: " + liveErr.Error()
			byID[id] = s
		}
	}
	out := make([]core.Session, 0, len(byID))
	for _, s := range byID {
		if _, e := os.Stat(s.Cwd); e != nil {
			s.Capabilities.Create = false
			s.Capabilities.Resume = false
			s.Error = "Workspace is unavailable: " + s.Cwd
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
func (p *Provider) findPath(id string) (string, error) {
	if !nativeIDPattern.MatchString(id) {
		return "", errors.New("invalid Claude native session ID")
	}
	paths, e := filepath.Glob(filepath.Join(p.profile.Home, "projects", "*", id+".jsonl"))
	if e != nil {
		return "", e
	}
	if len(paths) != 1 {
		return "", fmt.Errorf("native transcript absent or ambiguous (%d matches)", len(paths))
	}
	return paths[0], nil
}
func (p *Provider) History(ctx context.Context, id, cursor string, limit int) (core.History, error) {
	path, e := p.findPath(id)
	if e != nil {
		return core.History{}, e
	}
	h, err := readHistory(ctx, path, cursor, limit)
	if err == nil {
		p.displayPhoneDeliveries(&h, id)
	}
	return h, err
}

var nativeIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var backgroundIDPattern = regexp.MustCompile(`backgrounded\s*·\s*([a-f0-9]{8})`)

func validDirectory(cwd string) (string, error) {
	if cwd == "" {
		return "", errors.New("working directory is required")
	}
	s, e := filepath.EvalSymlinks(cwd)
	if e != nil {
		return "", fmt.Errorf("working directory: %w", e)
	}
	s, e = filepath.Abs(s)
	if e != nil {
		return "", e
	}
	st, e := os.Stat(s)
	if e != nil || !st.IsDir() {
		return "", errors.New("working directory is not an existing directory")
	}
	return s, nil
}

func (p *Provider) Execute(ctx context.Context, a core.Action) (core.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, ExecutionID: a.ExecutionID}
	if a.Model != "" || a.Effort != "" {
		if a.Kind != "create" {
			return r, errors.New("已有 Claude 会话请通过原生模型设置切换；恢复时不传模型参数以避免原生 fork")
		}
		if e := validateModelChoice(a); e != nil {
			return r, e
		}
	}
	if a.Kind == "delete" {
		return p.deleteHistory(ctx, a)
	}
	if a.Kind == "rename" {
		return p.renameHistory(ctx, a)
	}
	if a.Kind == "startup_status" || a.Kind == "startup_input" {
		return p.startupAction(ctx, a)
	}
	if a.Kind == "create" || a.Kind == "resume" {
		return p.start(ctx, a)
	}
	var terminal struct {
		Open bool `json:"open"`
	}
	if a.Kind == "terminal" {
		if len(a.Data) > 0 && json.Unmarshal(a.Data, &terminal) != nil {
			return r, errors.New("invalid terminal operation")
		}
		if !terminal.Open && executionExited(a.NativeID, a.ExecutionID) {
			r.Status = "accepted"
			r.Data, _ = json.Marshal(map[string]any{"state": "exited", "terminalId": a.TerminalID})
			return r, nil
		}
	}
	agents, e := p.agents(ctx)
	if e != nil {
		return r, e
	}
	var current *agent
	for i := range agents {
		if agents[i].SessionID == a.NativeID {
			current = &agents[i]
			break
		}
	}
	if current == nil {
		return r, errors.New("native session is not running; use explicit resume")
	}
	id := executionID(*current)
	if a.ExecutionID == "" || id == "" || a.ExecutionID != id {
		return r, errors.New("execution changed or exited; refresh before sending")
	}
	if current.Kind == "interactive" && a.Kind == "send" {
		return p.sendInbox(ctx, a, *current)
	}
	if current.Kind != "background" {
		return r, errors.New("unmanaged interactive CLI is read-only; do not create a second writer")
	}
	if a.Kind == "approve" {
		return r, errors.New("Claude approval requires the real native terminal; structured request IDs are not exposed by this CLI")
	}
	if a.Kind == "terminate" {
		return p.stopExecution(ctx, a, *current)
	}

	var att *attachment
	if a.Kind == "terminal" && !terminal.Open || a.Kind == "terminal_input" {
		att = p.attachments[id]
		if att == nil || a.TerminalID == "" || a.TerminalID != att.terminalID || !att.connected() {
			if a.Kind == "terminal" {
				r.Status = "accepted"
				r.Data, _ = json.Marshal(map[string]any{"state": "disconnected", "terminalId": a.TerminalID})
				return r, nil
			}
			return r, errors.New("Terminal connection ended or changed; reconnect explicitly before sending.")
		}
	} else {
		att, e = p.attach(ctx, *current, id)
		if e != nil {
			return r, e
		}
	}

	// Attaching may race a native supervisor restart. Never deliver a stale
	// action to the process that happens to replace its target.
	verified := false
	if fresh, err := p.agents(ctx); err == nil {
		for _, v := range fresh {
			if v.SessionID == a.NativeID && executionID(v) == id {
				verified = true
				copy := v
				current = &copy
				break
			}
		}
	}
	if !verified {
		return r, errors.New("native execution changed while attaching; refresh before sending")
	}
	a.TerminalID = att.terminalID
	switch a.Kind {
	case "attach", "terminal":
		var data struct {
			Cursor int64 `json:"cursor"`
		}
		_ = json.Unmarshal(a.Data, &data)
		out, cursor := att.snapshot(data.Cursor)
		screen, suggested, _ := att.view()
		r.Data, _ = json.Marshal(map[string]any{"state": att.state(), "terminalId": a.TerminalID, "output": string(out), "screen": screen, "suggestedReply": suggested, "cursor": cursor, "requestId": terminalToken(id, att.terminalID, screen), "ansi": true, "command": shellQuote(p.profile.Binary) + " attach " + current.ID, "description": "Shared native terminal. Use terminal keys for real permissions/questions; Ctrl+Z detaches without stopping."})
		r.Status = "accepted"
	case "send":
		if a.Text == "" {
			return r, errors.New("message is empty")
		}
		if strings.ContainsAny(a.Text, "\x00\x1b") {
			return r, errors.New("chat messages must not contain terminal escape characters")
		}
		if t := strings.TrimSpace(a.Text); strings.HasPrefix(t, "!") || strings.HasPrefix(t, "/") {
			return r, errors.New("native slash/bang commands require the terminal; chat accepts ordinary prompts")
		}
		if current.Status == "waiting" {
			return r, errors.New("native dialog is waiting; respond using terminal controls first")
		}
		sentAt := time.Now().UTC()
		r.Data, _ = json.Marshal(terminalDelivery{Delivery: "native_terminal", SentAt: sentAt})
		if n, err := att.write([]byte("\x1b[200~" + a.Text + "\x1b[201~\r")); err != nil {
			if n > 0 {
				r.Status = "unknown"
			}
			return r, err
		}
		r.Status = "submitted"
		r.Message = "Sent to the native input channel; transcript confirmation pending. Do not blindly retry."
		// Only a newly persisted matching native user record confirms acceptance.
		if path, e := p.findPath(a.NativeID); e == nil {
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for {
				h, e := readHistory(ctx, path, "", 12)
				if e == nil {
					for _, m := range h.Messages {
						if m.Role == "user" && terminalTextMatches(m.Text, a.Text) && !m.At.Before(sentAt) {
							r.Status = "accepted"
							r.Message = "Native transcript confirms the user message."
							return r, nil
						}
					}
				}
				select {
				case <-ctx.Done():
					r.Status = "unknown"
					return r, nil
				case <-deadline.C:
					return r, nil
				case <-tick.C:
				}
			}
		}
	case "interrupt":
		_, e = att.write([]byte{3})
		r.Status = "unknown"
		r.Message = "已向原生终端写入停止信号，但原生接口没有返回停止确认；这不表示实例已结束。"
	case "terminal_input":
		if len(a.Text) > 64*1024 {
			return r, errors.New("terminal input exceeds 64 KiB")
		}
		// Moving focus cannot submit anything, so it is not bound to the screen
		// token; this lets the phone press ↓ ↓ ↓ without waiting for a refresh.
		// Anything that can submit (Enter, text, Esc, Ctrl+C) still is.
		consume := ""
		if want := p.terminalRequest(id); !terminalNavigation(a.Text) {
			if want == "" || a.RequestID != want {
				return r, errors.New("native terminal request changed or was answered; refresh the terminal before responding")
			}
			if p.consumed[want] && !terminalNavigation(a.Text) {
				return r, errors.New("this native terminal response was already submitted; await native state")
			}
			if strings.ContainsAny(a.Text, "\r\n") {
				consume = want
			}
		}
		before := att.end()
		n, err := att.write([]byte(a.Text))
		if n > 0 && consume != "" {
			p.consumed[consume] = true
		}
		if err != nil {
			if n > 0 {
				r.Status = "unknown"
			}
			return r, err
		}
		r.Status = "submitted"
		r.Message = "Terminal input delivered; native result must be observed."
		// Return the screen as redrawn after this key so the phone shows the
		// effect (and the token for the next key) without waiting for a poll.
		// This is display only; it says nothing about native acceptance.
		settleTerminal(ctx, att, before)
		screen, suggested, cursor := att.view()
		r.Data, _ = json.Marshal(map[string]any{"state": att.state(), "terminalId": a.TerminalID, "screen": screen, "suggestedReply": suggested, "cursor": cursor, "requestId": terminalToken(id, att.terminalID, screen)})
	default:
		return r, fmt.Errorf("unsupported Claude action %q", a.Kind)
	}
	return r, e
}

// Every screen, including idle input and startup dialogs, requires a token.
// Navigation bypasses only the screen revision, never the connection identity.
func (p *Provider) terminalRequest(execution string) string {
	att := p.attachments[execution]
	if att == nil || !att.connected() {
		return ""
	}
	screen, _, _ := att.view()
	return terminalToken(execution, att.terminalID, screen)
}

func terminalToken(execution, transport, screen string) string {
	h := sha256.Sum256([]byte(execution + "\x00" + transport + "\x00" + screen))
	return "terminal:" + hex.EncodeToString(h[:16])
}

// A failed API request leaves the native agent blocked+idle with no wait,
// which is not an approval. Discovery runs every second, so the transcript is
// read again only after it changes.
func (p *Provider) latestReplyFailed(ctx context.Context, id string, updated time.Time) bool {
	if c, ok := p.apiErrors[id]; ok && !updated.IsZero() && c.updated.Equal(updated) {
		return c.failed
	}
	history, err := p.History(ctx, id, "", 8)
	if err != nil {
		return false
	}
	failed := false
	for i := len(history.Messages) - 1; i >= 0; i-- {
		m := history.Messages[i]
		if m.Role == "user" {
			break
		}
		if m.Role == "assistant" || m.Role == "error" {
			failed = strings.HasPrefix(strings.TrimSpace(m.Text), "API Error:")
			break
		}
	}
	if p.apiErrors == nil {
		p.apiErrors = map[string]apiErrorCheck{}
	}
	p.apiErrors[id] = apiErrorCheck{updated: updated, failed: failed}
	return failed
}

// Navigation never submits a selection. Keep it available after an answer
// while a multi-step native prompt is still visible. A run of navigation keys
// ("↓↓↓" to reach an option) is still only navigation.
func terminalNavigation(text string) bool {
	if text == "" {
		return false
	}
	for text != "" {
		matched := false
		for _, key := range []string{"\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D", "\x1b[Z", "\t"} {
			if strings.HasPrefix(text, key) {
				text, matched = text[len(key):], true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// settleTerminal waits briefly for the native CLI to redraw after a key: until
// new output arrives and then stays quiet for a moment, at most ~450ms.
func settleTerminal(ctx context.Context, att *attachment, before int64) {
	deadline := time.Now().Add(450 * time.Millisecond)
	last, quietSince := before, time.Time{}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Millisecond):
		}
		end := att.end()
		if end != last {
			last, quietSince = end, time.Now()
			continue
		}
		if last != before && time.Since(quietSince) >= 60*time.Millisecond {
			return
		}
	}
}

func (p *Provider) start(ctx context.Context, a core.Action) (core.Result, error) {
	r := core.Result{ActionID: a.ID}
	cwd, e := validDirectory(a.Cwd)
	if e != nil {
		return r, e
	}
	args := []string{"--bg"}
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}
	if a.Effort != "" {
		args = append(args, "--effort", a.Effort)
	}
	if a.Kind == "create" && p.profile.ClaudeCreatePermissionMode == "bypassPermissions" {
		args = append(args, "--dangerously-skip-permissions")
	}
	if a.Kind == "resume" {
		path, e := p.findPath(a.NativeID)
		if e != nil {
			return r, e
		}
		meta, e := readMetadata(path, p.profile.Name)
		if e != nil {
			return r, e
		}
		if meta.Cwd != "" && meta.Cwd != cwd {
			return r, errors.New("resume must use the existing native working directory")
		}
		agents, e := p.agents(ctx)
		if e != nil {
			return r, e
		}
		for _, v := range agents {
			if v.SessionID == a.NativeID && v.PID > 0 && executionID(v) != "" {
				return r, errors.New("session already has a live owner; attach instead of resuming")
			}
		}
		args = append(args, "--resume", a.NativeID)
	}
	if a.Text != "" {
		args = append(args, "--", a.Text)
	}
	launchctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c := p.command(launchctx, args...)
	c.WaitDelay = 2 * time.Second
	c.Dir = cwd
	b, e := c.CombinedOutput()
	if e != nil {
		if launchctx.Err() != nil {
			r.Status = "unknown"
			r.Message = "Native launch timed out before acknowledgement. Inspect Claude agents before retrying. On macOS, a background service can wait for Files and Folders access to Documents/Desktop; check native diagnostics or use a trusted accessible directory."
			return r, nil
		}
		if a.Kind == "create" && strings.Contains(string(b), "Workspace not trusted") {
			return p.beginStartup(a, args)
		}
		return r, fmt.Errorf("Claude native launch: %w: %s", e, b)
	}
	m := backgroundIDPattern.FindSubmatch(b)
	if len(m) != 2 {
		return r, fmt.Errorf("native launch did not return a recognized background ID: %s", b)
	}
	short := string(m[1])
	for attempts := 0; attempts < 20; attempts++ {
		agents, e := p.agents(ctx)
		if e == nil {
			for _, v := range agents {
				if v.ID == short {
					if a.Kind == "resume" && v.SessionID != a.NativeID {
						return r, errors.New("Claude created a different ID rather than resuming; refusing to claim success")
					}
					r.NativeID = v.SessionID
					r.ExecutionID = executionID(v)
					r.Status = "accepted"
					r.Message = "Native background session created; follow native state for startup, trust, permissions and completion."
					return r, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	r.Status = "unknown"
	r.Message = "Native launch returned a background ID, but reconciliation is pending. Do not retry creation."
	return r, nil
}
func (p *Provider) attach(ctx context.Context, a agent, id string) (*attachment, error) {
	size := desktopTerminalSize(ctx, p.profile.Binary, a)
	if old := p.attachments[id]; old != nil {
		old.mu.Lock()
		dead := old.dead
		old.mu.Unlock()
		if !dead {
			return old, old.resize(size)
		}
		delete(p.attachments, id)
	}
	// This process is only a terminal client. Killing it never kills the native
	// supervisor or worker. No shell is ever placed behind this PTY.
	c := p.command(context.Background(), "attach", a.ID)
	c.Env = append(c.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	if size == nil {
		size = &pty.Winsize{Rows: 32, Cols: 100}
	}
	f, input, e := startSingleTargetPTY(c, size)
	if e != nil {
		return nil, e
	}
	at := &attachment{terminalID: uuid.NewString(), input: input, cmd: c, file: f, ready: make(chan struct{}), size: *size}
	p.attachments[id] = at
	go func() {
		buf := make([]byte, 32768)
		for {
			n, e := f.Read(buf)
			if n > 0 {
				at.mu.Lock()
				at.output = append(at.output, buf[:n]...)
				if len(at.output) > 1024*1024 {
					drop := len(at.output) - 1024*1024
					at.output = append([]byte(nil), at.output[drop:]...)
					at.offset += int64(drop)
				}
				// Native permission dialogs hide the cursor, but still terminate
				// their rendered frame at the configured bottom row.
				bottom := []byte(fmt.Sprintf("\x1b[%d;1H", at.size.Rows))
				if bytes.Contains(at.output, []byte("\x1b[?25h")) || bytes.Contains(at.output, bottom) {
					at.once.Do(func() { close(at.ready) })
				}
				at.mu.Unlock()
			}
			if e != nil {
				break
			}
		}
		clientErr := c.Wait()
		_ = input.Close()
		_ = f.Close()
		at.mu.Lock()
		at.dead = true
		at.mu.Unlock()
		at.once.Do(func() { close(at.ready) })
		p.mu.Lock()
		defer p.mu.Unlock()
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = p.finishNativeExit(cleanup, a, id, at.exitInput.submittedAt(), clientErr)
	}()
	select {
	case <-at.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(8 * time.Second):
		return nil, errors.New("native terminal is not ready; open terminal to inspect; no input sent")
	}
	if !at.connected() {
		return nil, errors.New("Terminal connection ended or changed; reconnect explicitly before sending.")
	}
	return at, nil
}
func (a *attachment) write(b []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dead {
		return 0, errors.New("native attach client exited; refresh before sending")
	}
	if a.input != nil {
		_ = a.input.SetWriteDeadline(time.Now().Add(2 * time.Second))
		defer a.input.SetWriteDeadline(time.Time{})
		sentAt := time.Now().UTC()
		n, err := a.input.Write(b)
		a.exitInput.observe(b[:n], sentAt)
		return n, err
	}
	return a.file.Write(b)
}

func (a *attachment) connected() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.dead }
func (a *attachment) state() string {
	if a.connected() {
		return "connected"
	}
	return "disconnected"
}

// end is the stream position after the newest output, without copying it.
func (a *attachment) end() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.offset + int64(len(a.output))
}
func (a *attachment) snapshot(cursor int64) ([]byte, int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cursor < a.offset || cursor > a.offset+int64(len(a.output)) {
		cursor = a.offset
	}
	return append([]byte(nil), a.output[cursor-a.offset:]...), a.offset + int64(len(a.output))
}
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.startups {
		s.mu.Lock()
		done := s.Done
		s.mu.Unlock()
		if !done {
			_ = s.Cmd.Process.Kill()
		}
		_ = s.File.Close()
	}
	p.startups = map[string]*startup{}
	for _, a := range p.attachments {
		a.disconnect()
	}
	p.attachments = map[string]*attachment{}
	return nil
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// RunCLI is the reversible shell-integration entrypoint. The native executable
// is resolved once before dispatch; it is never invoked through a shell string.
func RunCLI(ctx context.Context, profile core.Profile, args []string, stdin, stdout, stderr *os.File) error {
	provider, e := New(profile)
	if e != nil {
		return e
	}
	p := provider.(*Provider)
	// Administrative and non-interactive commands keep their exact CLI behavior.
	passthrough := false
	for _, arg := range args {
		if arg == "-p" || arg == "--print" || arg == "--bg" || arg == "--background" || arg == "--version" || arg == "-v" || arg == "--help" || arg == "-h" {
			passthrough = true
		}
	}
	if len(args) > 0 {
		switch args[0] {
		case "auth", "agents", "attach", "logs", "stop", "kill", "rm", "respawn", "daemon", "doctor", "update", "mcp", "plugin", "install", "uninstall":
			passthrough = true
		}
	}
	if passthrough {
		c := p.command(ctx, args...)
		c.Stdin = stdin
		c.Stdout = stdout
		c.Stderr = stderr
		return c.Run()
	}
	// The native --bg --resume command deliberately forks a live conversation.
	// A shell user asking to resume one instead attaches to its existing worker.
	resume := ""
	for i, arg := range args {
		if (arg == "--resume" || arg == "-r") && i+1 < len(args) {
			resume = args[i+1]
		}
		if strings.HasPrefix(arg, "--resume=") {
			resume = strings.TrimPrefix(arg, "--resume=")
		}
	}
	if resume != "" {
		if aa, err := p.agents(ctx); err == nil {
			for _, a := range aa {
				if (a.SessionID == resume || a.Name == resume) && a.PID > 0 && executionID(a) != "" {
					if a.Kind != "background" {
						return errors.New("conversation is already running in an unmanaged interactive terminal; continue there")
					}
					fmt.Fprintln(stderr, "Attaching to the existing native background execution; its launch settings remain unchanged.")
					return p.runTargetCLI(ctx, a.ID, stdin, stdout)
				}
			}
		}
	}
	c := p.command(ctx, append([]string{"--bg"}, args...)...)
	output, e := runCLILauncherPTY(c, stdin, stdout)
	if e != nil {
		return fmt.Errorf("native background launch failed; inspect native sessions before retrying: %w", e)
	}
	m := backgroundIDPattern.FindStringSubmatch(terminalScreen(output))
	if len(m) != 2 {
		return errors.New("Claude did not return a recognized background ID; inspect native sessions before retrying")
	}
	return p.runTargetCLI(ctx, m[1], stdin, stdout)
}
