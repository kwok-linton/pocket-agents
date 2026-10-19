package claudenative

import (
	"context"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
)

func TestModelFlagsAndValidation(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "native")
	capture := filepath.Join(dir, "args")
	os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\nexit 1\n"), 0700)
	p := &Provider{profile: core.Profile{Binary: bin, Home: dir, Env: []string{"CAPTURE=" + capture}}}
	_, _ = p.start(context.Background(), core.Action{Kind: "create", Cwd: dir, Model: "sonnet", Effort: "low", Text: "hello"})
	b, _ := os.ReadFile(capture)
	if !strings.Contains(string(b), "--model\nsonnet\n--effort\nlow\n") {
		t.Fatalf("%q", b)
	}
	for _, a := range []core.Action{{Model: "--help"}, {Model: "sonnet\r/model opus"}, {Effort: "fake"}} {
		if validateModelChoice(a) == nil {
			t.Fatal("invalid model flags accepted")
		}
	}
}
