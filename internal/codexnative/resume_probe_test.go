package codexnative

import (
	"context"
	"os"
	"pocketagents/internal/core"
	"testing"
	"time"
)

func TestNativeResumeWithFirstMessage(t *testing.T) {
	if os.Getenv("POCKET_RESUME_PROBE") != "1" {
		t.Skip("dedicated native probe opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pi, e := New(core.Profile{Name: "default", Binary: "codex"})
	if e != nil {
		t.Fatal(e)
	}
	p := pi.(*Provider)
	defer p.Close()
	r, e := p.client(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var start struct {
		Thread nativeThread `json:"thread"`
	}
	if e = r.call(ctx, "thread/start", map[string]any{"cwd": os.TempDir(), "ephemeral": false}, &start); e != nil {
		t.Fatal(e)
	}
	id := start.Thread.ID
	if e = r.call(ctx, "turn/start", map[string]any{"threadId": id, "input": []map[string]string{{"type": "text", "text": "Reply only SEED_OK. Do not use tools."}}}, nil); e != nil {
		t.Fatal(e)
	}
	for {
		var page struct {
			Data []nativeTurn `json:"data"`
		}
		if e = r.call(ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 1}, &page); e != nil {
			t.Fatal(e)
		}
		if len(page.Data) > 0 && page.Data[0].Status != "inProgress" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Second):
		}
	}
	if e = r.call(ctx, "thread/unsubscribe", map[string]string{"threadId": id}, nil); e != nil {
		t.Fatal(e)
	}
	model, effort := "", ""
	if os.Getenv("POCKET_MODEL_PROBE") == "1" {
		catalog, err := p.catalog(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range catalog {
			if len(m.SupportedReasoningEfforts) > 0 {
				model = m.Model
				effort = m.SupportedReasoningEfforts[0].ReasoningEffort
				break
			}
		}
		if model == "" {
			t.Fatal("no native model options")
		}
	}
	result, e := p.Execute(ctx, core.Action{Model: model, Effort: effort, ID: "resume-first-message-" + id, Kind: "resume", NativeID: id, Cwd: os.TempDir(), Text: "Reply only POCKET_RESUME_MESSAGE_OK. Do not use tools or change any files."})
	if e != nil || result.Status != "accepted" || result.NativeID != id {
		t.Fatalf("%+v %v", result, e)
	}
	var response map[string]any
	if e = r.call(ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 10}, &response); e != nil {
		t.Fatal(e)
	}
	turns, ok := response["data"].([]any)
	if !ok || len(turns) != 2 {
		t.Fatalf("message did not start exactly one turn: %+v", response)
	}
	if model != "" {
		var current struct {
			Model  string `json:"model"`
			Effort string `json:"reasoningEffort"`
		}
		if e = r.call(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &current); e != nil {
			t.Fatal(e)
		}
		if current.Model != model || current.Effort != effort {
			t.Fatalf("native setting mismatch: %+v want %s/%s", current, model, effort)
		}
		t.Logf("native readback model=%s effort=%s", current.Model, current.Effort)
	}
	t.Logf("native ID preserved %s; one new turn started (two total)", id)
}
