package codexnative

import (
	"context"
	"encoding/json"
	"os"
	"pocketagents/internal/core"
	"strings"
	"testing"
	"time"
)

func TestNativeApprovalProbe(t *testing.T) {
	if os.Getenv("POCKET_CODEX_APPROVAL_PROBE") != "1" {
		t.Skip("opt-in real harmless approval")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	pi, _ := New(core.Profile{Name: "default"})
	p := pi.(*Provider)
	defer p.Close()
	r, e := p.client(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var out struct {
		Thread       nativeThread    `json:"thread"`
		Policy       json.RawMessage `json:"approvalPolicy"`
		Sandbox      json.RawMessage `json:"sandbox"`
		Instructions []string        `json:"instructionSources"`
	}
	e = r.call(ctx, "thread/start", map[string]any{"cwd": "/tmp/pocket-codex-m0", "approvalPolicy": "on-request", "sandbox": "read-only", "approvalsReviewer": "user"}, &out)
	if e != nil {
		t.Fatal(e)
	}
	p.merge(out.Thread, true)
	id := out.Thread.ID
	t.Log("APPROVAL_THREAD", id, "policy", string(out.Policy), "sandbox", string(out.Sandbox))
	r.call(ctx, "thread/name/set", map[string]string{"threadId": id, "name": "Pocket M0 native approval probe"}, nil)
	res, e := p.Execute(ctx, core.Action{ID: "pocket-m0-approval-start", Kind: "send", NativeID: id, ExecutionID: p.execution(id), Text: "Pocket M0 explicit approval test. Use exec_command to run `printf POCKET_APPROVAL_OK` with sandbox_permissions require_escalated and justification 'Pocket M0 harmless approval verification'. This must produce a real user approval request. Do not write files or run other commands. After it finishes report the output."})
	if e != nil {
		t.Fatal(e)
	}
	t.Log("START", res.Status)
	var approval core.Approval
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		pending := p.approvalsLocked(id)
		p.mu.Unlock()
		if len(pending) > 0 {
			approval = pending[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if approval.ID == "" {
		h, _ := p.History(ctx, id, "", 20)
		t.Log("HISTORY", h)
		t.Fatal("no real approval arrived")
	}
	t.Log("PENDING", approval.Kind, approval.Detail, approval.Options)
	res, e = p.Execute(ctx, core.Action{ID: "pocket-m0-approval-accept", Kind: "approve", NativeID: id, ExecutionID: approval.ExecutionID, RequestID: approval.ID, Decision: "accept"})
	if e != nil {
		t.Fatal(e)
	}
	t.Log("APPROVE", res.Status, res.Message)
	_, e = p.Execute(ctx, core.Action{ID: "pocket-m0-approval-repeat", Kind: "approve", NativeID: id, ExecutionID: approval.ExecutionID, RequestID: approval.ID, Decision: "accept"})
	if e == nil {
		t.Fatal("duplicate approval accepted")
	}
	t.Log("DUPLICATE_REJECTED", e)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		s := p.sessions[id]
		p.mu.Unlock()
		if s.TurnState == "completed" || s.TurnState == "error" {
			t.Log("FINAL_STATE", s.TurnState, s.Error)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	h, e := p.History(ctx, id, "", 20)
	t.Log("FINAL_HISTORY", h, e)
}

func TestNativeInterruptProbe(t *testing.T) {
	if os.Getenv("POCKET_CODEX_INTERRUPT_PROBE") != "1" {
		t.Skip("opt-in interrupt test creates a dedicated thread")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pi, _ := New(core.Profile{Name: "default"})
	p := pi.(*Provider)
	defer p.Close()
	res, e := p.Execute(ctx, core.Action{ID: "pocket-m0-sleep", Kind: "create", Cwd: "/tmp/pocket-codex-m0", Text: "Pocket M0 persistence test. Run exactly `sleep 20` with exec_command, then reply POCKET_SLEEP_DONE. No other tools, no file changes."})
	if e != nil {
		t.Fatal(e)
	}
	id := res.NativeID
	t.Log("INTERRUPT_THREAD", id, "EXECUTION", res.ExecutionID)
	time.Sleep(3 * time.Second)
	before := p.execution(id)
	p.Close()
	r, e := p.client(ctx)
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := p.loaded(ctx, r)
	if e != nil || !loaded[id] {
		t.Fatalf("thread stopped on disconnect %v %v", loaded[id], e)
	}
	if before != p.execution(id) {
		t.Fatal("execution changed across disconnect")
	}
	t.Log("DISCONNECTED_RECONNECTED_SAME_EXECUTION", before)
	res, e = p.Execute(ctx, core.Action{ID: "pocket-m0-interrupt", Kind: "interrupt", NativeID: id, ExecutionID: before})
	if e != nil {
		t.Fatal(e)
	}
	t.Log("INTERRUPT", res.Status)
	for ctx.Err() == nil {
		var turns struct {
			Data []nativeTurn `json:"data"`
		}
		e = r.call(ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 1}, &turns)
		if e != nil {
			t.Fatal(e)
		}
		if len(turns.Data) > 0 && turns.Data[0].Status == "interrupted" {
			t.Log("NATIVE_TURN_STATUS", "interrupted")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("native interrupted status not observed")
}

func TestNativeStoppedResumeProbe(t *testing.T) {
	id := os.Getenv("POCKET_CODEX_RESUME_THREAD")
	if id == "" {
		t.Skip("opt-in existing stopped Pocket test thread")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	pi, _ := New(core.Profile{Name: "default"})
	p := pi.(*Provider)
	defer p.Close()
	r, e := p.client(ctx)
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := p.loaded(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if loaded[id] {
		t.Fatal("test requires a stopped, unloaded thread")
	}
	held, e := p.writerHeld(id)
	if e != nil || held {
		t.Fatal("test thread still has a writer")
	}
	res, e := p.Execute(ctx, core.Action{ID: "pocket-m0-resume-stopped", Kind: "resume", NativeID: id})
	if e != nil {
		t.Fatal(e)
	}
	if res.NativeID != id || res.ExecutionID == "" {
		t.Fatal("native resume identity invalid")
	}
	t.Log("RESUMED_NATIVE_ID", res.NativeID, "EXECUTION", res.ExecutionID)
	res, e = p.Execute(ctx, core.Action{ID: "pocket-m0-stopped-second", Kind: "send", NativeID: id, ExecutionID: res.ExecutionID, Text: "Pocket M0 resume round 2: reply POCKET_STOPPED_TWO and confirm POCKET_STOPPED_ONE is in history. No tools."})
	if e != nil {
		t.Fatal(e)
	}
	for ctx.Err() == nil {
		p.mu.Lock()
		state := p.sessions[id].TurnState
		p.mu.Unlock()
		if state == "completed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	h, e := p.History(ctx, id, "", 10)
	if e != nil {
		t.Fatal(e)
	}
	t.Log("RESUMED_HISTORY", h)
	text := ""
	for _, m := range h.Messages {
		text += m.Text
	}
	if !strings.Contains(text, "POCKET_STOPPED_TWO") || !strings.Contains(text, "POCKET_STOPPED_ONE") {
		t.Fatal("round-trip markers missing")
	}
}

func TestNativeTerminalApprovalProbe(t *testing.T) {
	id := os.Getenv("POCKET_CODEX_LOCAL_APPROVAL_THREAD")
	if id == "" {
		t.Skip("opt-in manual native TUI approval test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pi, _ := New(core.Profile{Name: "default"})
	p := pi.(*Provider)
	defer p.Close()
	r, e := p.client(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.join(ctx, r, id, false); e != nil {
		t.Fatal(e)
	}
	_, e = p.Execute(ctx, core.Action{ID: "pocket-m0-local-approval-" + time.Now().Format("150405"), Kind: "send", NativeID: id, ExecutionID: p.execution(id), Text: "Pocket M0 local-first approval test. Execute exactly `printf POCKET_LOCAL_APPROVAL` using exec_command with sandbox_permissions require_escalated and justification 'Pocket M0 local terminal approval test'. Wait for real approval. Do not run other tools or write files."})
	if e != nil {
		t.Fatal(e)
	}
	var approval core.Approval
	for ctx.Err() == nil {
		p.mu.Lock()
		reqs := p.approvalsLocked(id)
		p.mu.Unlock()
		if len(reqs) > 0 {
			approval = reqs[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if approval.ID == "" {
		t.Fatal("approval did not arrive")
	}
	t.Log("WAITING_FOR_NATIVE_TUI", approval.Detail)
	var resolved bool
	for ctx.Err() == nil {
		p.mu.Lock()
		_, pending := p.pending[approval.ID]
		p.mu.Unlock()
		if !pending {
			resolved = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !resolved {
		t.Fatal("native TUI resolution not observed")
	}
	_, e = p.Execute(ctx, core.Action{ID: "pocket-m0-stale-after-native", Kind: "approve", NativeID: id, ExecutionID: approval.ExecutionID, RequestID: approval.ID, Decision: "accept"})
	if e == nil {
		t.Fatal("stale phone approval accepted")
	}
	t.Log("NATIVE_TUI_FIRST_PHONE_STALE_REJECTED", e)
}

func TestNativeQuestionProbe(t *testing.T) {
	if os.Getenv("POCKET_CODEX_QUESTION_PROBE") != "1" {
		t.Skip("opt-in native question test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pi, _ := New(core.Profile{Name: "default"})
	p := pi.(*Provider)
	defer p.Close()
	r, e := p.client(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var started struct {
		Thread nativeThread `json:"thread"`
		Model  string       `json:"model"`
	}
	if e = r.call(ctx, "thread/start", map[string]string{"cwd": "/tmp/pocket-codex-m0"}, &started); e != nil {
		t.Fatal(e)
	}
	id := started.Thread.ID
	p.merge(started.Thread, true)
	r.call(ctx, "thread/name/set", map[string]string{"threadId": id, "name": "Pocket M0 native question probe"}, nil)
	t.Log("QUESTION_THREAD", id)
	e = r.call(ctx, "turn/start", map[string]any{"threadId": id, "input": []map[string]string{{"type": "text", "text": "Pocket M0 harmless interface test. Call request_user_input exactly once asking which marker to echo: POCKET_ANSWER_A or POCKET_ANSWER_B. After receiving the response repeat that marker. Do not run any other tools or write files."}}, "collaborationMode": map[string]any{"mode": "plan", "settings": map[string]any{"model": started.Model}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	var a core.Approval
	for ctx.Err() == nil {
		p.mu.Lock()
		reqs := p.approvalsLocked(id)
		p.mu.Unlock()
		if len(reqs) > 0 {
			a = reqs[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if a.ID == "" {
		t.Fatal("no native question arrived")
	}
	t.Log("QUESTION", a.Kind, a.Detail)
	res, e := p.Execute(ctx, core.Action{ID: "pocket-m0-answer", Kind: "approve", NativeID: id, ExecutionID: a.ExecutionID, RequestID: a.ID, Decision: "answer", Text: "POCKET_ANSWER_A"})
	if e != nil {
		t.Fatal(e)
	}
	t.Log("ANSWER", res.Status)
	for ctx.Err() == nil {
		p.mu.Lock()
		state := p.sessions[id].TurnState
		p.mu.Unlock()
		if state == "completed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	h, e := p.History(ctx, id, "", 10)
	t.Log("QUESTION_FINAL_HISTORY", h, e)
	if e != nil {
		t.Fatal(e)
	}
}
