package codexnative

import (
	"context"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
)

func TestDeleteUsesNativeUUIDCommand(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "codex")
	capture := filepath.Join(home, "args")
	os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n"), 0700)
	p := &Provider{profile: core.Profile{Home: home, Binary: bin, Env: []string{"CAPTURE=" + capture}}}
	id := "11111111-2222-3333-4444-555555555555"
	r, e := p.deleteHistory(context.Background(), core.Action{NativeID: id})
	if e != nil || r.Status != "accepted" {
		t.Fatalf("%+v %v", r, e)
	}
	b, _ := os.ReadFile(capture)
	if strings.TrimSpace(string(b)) != "delete\n--force\n"+id {
		t.Fatalf("%q", b)
	}
	if _, e = p.deleteHistory(context.Background(), core.Action{NativeID: "--all"}); e == nil {
		t.Fatal("invalid ID accepted")
	}
}
