package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketagents/internal/core"
)

func TestNativeHistoryPaginationAndTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	var b strings.Builder
	for _, id := range []string{"one", "two", "three", "four"} {
		v := map[string]any{"type": "user", "uuid": id, "message": map[string]any{"content": id}, "timestamp": "2026-10-17T00:00:00Z"}
		j, _ := json.Marshal(v)
		b.Write(j)
		b.WriteByte('\n')
	}
	if e := os.WriteFile(path, []byte(b.String()), 0600); e != nil {
		t.Fatal(e)
	}
	h, e := readHistory(context.Background(), path, "", 2)
	if e != nil || len(h.Messages) != 2 || h.Messages[0].ID != "three" || !h.HasMore {
		t.Fatalf("page one: %+v %v", h, e)
	}
	appendFile, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = appendFile.WriteString(`{"type":"user","uuid":"five","message":{"content":"five"}}` + "\n")
	_ = appendFile.Close()
	h2, e := readHistory(context.Background(), path, h.Cursor, 2)
	if e != nil || len(h2.Messages) != 2 || h2.Messages[0].ID != "one" || h2.Messages[1].ID != "two" {
		t.Fatalf("page two: %+v %v", h2, e)
	}
	_ = os.WriteFile(path, []byte("{}\n"), 0600)
	if _, e = readHistory(context.Background(), path, h.Cursor, 2); e == nil {
		t.Fatal("truncated transcript accepted old cursor")
	}
}
func TestExistingInteractiveOwnerNeverAdvertisesControl(t *testing.T) {
	s := core.Session{}
	applyAgent(&s, agent{Kind: "interactive", SessionID: "test", PID: os.Getpid(), Status: "idle"})
	if s.Capabilities.Send || s.Capabilities.Terminal || s.Capabilities.Resume || s.ExecutionState != "alive" || s.TurnState != "unknown" {
		t.Fatalf("unsafe live capabilities: %+v", s)
	}
}
func TestNativeBackgroundStatesAreNotInferredFromSilence(t *testing.T) {
	for _, tc := range []struct{ state, status, waiting, want string }{{"working", "idle", "", "working"}, {"done", "idle", "", "completed"}, {"blocked", "waiting", "permission prompt", "waiting_approval"}, {"blocked", "waiting", "input needed", "waiting_input"}, {"", "idle", "", "unknown"}} {
		s := core.Session{}
		applyAgent(&s, agent{Kind: "background", PID: os.Getpid(), State: tc.state, Status: tc.status, WaitingFor: tc.waiting})
		if s.TurnState != tc.want {
			t.Fatalf("%+v: %s", tc, s.TurnState)
		}
	}
}

func TestExitedNativeWorkerDoesNotAdvertiseHistoricalWaitOrWork(t *testing.T) {
	for _, tc := range []struct{ state, status, waiting, want string }{
		{"working", "", "", "unknown"},
		{"blocked", "", "", "unknown"},
		{"", "busy", "", "unknown"},
		{"", "waiting", "input needed", "unknown"},
		{"blocked", "waiting", "permission prompt", "unknown"},
		{"done", "", "", "completed"},
		{"failed", "", "", "error"},
		{"stopped", "", "", "interrupted"},
	} {
		s := core.Session{Pending: []core.Approval{{ID: "stale-request"}}}
		applyAgent(&s, agent{Kind: "background", State: tc.state, Status: tc.status, WaitingFor: tc.waiting})
		if s.ExecutionState != "exited" || s.ExecutionID != "" || s.TurnState != tc.want || len(s.Pending) != 0 {
			t.Fatalf("historical state leaked as current activity for %+v: %+v", tc, s)
		}
		if s.Capabilities.Send || s.Capabilities.Terminal || s.Capabilities.Interrupt || !s.Capabilities.Resume {
			t.Fatalf("exited native worker has invalid capabilities: %+v", s.Capabilities)
		}
		if strings.Contains(s.Error, "open native terminal") {
			t.Fatalf("exited worker advertises a stale native wait: %q", s.Error)
		}
	}
}

// Opt-in integration test creates only a dedicated native M0 session. It runs a
// real model request using the configured user's existing credentials/config.
func TestRealNativeLoop(t *testing.T) {
	if os.Getenv("POCKET_CLAUDE_REAL") == "" {
		t.Skip("set POCKET_CLAUDE_REAL=1 to invoke the native CLI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	profile := core.Profile{Name: "default", Binary: os.Getenv("POCKET_CLAUDE_BINARY")}
	v, e := New(profile)
	if e != nil {
		t.Fatal(e)
	}
	p := v.(*Provider)
	defer p.Close()
	cwd := os.Getenv("POCKET_CLAUDE_CWD")
	if cwd == "" {
		t.Fatal("explicit dedicated POCKET_CLAUDE_CWD required")
	}
	r, e := p.Execute(ctx, core.Action{ID: "m0-create-" + time.Now().Format("150405"), Kind: "create", Cwd: cwd, Text: "Reply exactly POCKET_ADAPTER_ONE. Do not run any tools."})
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("CREATE native=%s execution=%s status=%s", r.NativeID, r.ExecutionID, r.Status)
	if r.NativeID == "" || r.ExecutionID == "" {
		t.Fatalf("missing identity: %+v", r)
	}
	wait := func(provider core.Provider, token string) core.Session {
		t.Helper()
		for {
			h, e := provider.History(ctx, r.NativeID, "", 30)
			if e == nil {
				for _, m := range h.Messages {
					if m.Role == "assistant" && strings.Contains(m.Text, token) {
						ss, e := provider.Discover(ctx)
						if e != nil {
							t.Fatal(e)
						}
						for _, s := range ss {
							if s.NativeID == r.NativeID {
								return s
							}
						}
					}
				}
			}
			select {
			case <-ctx.Done():
				t.Fatalf("token %s missing: %v", token, ctx.Err())
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	s := wait(p, "POCKET_ADAPTER_ONE")
	t.Logf("DISCOVER execution=%s state=%s", s.ExecutionID, s.TurnState)
	a, e := p.Execute(ctx, core.Action{ID: "m0-send-" + time.Now().Format("150405"), Kind: "send", NativeID: r.NativeID, ExecutionID: s.ExecutionID, Text: "Reply exactly POCKET_ADAPTER_TWO 中文 🚀. Do not run tools."})
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("SEND %s %s", a.Status, a.Message)
	s = wait(p, "POCKET_ADAPTER_TWO")
	before := s.ExecutionID
	_ = p.Close()
	v, e = New(profile)
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	ss, e := v.Discover(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, s = range ss {
		if s.NativeID == r.NativeID {
			break
		}
	}
	if s.NativeID != r.NativeID || s.ExecutionID != before {
		t.Fatalf("native instance changed across adapter restart: %s -> %s", before, s.ExecutionID)
	}
	a, e = v.Execute(ctx, core.Action{ID: "m0-after-restart", Kind: "send", NativeID: r.NativeID, ExecutionID: before, Text: "Reply exactly POCKET_ADAPTER_THREE. Do not run tools."})
	if e != nil {
		t.Fatal(e)
	}
	s = wait(v, "POCKET_ADAPTER_THREE")
	t.Logf("RESTART+SEND native=%s same-execution=%s state=%s status=%s", s.NativeID, s.ExecutionID, s.TurnState, a.Status)
	_, e = v.Execute(ctx, core.Action{ID: "m0-stop", Kind: "terminate", NativeID: r.NativeID, ExecutionID: s.ExecutionID})
	if e != nil {
		t.Fatal(e)
	}
}

func TestRealExistingSession(t *testing.T) {
	if os.Getenv("POCKET_CLAUDE_EXISTING") == "" {
		t.Skip("set dedicated POCKET_CLAUDE_EXISTING native ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	v, e := New(core.Profile{Binary: os.Getenv("POCKET_CLAUDE_BINARY")})
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	ss, e := v.Discover(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var s core.Session
	for _, candidate := range ss {
		if candidate.NativeID == os.Getenv("POCKET_CLAUDE_EXISTING") {
			s = candidate
			break
		}
	}
	if s.NativeID == "" {
		t.Fatal("native session not discovered")
	}
	t.Logf("SESSION native=%s execution=%s state=%s cwd=%s", s.NativeID, s.ExecutionID, s.TurnState, s.Cwd)
	kind := os.Getenv("POCKET_CLAUDE_ACTION")
	if kind == "" {
		kind = "terminal"
	}
	action := core.Action{ID: "m0-existing-" + time.Now().Format("150405.000"), Kind: kind, NativeID: s.NativeID, ExecutionID: s.ExecutionID, Text: os.Getenv("POCKET_CLAUDE_TEXT")}
	if kind == "terminal" {
		action.Data = json.RawMessage(`{"open":true}`)
	}
	if kind == "terminal_input" {
		opened, err := v.Execute(ctx, core.Action{Kind: "terminal", NativeID: s.NativeID, ExecutionID: s.ExecutionID, Data: json.RawMessage(`{"open":true}`)})
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			TerminalID string `json:"terminalId"`
			RequestID  string `json:"requestId"`
		}
		if json.Unmarshal(opened.Data, &data) != nil || data.TerminalID == "" {
			t.Fatal("terminal identity absent")
		}
		action.TerminalID, action.RequestID = data.TerminalID, data.RequestID
	}
	r, e := v.Execute(ctx, action)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("ACTION %s status=%s message=%s", kind, r.Status, r.Message)
	if len(r.Data) > 0 {
		time.Sleep(150 * time.Millisecond)
		var identity struct {
			TerminalID string `json:"terminalId"`
		}
		_ = json.Unmarshal(r.Data, &identity)
		if next, err := v.Execute(ctx, core.Action{Kind: "terminal", NativeID: s.NativeID, ExecutionID: s.ExecutionID, TerminalID: identity.TerminalID}); err == nil {
			r = next
		}
		var d map[string]any
		_ = json.Unmarshal(r.Data, &d)
		t.Logf("SCREEN\n%s", d["screen"])
	}
	if kind == "terminal_input" {
		time.Sleep(time.Second)
	}
}

func TestRealResume(t *testing.T) {
	if os.Getenv("POCKET_CLAUDE_RESUME") == "" {
		t.Skip("set a dedicated stopped POCKET_CLAUDE_RESUME native ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	v, e := New(core.Profile{Binary: os.Getenv("POCKET_CLAUDE_BINARY")})
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	id := os.Getenv("POCKET_CLAUDE_RESUME")
	sentAt := time.Now().UTC()
	r, e := v.Execute(ctx, core.Action{Model: os.Getenv("POCKET_CLAUDE_MODEL"), Effort: os.Getenv("POCKET_CLAUDE_EFFORT"), ID: "m0-resume", Kind: "resume", NativeID: id, Cwd: os.Getenv("POCKET_CLAUDE_CWD"), Text: "Reply exactly POCKET_RESUMED_NATIVE_ID. Do not run tools."})
	if e != nil {
		t.Fatal(e)
	}
	if r.NativeID != id {
		t.Fatalf("native ID changed: %+v", r)
	}
	for {
		h, e := v.History(ctx, id, "", 20)
		if e == nil {
			for _, m := range h.Messages {
				if m.Role == "assistant" && !m.At.Before(sentAt) && strings.Contains(m.Text, "POCKET_RESUMED_NATIVE_ID") {
					t.Logf("RESUME native=%s execution=%s", id, r.ExecutionID)
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func TestRealApproval(t *testing.T) {
	if os.Getenv("POCKET_CLAUDE_APPROVAL") == "" {
		t.Skip("set dedicated manual-mode POCKET_CLAUDE_APPROVAL native ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	v, e := New(core.Profile{Binary: os.Getenv("POCKET_CLAUDE_BINARY")})
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	p := v.(*Provider)
	id := os.Getenv("POCKET_CLAUDE_APPROVAL")
	var a agent
	aa, e := p.agents(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, candidate := range aa {
		if candidate.SessionID == id {
			a = candidate
		}
	}
	if a.PID == 0 {
		t.Fatal("test native session is not alive")
	}
	execution := executionID(a)
	_, e = p.Execute(ctx, core.Action{ID: "approval-prompt", Kind: "send", NativeID: id, ExecutionID: execution, Text: "Use the Write tool to create approval-second.txt containing exactly SECOND_APPROVAL_PROOF. Do not use Bash or worktrees."})
	if e != nil {
		t.Fatal(e)
	}
	for {
		aa, e = p.agents(ctx)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, candidate := range aa {
			if candidate.SessionID == id && candidate.Status == "waiting" && candidate.WaitingFor == "permission prompt" {
				found = true
				a = candidate
				break
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	r, e := p.Execute(ctx, core.Action{Kind: "terminal", NativeID: id, ExecutionID: execution, Data: json.RawMessage(`{"open":true}`)})
	if e != nil {
		t.Fatal(e)
	}
	var data struct {
		RequestID  string `json:"requestId"`
		TerminalID string `json:"terminalId"`
	}
	_ = json.Unmarshal(r.Data, &data)
	if data.RequestID == "" {
		t.Fatal("missing terminal wait token")
	}
	t.Logf("REAL WAIT native=%s execution=%s token=%s", id, execution, data.RequestID)
	_, e = p.Execute(ctx, core.Action{ID: "approval-enter", Kind: "terminal_input", NativeID: id, ExecutionID: execution, RequestID: data.RequestID, TerminalID: data.TerminalID, Text: "\r"})
	if e != nil {
		t.Fatal(e)
	}
	for {
		b, e := os.ReadFile(filepath.Join(a.Cwd, "approval-second.txt"))
		if e == nil && strings.Contains(string(b), "SECOND_APPROVAL_PROOF") {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	time.Sleep(300 * time.Millisecond)
	_, e = p.Execute(ctx, core.Action{ID: "approval-stale", Kind: "terminal_input", NativeID: id, ExecutionID: execution, RequestID: data.RequestID, TerminalID: data.TerminalID, Text: "\r"})
	if e == nil {
		t.Fatal("stale terminal approval was accepted")
	}
	t.Logf("APPROVED then stale response rejected: %v", e)
}

func TestStartupStyledAcknowledgementAndLostController(t *testing.T) {
	// Native --bg in a PTY styles the separator/ID; matching raw bytes misses
	// a successful launch and dangerously encourages the user to create twice.
	raw := "\x1b[32mbackgrounded\x1b[0m · \x1b[1mc1b5eff3\x1b[0m\r\n"
	match := backgroundIDPattern.FindStringSubmatch(terminalScreen([]byte(raw)))
	if len(match) != 2 || match[1] != "c1b5eff3" {
		t.Fatalf("native styled acknowledgement missed: %q", terminalScreen([]byte(raw)))
	}
	p := &Provider{}
	r, e := p.startupAction(context.Background(), core.Action{Kind: "startup_input", RequestID: "old", ExecutionID: "startup:old", Text: "\r"})
	if e != nil || r.Status != "unknown" || r.NativeID != "" {
		t.Fatalf("lost controller must not claim acceptance or fabricate a session: %+v %v", r, e)
	}
	r, e = p.startupResult(context.Background(), &startup{Expired: true})
	if e != nil || r.Status != "unknown" || len(r.Data) != 0 {
		t.Fatalf("expired launcher must not expose stale input controls: %+v %v", r, e)
	}
}

func TestBlockedWithoutNativeQuestionDoesNotAskForApproval(t *testing.T) {
	for _, tc := range []struct{ status, waiting, want string }{
		{"", "", "unknown"},
		{"idle", "", "unknown"},
		{"waiting", "input needed", "waiting_input"},
		{"", "input needed", "waiting_input"},
		{"waiting", "permission prompt", "waiting_approval"},
		{"", "sandbox permission", "waiting_approval"},
		{"busy", "", "working"},
	} {
		s := core.Session{}
		applyAgent(&s, agent{Kind: "background", PID: os.Getpid(), State: "blocked", Status: tc.status, WaitingFor: tc.waiting})
		if s.TurnState != tc.want {
			t.Fatalf("%+v: got %s", tc, s.TurnState)
		}
	}
}

func TestHistoryLookupDistinguishesMissingAndDuplicateRecords(t *testing.T) {
	p := &Provider{profile: core.Profile{Home: t.TempDir()}}
	id := "00000000-0000-0000-0000-000000000001"
	if _, err := p.findPath(id); err == nil || !strings.Contains(err.Error(), "No native conversation history") {
		t.Fatalf("missing history: %v", err)
	}
	for _, project := range []string{"first", "second"} {
		dir := filepath.Join(p.profile.Home, "projects", project)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if path, err := p.findPath(id); err == nil || path != "" || !strings.Contains(err.Error(), "Multiple native conversation records") {
		t.Fatalf("ambiguous history selected: %q %v", path, err)
	}
}
