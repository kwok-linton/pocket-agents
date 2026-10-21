package claudenative

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

const (
	attachAltEnter     = "\x1b[?1049h"
	attachAltLeave     = "\x1b[?1049l"
	attachSyncBegin    = "\x1b[?2026h"
	attachSyncEnd      = "\x1b[?2026l"
	attachDisplayLimit = 512 * 1024
)

// attachDisplay owns only the display of one native attach client. Claude's
// reconnect loop leaves the alternate screen, prints to stderr, then enters it
// again. Defer that screen switch until the client actually exits, retaining the
// old frame while the same client reconnects. Input and native lifecycle are not
// involved. Unknown/oversized output fails open instead of hiding diagnostics.
// All methods run on the proxy's output goroutine.
type attachDisplay struct {
	alt, holding, repainting, synchronized, notice, passthrough bool
	control, deferred, frame, text                              []byte
	content                                                     bool
	since, lastData                                             time.Time
	size                                                        func() (rows, cols int)
}

func (d *attachDisplay) feed(data []byte, now time.Time) []byte {
	if d.passthrough {
		return data
	}
	d.lastData = now
	var out []byte
	for len(data) > 0 {
		if len(d.control) == 0 && data[0] != '\x1b' {
			n := bytes.IndexByte(data, '\x1b')
			if n < 0 {
				n = len(data)
			}
			out = append(out, d.accept(data[:n], false, now)...)
			data = data[n:]
			if d.passthrough {
				return append(out, data...)
			}
			continue
		}
		d.control = append(d.control, data[0])
		data = data[1:]
		if len(d.control) > 4096 {
			out = append(out, d.failOpen(d.control)...)
			out = append(out, data...)
			return out
		}
		if !attachControlComplete(d.control) {
			continue
		}
		out = append(out, d.accept(d.control, true, now)...)
		d.control = nil
		if d.passthrough {
			return append(out, data...)
		}
	}
	return out
}

// Do not interpret alternate-screen-looking bytes inside OSC/DCS/APC strings.
func attachControlComplete(b []byte) bool {
	if len(b) < 2 {
		return false
	}
	switch b[1] {
	case '[':
		return len(b) >= 3 && b[len(b)-1] >= 0x40 && b[len(b)-1] <= 0x7e
	case ']', 'P', '_', '^', 'X':
		return (b[1] == ']' && b[len(b)-1] == 7) || bytes.HasSuffix(b, []byte("\x1b\\"))
	case '(', ')', '*', '+':
		return len(b) >= 3
	default:
		return true
	}
}

func (d *attachDisplay) accept(b []byte, control bool, now time.Time) []byte {
	var s string
	if control {
		s = string(b)
	}
	if control && s == attachAltLeave && d.alt && !d.holding {
		d.holding, d.since = true, now
	}
	if !d.holding {
		if control && s == attachAltEnter {
			d.alt = true
		}
		return b
	}
	if control && attachTerminalQuery(b) {
		return b
	}
	d.deferred = append(d.deferred, b...)
	if len(d.deferred) > attachDisplayLimit {
		return d.failOpen(nil)
	}
	if control {
		switch s {
		case attachAltEnter:
			d.repainting, d.synchronized = true, false
			d.frame, d.text = nil, nil
			d.content = false
			return nil
		case attachAltLeave:
			d.repainting, d.synchronized = false, false
			d.frame, d.text = nil, nil
			d.content = false
			return nil
		}
	}
	if !d.repainting {
		return nil
	}
	if control {
		switch s {
		case attachSyncBegin:
			d.synchronized = true
			return nil
		case attachSyncEnd:
			if d.synchronized && d.content {
				return d.paint()
			}
			d.synchronized = false
			return nil
		}
	} else {
		d.observeText(b)
	}
	d.frame = append(d.frame, b...)
	return nil
}

func attachTerminalQuery(b []byte) bool {
	// Probes must reach the real terminal even while retaining a frame;
	// otherwise a client waiting for a terminal reply could deadlock.
	if bytes.HasPrefix(b, []byte("\x1b[")) {
		last := b[len(b)-1]
		return last == 'n' || last == 'c' || bytes.HasSuffix(b, []byte("$p")) || string(b) == "\x1b[?u"
	}
	return bytes.HasPrefix(b, []byte("\x1b]")) && bytes.Contains(b, []byte(";?"))
}

func (d *attachDisplay) idle(now time.Time) []byte {
	if !d.holding || d.passthrough {
		return nil
	}
	// Older native renderers do not delimit synchronized frames. Wait for a
	// quiet output boundary, but never paint the interim "Attaching…" screen.
	if d.repainting && !d.synchronized && len(d.control) == 0 && d.content && now.Sub(d.lastData) >= 50*time.Millisecond {
		return d.paint()
	}
	if d.notice || now.Sub(d.since) < time.Second {
		return nil
	}
	d.notice = true
	rows, cols := 32, 100
	if d.size != nil {
		rows, cols = d.size()
	}
	if rows < 1 || cols < 2 {
		return nil
	}
	msg := "Reconnecting... (Ctrl+Z to detach)"
	msg = msg[:min(len(msg), cols-1)]
	return []byte(fmt.Sprintf("\x1b7\x1b[0m\x1b[%d;1H\x1b[2K%s\x1b8", rows, msg))
}

func (d *attachDisplay) observeText(b []byte) {
	if d.content {
		return
	}
	// Recognize the interim native label across arbitrary reads, including
	// split UTF-8. Blank canvas padding must not crowd out the real replay.
	for _, c := range b {
		if len(d.text) == 0 && (c == ' ' || c == '\r' || c == '\n' || c == '\t') {
			continue
		}
		d.text = append(d.text, c)
		s := string(d.text)
		if s == "Attaching…" || s == "Attaching..." {
			d.text = nil
		} else if !strings.HasPrefix("Attaching…", s) && !strings.HasPrefix("Attaching...", s) {
			d.content = true
			return
		}
	}
}

func (d *attachDisplay) paint() []byte {
	// The host sees one synchronized repaint, including the native clear/home,
	// instead of a shell flash, placeholder and partial replay. Terminals which
	// do not implement synchronized output still keep their alternate screen.
	out := append([]byte(attachSyncBegin), d.frame...)
	out = append(out, attachSyncEnd...)
	d.holding, d.repainting, d.synchronized, d.notice = false, false, false, false
	d.deferred, d.frame, d.text = nil, nil, nil
	d.content = false
	return out
}

func (d *attachDisplay) failOpen(extra []byte) []byte {
	out := append([]byte(nil), d.deferred...)
	out = append(out, extra...)
	d.deferred, d.frame, d.text, d.control = nil, nil, nil, nil
	d.passthrough = true
	return out
}

func (d *attachDisplay) finish() []byte {
	// Replay the deferred native teardown/diagnostics on a real exit. Never
	// leave the caller stuck in the retained screen or hide an attach error.
	out := append([]byte(nil), d.deferred...)
	out = append(out, d.control...)
	if d.alt && !d.passthrough && (!d.holding || d.repainting) {
		out = append(out, (attachSyncEnd + "\x1b[?25h" + attachAltLeave)...)
	}
	return out
}
