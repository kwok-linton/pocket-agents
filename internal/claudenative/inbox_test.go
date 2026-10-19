package claudenative

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
	"time"
)

func TestInboxRefusesStaleIdentity(t *testing.T) {
	p := &Provider{profile: core.Profile{Home: t.TempDir()}}
	if _, err := p.inbox(agent{PID: os.Getpid(), SessionID: "missing"}); err == nil {
		t.Fatal("missing registry accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, []byte("x"), 0644)
	if ownedPrivate(file, false) == nil {
		t.Fatal("public file accepted")
	}
	_ = os.Chmod(file, 0600)
	if ownedPrivate(file, true) == nil {
		t.Fatal("regular file accepted as socket")
	}
	link := file + "-link"
	_ = os.Symlink(file, link)
	if ownedPrivate(link, false) == nil {
		t.Fatal("symlink accepted")
	}
}
func TestNativeInteractiveInbox(t *testing.T) {
	id := os.Getenv("POCKET_TEST_INBOX_SESSION")
	if id == "" {
		t.Skip("explicit dedicated native session required")
	}
	dataDir := os.Getenv("POCKET_TEST_BRIDGE_DATA")
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	provider, err := New(core.Profile{Name: "default", Binary: os.Getenv("POCKET_TEST_CLAUDE_BINARY"), DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	p := provider.(*Provider)
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	agents, err := p.agents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var current agent
	for _, a := range agents {
		if a.SessionID == id {
			current = a
		}
	}
	if current.Kind != "interactive" {
		t.Fatal("target must be a dedicated interactive CLI")
	}
	generation := executionID(current)
	if os.Getenv("POCKET_TEST_BRIDGE_DATA") != "" {
		if _, err := p.bridge(generation); err != nil {
			t.Fatal("actual hook bridge required:", err)
		}
	}
	action := core.Action{ID: "pocket-inbox-proof-" + time.Now().Format("150405.000000"), NativeID: id, ExecutionID: generation, Kind: "send", Text: "Reply exactly POCKET_NATIVE_INBOX_ADAPTER_OK. Do not use tools or modify any files."}
	result, err := p.Execute(ctx, action)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "accepted" {
		t.Fatalf("delivery status %s: %s", result.Status, result.Message)
	}
	if executionID(current) != generation {
		t.Fatal("native execution changed")
	}
	history, err := p.History(ctx, id, "", 30)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range history.Messages {
		if m.Role == "user" && m.Kind == "phone_message" && m.Text == action.Text {
			found = true
		}
	}
	if !found {
		t.Fatal("confirmed phone message not visible in history")
	}
	t.Log("same native execution, confirmed transcript delivery, phone attribution passed")
}

func TestInboxLateConfirmation(t *testing.T) {
	p := &Provider{profile: core.Profile{Home: t.TempDir(), DataDir: t.TempDir()}}
	id := "11111111-1111-4111-8111-111111111111"
	dir := filepath.Join(p.profile.Home, "projects", "test")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	a := core.Action{ID: "late-phone", NativeID: id, Kind: "send", Text: "phone text"}
	pending := core.Result{ActionID: a.ID, Status: "submitted"}
	r, err := p.ReconcileResult(context.Background(), a, pending)
	if err != nil || r.Status != "submitted" {
		t.Fatalf("absent message accepted: %+v %v", r, err)
	}
	marker := fmt.Sprintf("[Pocket Agents message %x]\n", sha256.Sum256([]byte(a.ID)))
	appendMessage := func(role, uuid string) {
		b, _ := json.Marshal(map[string]any{"type": role, "uuid": uuid, "sessionId": id, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "origin": map[string]string{"kind": "peer"}, "isMeta": true, "promptSource": "system", "message": map[string]string{"role": role, "content": marker + a.Text}})
		f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		_, _ = f.Write(append(b, '\n'))
	}
	appendMessage("assistant", "echo")
	r, err = p.ReconcileResult(context.Background(), a, pending)
	if err != nil || r.Status != "submitted" {
		t.Fatalf("assistant echo accepted: %+v %v", r, err)
	}
	appendMessage("user", "delivered")
	r, err = p.ReconcileResult(context.Background(), a, pending)
	if err != nil || r.Status != "accepted" {
		t.Fatalf("late delivery missing: %+v %v", r, err)
	}
	h, err := p.History(context.Background(), id, "", 30)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range h.Messages {
		if m.ID == "delivered" && m.Kind == "phone_message" && m.Text == a.Text {
			found = true
		}
	}
	if !found {
		t.Fatal("late phone attribution missing")
	}
}

func TestTerminalLateConfirmationRejectsOldHistoryAndAssistantEcho(t *testing.T) {
	p := &Provider{profile: core.Profile{Home: t.TempDir(), DataDir: t.TempDir()}}
	id := "22222222-2222-4222-8222-222222222222"
	dir := filepath.Join(p.profile.Home, "projects", "terminal")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	sent := time.Now().UTC()
	action := core.Action{ID: "terminal-late", Kind: "send", NativeID: id, Text: "same text"}
	data, _ := json.Marshal(terminalDelivery{Delivery: "native_terminal", SentAt: sent})
	pending := core.Result{ActionID: action.ID, Status: "unknown", Data: data}
	write := func(role, uuid string, at time.Time) {
		b, _ := json.Marshal(map[string]any{"type": role, "uuid": uuid, "sessionId": id, "timestamp": at.Format(time.RFC3339Nano), "message": map[string]string{"role": role, "content": action.Text}})
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err = f.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	write("user", "old", sent.Add(-time.Second))
	write("assistant", "echo", sent.Add(time.Second))
	got, err := p.ReconcileResult(context.Background(), action, pending)
	if err != nil || got.Status != "unknown" {
		t.Fatalf("old/echo incorrectly confirmed: %+v %v", got, err)
	}
	write("user", "new", sent.Add(2*time.Second))
	got, err = p.ReconcileResult(context.Background(), action, pending)
	if err != nil || got.Status != "accepted" {
		t.Fatalf("late native user not confirmed: %+v %v", got, err)
	}
	pending.Data = json.RawMessage(`{"delivery":"native_terminal"}`)
	got, err = p.ReconcileResult(context.Background(), action, pending)
	if err != nil || got.Status != "unknown" {
		t.Fatalf("missing send time incorrectly confirmed: %+v %v", got, err)
	}
}

func TestLegacyInterruptDoesNotWaitForeverOrClaimSuccess(t *testing.T) {
	p := &Provider{}
	before := core.Result{ActionID: "old-stop", Status: "submitted"}
	got, err := p.ReconcileResult(context.Background(), core.Action{ID: "old-stop", Kind: "interrupt"}, before)
	if err != nil || got.Status != "unknown" || got.ActionID != before.ActionID || got.Message == "" {
		t.Fatalf("legacy stop must become explicitly unconfirmed: %+v, %v", got, err)
	}
	again, err := p.ReconcileResult(context.Background(), core.Action{ID: "old-stop", Kind: "interrupt"}, got)
	if err != nil || again.Status != "unknown" {
		t.Fatalf("query must not promote stop to success: %+v %v", again, err)
	}
}

func TestNativePastedContentReceipt(t *testing.T) {
	sent := "开头\n中文 pasted body\n尾行"
	wrapped := "开头\n\n<pasted_content id=\"4dde\">\n\n中文 pasted body\n尾行\n</pasted_content id=\"4dde\">\n"
	if !terminalTextMatches(wrapped, sent) {
		t.Fatal("native paste envelope not matched")
	}
	for _, bad := range []string{wrapped + "extra", strings.Replace(wrapped, "4dde\">\n", "other\">\n", 1), strings.Replace(wrapped, "pasted body", "different body", 1)} {
		if terminalTextMatches(bad, sent) {
			t.Fatal("nonmatching message accepted")
		}
	}
	if terminalTextMatches(wrapped, sent+" ") {
		t.Fatal("meaningful whitespace dropped")
	}
	if terminalTextMatches(wrapped, "<pasted_content") {
		t.Fatal("user typed tag normalized")
	}
}

func TestLateInboxReceiptBeyondHistoryWindow(t *testing.T) {
	p := &Provider{profile: core.Profile{Home: t.TempDir(), DataDir: t.TempDir()}}
	id := "33333333-3333-4333-8333-333333333333"
	dir := filepath.Join(p.profile.Home, "projects", "long")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, id+".jsonl")
	a := core.Action{ID: "very-late", Kind: "send", NativeID: id, Text: "fixture prompt"}
	marker := fmt.Sprintf("[Pocket Agents message %x]\n", sha256.Sum256([]byte(a.ID)))
	line, _ := json.Marshal(map[string]any{"type": "user", "uuid": "original-receipt", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]string{"content": marker + a.Text}})
	data := append(line, '\n')
	for i := 0; i < 4100; i++ {
		row, _ := json.Marshal(map[string]any{"type": "assistant", "uuid": fmt.Sprint(i), "message": map[string]string{"content": "later unrelated reply"}})
		data = append(data, append(row, '\n')...)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := p.ReconcileResult(context.Background(), a, core.Result{ActionID: a.ID, Status: "unknown"})
	if err != nil || result.Status != "accepted" {
		t.Fatal("old native receipt missed", result.Status, err)
	}
}
