package claudenative

import (
	"reflect"
	"testing"

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
