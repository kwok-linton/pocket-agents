package claudenative

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestAttachDisplayPreservesOrdinaryOutput(t *testing.T) {
	// Control strings may contain text that looks like a screen transition.
	want := "中文 Reconnecting to example…\r\n\x1b[31mred\x1b[0m" +
		"\x1b]0;title " + attachAltLeave + "\a" +
		"\x1bPpayload " + attachAltEnter + "\x1b\\\x1b(Bend"
	for step := 1; step <= len(want); step++ {
		d := &attachDisplay{}
		var got []byte
		for i := 0; i < len(want); i += step {
			got = append(got, d.feed([]byte(want[i:min(i+step, len(want))]), time.Time{})...)
		}
		got = append(got, d.finish()...)
		if string(got) != want {
			t.Fatalf("chunk size %d changed ordinary output: %q", step, got)
		}
	}
}

func TestAttachDisplayReconnectAcrossEveryBoundary(t *testing.T) {
	initial := attachAltEnter + "OLD FRAME"
	setup := "\x1b[2J\x1b[H\n  \x1b[2mAttaching…\x1b[0m\n"
	replay := "\x1b[2J\x1b[HNEW 中文 FRAME\x1b[?25l"
	reconnect := attachAltLeave + "Reconnecting to example…\r\n" +
		attachAltEnter + setup + attachSyncBegin + replay + attachSyncEnd
	want := initial + attachSyncBegin + setup + replay + attachSyncEnd
	for split := 0; split <= len(reconnect); split++ {
		d := &attachDisplay{}
		got := d.feed([]byte(initial), time.Time{})
		got = append(got, d.feed([]byte(reconnect[:split]), time.Time{})...)
		got = append(got, d.feed([]byte(reconnect[split:]), time.Time{})...)
		if string(got) != want {
			t.Fatalf("split %d leaked a screen transition or lost replay: %q", split, got)
		}
		if d.holding {
			t.Fatal("still holding after replay")
		}
		if tail := string(d.finish()); tail != attachSyncEnd+"\x1b[?25h"+attachAltLeave {
			t.Fatalf("abrupt exit did not restore main screen: %q", tail)
		}
	}
}

func TestAttachDisplayRepeatedReconnectAndQuietFallback(t *testing.T) {
	now := time.Now()
	d := &attachDisplay{size: func() (int, int) { return 44, 20 }}
	d.feed([]byte(attachAltEnter+"old"), now)
	for i := 0; i < 3; i++ {
		cycle := attachAltLeave + "Reconnecting…\r\n" + attachAltEnter +
			"\x1b[2J\x1b[H" + attachSyncBegin + "  Attaching…\r\n" + attachSyncEnd
		if got := d.feed([]byte(cycle), now); len(got) != 0 {
			t.Fatalf("placeholder painted: %q", got)
		}
		if got := d.idle(now.Add(100 * time.Millisecond)); len(got) != 0 {
			t.Fatalf("placeholder painted after quiet boundary: %q", got)
		}
	}
	notice := string(d.idle(now.Add(time.Second)))
	if !strings.Contains(notice, "\x1b[44;1H") || strings.ContainsAny(notice, "\r\n") || !strings.Contains(notice, "Reconnecting...") {
		t.Fatalf("invalid bottom-line notice: %q", notice)
	}
	if got := d.idle(now.Add(2 * time.Second)); len(got) != 0 {
		t.Fatalf("notice repeated: %q", got)
	}
	d.feed([]byte("\x1b[2J\x1b[HRESTORED"), now.Add(2*time.Second))
	if got := d.idle(now.Add(2010 * time.Millisecond)); len(got) != 0 {
		t.Fatal("painted before quiet boundary")
	}
	got := string(d.idle(now.Add(2100 * time.Millisecond)))
	if !strings.HasPrefix(got, attachSyncBegin) || !strings.HasSuffix(got, attachSyncEnd) || !strings.Contains(got, "RESTORED") || strings.Contains(got, attachAltLeave) {
		t.Fatalf("bad fallback frame: %q", got)
	}
}

func TestAttachDisplayDoesNotPaintPartialSynchronizedFrame(t *testing.T) {
	now := time.Now()
	d := &attachDisplay{}
	d.feed([]byte(attachAltEnter+"old"+attachAltLeave+attachAltEnter+attachSyncBegin+"partial"), now)
	if got := d.idle(now.Add(100 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("painted in-progress frame: %q", got)
	}
	if got := string(d.feed([]byte(" complete"+attachSyncEnd), now)); got != attachSyncBegin+"partial complete"+attachSyncEnd {
		t.Fatalf("incomplete replay: %q", got)
	}
}

func TestAttachDisplayReplayAfterLargeBlankCanvas(t *testing.T) {
	now := time.Now()
	for _, step := range []int{1, 8192} {
		d := &attachDisplay{}
		d.feed([]byte(attachAltEnter+"old"+attachAltLeave), now)
		data := attachAltEnter + "  Attaching…\r\n" + attachSyncBegin + strings.Repeat(" ", 8192) + "replay" + attachSyncEnd
		var got []byte
		for i := 0; i < len(data); i += step {
			got = append(got, d.feed([]byte(data[i:min(i+step, len(data))]), now)...)
		}
		if !strings.HasSuffix(string(got), "replay"+attachSyncEnd) || d.holding {
			t.Fatalf("chunk size %d: blank padding prevented recovery", step)
		}
	}
}

func TestAttachDisplayTerminalQueriesPassOnce(t *testing.T) {
	d := &attachDisplay{}
	d.feed([]byte(attachAltEnter+"old"+attachAltLeave), time.Time{})
	queries := "\x1b[6n\x1b[c\x1b[>c\x1b[?2026$p\x1b[?u\x1b]11;?\a"
	var got []byte
	for _, b := range []byte(queries) {
		got = append(got, d.feed([]byte{b}, time.Time{})...)
	}
	if string(got) != queries {
		t.Fatalf("terminal queries blocked: %q", got)
	}
	if got := string(d.finish()); got != attachAltLeave {
		t.Fatalf("queries repeated on exit: %q", got)
	}
}

func TestAttachDisplayExitRetainsDiagnostics(t *testing.T) {
	for _, suffix := range []string{"", "\r\nError: native connection lost\r\n", "\x1b["} {
		d := &attachDisplay{}
		initial := attachAltEnter + "old"
		got := d.feed([]byte(initial+attachAltLeave+suffix), time.Time{})
		got = append(got, d.finish()...)
		if string(got) != initial+attachAltLeave+suffix {
			t.Fatalf("exit diagnostic changed: %q", got)
		}
	}
}

func TestAttachDisplayBufferLimitFailsOpenWithoutLoss(t *testing.T) {
	for _, payload := range []string{
		strings.Repeat("x", attachDisplayLimit+1),
		"\x1b]0;" + strings.Repeat("x", 4097) + "\a",
	} {
		d := &attachDisplay{}
		want := attachAltEnter + "old" + attachAltLeave + "Reconnecting…" + attachAltEnter + payload + "after"
		var got []byte
		for i := 0; i < len(want); i += 17 {
			got = append(got, d.feed([]byte(want[i:min(i+17, len(want))]), time.Time{})...)
		}
		got = append(got, d.finish()...)
		if !bytes.Equal(got, []byte(want)) || !d.passthrough {
			t.Fatalf("fail-open changed output: got %d bytes, want %d", len(got), len(want))
		}
	}
}
