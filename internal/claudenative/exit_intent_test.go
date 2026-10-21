package claudenative

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketagents/internal/core"
)

func TestWaitTargetCLIWaitsForPIDWithoutStartingAnotherWorker(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture")
	script := fmt.Sprintf(`#!/bin/sh
test "$1" = agents || exit 91
if [ ! -f '%s/seen' ]; then
 touch '%s/seen'
 printf '[{"id":"target","sessionId":"session","kind":"background"}]'
else
 printf '[{"id":"target","sessionId":"session","kind":"background"},{"id":"target","sessionId":"session","kind":"background","pid":%d}]'
fi
`, dir, dir, os.Getpid())
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	p := &Provider{profile: core.Profile{Binary: binary, Home: dir}}
	a, generation, err := p.waitTargetCLI(context.Background(), "target")
	if err != nil || a == nil || a.PID != os.Getpid() || generation == "" {
		t.Fatalf("%+v %q %v", a, generation, err)
	}
}

func TestWaitTargetCLIDoesNotAttachOtherSessionOrWaitForever(t *testing.T) {
	for _, output := range []string{
		`[{"id":"other","sessionId":"other","kind":"background","pid":%d}]`,
		`[{"id":"target","sessionId":"session","kind":"background"}]`,
	} {
		dir := t.TempDir()
		binary := filepath.Join(dir, "fixture")
		body := strings.ReplaceAll(output, "%d", fmt.Sprint(os.Getpid()))
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' '"+body+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		p := &Provider{profile: core.Profile{Binary: binary, Home: dir}}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		a, _, err := p.waitTargetCLI(ctx, "target")
		cancel()
		if err == nil || a != nil {
			t.Fatalf("unexpected target: %+v %v", a, err)
		}
	}
}
