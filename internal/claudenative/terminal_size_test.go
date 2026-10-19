package claudenative

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestDesktopAttachTerminalsMatchSessionAndExcludePhone(t *testing.T) {
	processes := `
  101 50 ttys002 /opt/bin/claude attach abcd1234
  102 99 ttys003 /opt/bin/claude attach abcd1234
  103 50 pts/4 /opt/bin/claude attach full-native-id
  104 50 ttys005 /opt/bin/claude attach another-session
  105 50 ?? /opt/bin/claude attach abcd1234
  106 50 ../../file /opt/bin/claude attach abcd1234
  107 50 ttys006 /other/bin/claude attach abcd1234
  108 50 ttys007 /opt/bin/claude attach abcd1234-extra
`
	got := desktopAttachTerminals(processes, "/opt/bin/claude", agent{ID: "abcd1234", SessionID: "full-native-id"}, 99)
	if !reflect.DeepEqual(got, []string{"/dev/ttys002", "/dev/pts/4"}) {
		t.Fatalf("unrelated/private terminal selected: %v", got)
	}
}

func TestAttachedPhoneFollowsDesktopSplitWithoutBrowserReads(t *testing.T) {
	open := func() (*os.File, *os.File) {
		t.Helper()
		master, slave, err := pty.Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { master.Close(); slave.Close() })
		return master, slave
	}
	_, desktop := open()
	_, otherDesktop := open()
	phone, phoneSlave := open()
	set := func(f *os.File, rows, cols uint16) {
		t.Helper()
		if err := pty.Setsize(f, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
			t.Fatal(err)
		}
	}
	set(desktop, 47, 187)
	set(otherDesktop, 36, 120)
	set(phone, 32, 100)
	// The other session is listed first and our phone client is listed last.
	// Neither may override the desktop belonging to this native session.
	tty := func(f *os.File) string {
		rel, err := filepath.Rel("/dev", f.Name())
		if err != nil {
			t.Fatal(err)
		}
		return rel
	}
	processes := fmt.Sprintf("101 50 %s /fixture/claude attach other\n102 50 %s /fixture/claude attach target\n103 99 %s /fixture/claude attach target\n", tty(otherDesktop), tty(desktop), tty(phoneSlave))
	var missing atomic.Bool
	var lookups atomic.Int32
	att := &attachment{file: phone, terminalID: "same-connection", size: pty.Winsize{Rows: 32, Cols: 100}}
	ctx, cancel := context.WithCancel(context.Background())
	att.sizeCancel = cancel
	att.sizeDone = att.followSize(ctx, func(context.Context) *pty.Winsize {
		lookups.Add(1)
		if missing.Load() {
			return nil
		}
		return desktopSizeFromProcesses(processes, "/fixture/claude", agent{ID: "target"}, 99)
	})
	t.Cleanup(att.disconnect)
	waitSize := func(rows, cols uint16) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			size, err := pty.GetsizeFull(phoneSlave)
			if err != nil {
				t.Fatal(err)
			}
			att.mu.Lock()
			tracked := att.size
			att.mu.Unlock()
			if size.Rows == rows && size.Cols == cols && tracked.Rows == rows && tracked.Cols == cols {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("phone did not follow desktop to %dx%d", cols, rows)
	}
	waitSize(47, 187)
	set(desktop, 45, 93)
	waitSize(45, 93)
	set(desktop, 47, 187)
	waitSize(47, 187)
	missing.Store(true)
	before := lookups.Load()
	deadline := time.Now().Add(3 * time.Second)
	for lookups.Load() == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if lookups.Load() == before {
		t.Fatal("follower stopped when browser was idle")
	}
	waitSize(47, 187)
	if att.terminalID != "same-connection" {
		t.Fatal("resize replaced the connection")
	}
	att.disconnect()
	select {
	case <-att.sizeDone:
	default:
		t.Fatal("disconnected attachment left its size follower running")
	}
}

func TestSizeFollowerCancellationDoesNotApplyLateLookup(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	att := &attachment{file: master, size: pty.Winsize{Rows: 45, Cols: 93}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	att.sizeCancel = cancel
	att.sizeDone = att.followSize(ctx, func(ctx context.Context) *pty.Winsize {
		close(started)
		<-ctx.Done()
		return &pty.Winsize{Rows: 47, Cols: 187}
	})
	<-started
	att.disconnect()
	if att.size.Rows != 45 || att.size.Cols != 93 {
		t.Fatal("late lookup resized a disconnected client")
	}
}

func TestAttachmentResizeAndViewUseTheSameDimensions(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	a := &attachment{file: master}
	size := &pty.Winsize{Rows: 45, Cols: 93}
	if err := a.resize(size); err != nil {
		t.Fatal(err)
	}
	actual, err := pty.GetsizeFull(slave)
	if err != nil || actual.Rows != 45 || actual.Cols != 93 {
		t.Fatalf("native PTY did not follow desktop: %+v %v", actual, err)
	}
	a.output = []byte("\x1b[40;1H选项\x1b[43;1H❯ 2. [✔] 已选择\x1b[45;1H底部")
	screen, _, cursor := a.view()
	want, _ := terminalViewSize(a.output, 45, 93)
	if screen != want || cursor != int64(len(a.output)) {
		t.Fatalf("phone did not use native dimensions: %q", screen)
	}
	if err := a.resize(nil); err != nil || a.size.Rows != 45 {
		t.Fatal("desktop disconnect changed native dimensions", err)
	}
}
