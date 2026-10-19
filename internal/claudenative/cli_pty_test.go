package claudenative

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"pocketagents/internal/core"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestRunCLIFirstWorkspaceKeepsNativeTTYAndHandsInputToAttach(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "native-fixture")
	record := filepath.Join(dir, "calls")
	script := `#!/bin/sh
set -eu
if [ "$1" = agents ]; then
 printf '[{"id":"c1b5eff3","sessionId":"10000000-0000-4000-8000-000000000001","kind":"background","pid":%s}]\n' "$POCKET_FIXTURE_PID"
 exit 0
fi
test -t 1 && test -t 2 || exit 91
printf '%s\n' "$*" >> "$POCKET_FIXTURE_RECORD"
case "$1" in
  --bg)
    test -t 0 || exit 96
    stty size >> "$POCKET_FIXTURE_RECORD"
    printf 'NATIVE TRUST PROMPT> '
    IFS= read -r answer
    test "$answer" = yes || exit 92
    printf 'TRUST_ACCEPTED\n' >> "$POCKET_FIXTURE_RECORD"
    printf '\033[32mbackgrounded\033[0m · \033[1mc1b5eff3\033[0m\r\n'
    ;;
  attach)
    test ! -t 0 || exit 97
    test "$2" = c1b5eff3 || exit 93
    printf 'NATIVE ATTACH PROMPT> '
    IFS= read -r answer
    test "$answer" = continued || exit 94
    printf 'ATTACH_INPUT_RECEIVED\n' >> "$POCKET_FIXTURE_RECORD"
    ;;
  *) exit 95 ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err = pty.Setsize(slave, &pty.Winsize{Rows: 47, Cols: 131}); err != nil {
		t.Fatal(err)
	}
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		finished <- RunCLI(ctx, core.Profile{Binary: binary, Home: dir, Env: []string{"POCKET_FIXTURE_RECORD=" + record, "POCKET_FIXTURE_PID=" + strconv.Itoa(os.Getpid())}}, []string{"first workspace prompt"}, slave, slave, slave)
	}()
	defer func() {
		cancel()
		<-runDone
	}()
	chunks := make(chan string, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, e := master.Read(buf)
			if n > 0 {
				select {
				case chunks <- string(buf[:n]):
				case <-ctx.Done():
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	waitOutput := func(want string) {
		t.Helper()
		var output strings.Builder
		for !strings.Contains(output.String(), want) {
			select {
			case chunk := <-chunks:
				output.WriteString(chunk)
			case e := <-finished:
				t.Fatalf("native exited before %q: %v; output=%q", want, e, output.String())
			case <-ctx.Done():
				t.Fatalf("missing native output %q: %s", want, output.String())
			}
		}
	}
	waitOutput("NATIVE TRUST PROMPT> ")
	if _, err = master.Write([]byte("yes\r")); err != nil {
		t.Fatal(err)
	}
	waitOutput("NATIVE ATTACH PROMPT> ")
	if _, err = master.Write([]byte("continued\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	after, err := term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("original terminal mode not restored: %v", err)
	}
	calls, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "--bg first workspace prompt\n47 131\nTRUST_ACCEPTED\nattach c1b5eff3\nATTACH_INPUT_RECEIVED\n" {
		t.Fatalf("unexpected native calls or input: %q", calls)
	}
}

func TestRunCLINonTerminalRejectsBeforeNativeLaunch(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "native-fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	err = RunCLI(context.Background(), core.Profile{Binary: binary, Home: dir}, []string{"new prompt"}, input, os.Stdout, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "requires a terminal on stdin") {
		t.Fatalf("nonterminal startup was not clearly rejected: %v", err)
	}
}

func TestCLILaunchFailureRestoresTerminal(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "printf 'NATIVE LAUNCH REFUSED'; exit 7")
	raw, err := runCLILauncherPTY(cmd, slave, output)
	if err == nil || !strings.Contains(err.Error(), "exit status 7") || !strings.Contains(string(raw), "NATIVE LAUNCH REFUSED") {
		t.Fatalf("native failure/output not retained: %v %q", err, raw)
	}
	after, err := term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failure did not restore terminal: %v", err)
	}
}
