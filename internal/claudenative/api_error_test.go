package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pocketagents/internal/core"
)

func TestLatestAPIErrorIsCachedUntilTranscriptChanges(t *testing.T) {
	p := &Provider{profile: core.Profile{Home: t.TempDir(), DataDir: t.TempDir()}}
	id := "22222222-2222-4222-8222-222222222222"
	dir := filepath.Join(p.profile.Home, "projects", "test")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	write := func(lines ...[2]string) {
		var b []byte
		for i, l := range lines {
			line, _ := json.Marshal(map[string]any{"type": l[0], "uuid": l[0] + string(rune('a'+i)), "sessionId": id, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]string{"role": l[0], "content": l[1]}})
			b = append(append(b, line...), '\n')
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	first := time.Unix(1000, 0)
	write([2]string{"user", "fix it"}, [2]string{"assistant", `API Error: 524 {"status":524}`})
	if !p.latestReplyFailed(ctx, id, first) {
		t.Fatal("latest API error not detected")
	}
	// Same update time: the cached answer is used without reading the file.
	write([2]string{"user", "fix it"}, [2]string{"assistant", "done"})
	if !p.latestReplyFailed(ctx, id, first) {
		t.Fatal("unchanged transcript was re-read")
	}
	if p.latestReplyFailed(ctx, id, first.Add(time.Second)) {
		t.Fatal("recovered reply still reported as an API error")
	}
	write([2]string{"assistant", "API Error: 500"}, [2]string{"user", "continue"})
	if p.latestReplyFailed(ctx, id, first.Add(2*time.Second)) {
		t.Fatal("error before a newer user message reported as current")
	}
	write([2]string{"assistant", "Explaining what API Error: 524 means"})
	if p.latestReplyFailed(ctx, id, first.Add(3*time.Second)) {
		t.Fatal("quoted API error text treated as a failure")
	}
}
