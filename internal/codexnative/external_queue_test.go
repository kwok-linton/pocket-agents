package codexnative

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
	"time"
)

func TestNativeExternalQueue(t *testing.T) {
	binary := os.Getenv("POCKET_TEST_CODEX_QUEUE_BINARY")
	if binary == "" {
		t.Skip("explicit native integration opt-in required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "pocket-queue-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "native.sock")
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "unix://"+socket)
	cmd.Stderr, err = os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	for i := 0; i < 100; i++ {
		if _, err = os.Stat(socket); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	private, err := dialRPC(ctx, socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer private.close()
	home, _ := os.UserHomeDir()
	shared, err := dialRPC(ctx, filepath.Join(home, ".codex/app-server-control/app-server-control.sock"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.close()
	var created struct {
		Thread nativeThread `json:"thread"`
	}
	if err = private.call(ctx, "thread/start", map[string]any{"cwd": filepath.Join(home, ".pocket-agents/validation"), "ephemeral": false}, &created); err != nil {
		t.Fatal(err)
	}
	id := created.Thread.ID
	var initial json.RawMessage
	if err = private.call(ctx, "turn/start", map[string]any{"threadId": id, "input": []map[string]string{{"type": "text", "text": "Reply exactly POCKET_PRIVATE_QUEUE_READY. Do not use tools."}}}, &initial); err != nil {
		t.Fatal(err)
	}
	// This second connection never resumes/loads the privately owned thread.
	pp, err := New(core.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	adapter := pp.(*Provider)
	offered := adapter.offerExternalQueue(ctx, shared, core.Session{NativeID: id, ExecutionState: "alive"})
	if !offered.Capabilities.Send || offered.ExecutionID == "" {
		t.Fatal("queue not offered for live external owner")
	}
	fixture := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			fixture.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	imagePath := filepath.Join(dir, "fixture.png")
	imageFile, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(imageFile, fixture); err != nil {
		t.Fatal(err)
	}
	_ = imageFile.Close()
	action := core.Action{ID: "pocket-external-queue-proof-" + time.Now().Format("150405"), Kind: "send", NativeID: id, ExecutionID: offered.ExecutionID, Text: "Inspect the attached image. Reply only POCKET_EXTERNAL_QUEUE_COLOR_ followed by its dominant color in English uppercase. Do not use tools.", InputFiles: []core.FileInput{{Path: imagePath, MIME: "image/png"}}}
	result, err := adapter.sendExternalQueue(ctx, shared, action)
	if err != nil || result.Status != "accepted" {
		t.Fatalf("queue: %s %v", result.Status, err)
	}
	action.ExecutionID = "stale"
	if _, err := adapter.sendExternalQueue(ctx, shared, action); err == nil {
		t.Fatal("stale writer accepted")
	}
	t.Log("native queue accepted without thread/resume; waiting for private owner")
	for ctx.Err() == nil {
		var history struct {
			Data []struct {
				Item json.RawMessage `json:"item"`
			} `json:"data"`
		}
		if err = private.call(ctx, "thread/items/list", map[string]any{"threadId": id, "limit": 30, "sortDirection": "desc"}, &history); err != nil {
			t.Fatal(err)
		}
		for _, item := range history.Data {
			m := renderItem(item.Item)
			if m.Role == "assistant" && strings.Contains(m.Text, "POCKET_EXTERNAL_QUEUE_COLOR_RED") {
				if adapter.queueGeneration(id) != offered.ExecutionID {
					t.Fatal("native ownership changed")
				}
				loaded, err := adapter.loaded(ctx, shared)
				if err != nil || loaded[id] {
					t.Fatal("shared daemon must not load the private thread")
				}
				t.Logf("PASS: original private process %d consumed shared native queue and replied; native thread %s", cmd.Process.Pid, id)
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatal("private owner did not consume native queue within deadline")
}
