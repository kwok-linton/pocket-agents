package claudenative

import (
	"context"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"testing"
)

func TestCreatePermissionModeIsExplicitAndNotGlobal(t *testing.T) {
	for _, mode := range []string{"", "default", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "args")
			bin := filepath.Join(dir, "native")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			p := &Provider{profile: core.Profile{Binary: bin, Home: dir, Env: []string{"CAPTURE=" + capture}, ClaudeCreatePermissionMode: mode}}
			_, _ = p.start(context.Background(), core.Action{Kind: "create", Cwd: dir, Text: "literal --permission-mode default"})
			b, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			args := string(b)
			if strings.Contains(args, "\n--dangerously-skip-permissions\n") != (mode == "bypassPermissions") {
				t.Fatalf("mode %q: %q", mode, args)
			}
			if !strings.HasSuffix(args, "--\nliteral --permission-mode default\n") {
				t.Fatal("task must remain after argument boundary")
			}
			if err := p.command(context.Background(), "--version").Run(); err == nil {
				t.Fatal("fixture should exit 1")
			}
			b, _ = os.ReadFile(capture)
			if string(b) != "--version\n" {
				t.Fatal("permission flags leaked to non-create command")
			}
		})
	}
}
