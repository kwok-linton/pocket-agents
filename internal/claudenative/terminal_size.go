package claudenative

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

var processTerminal = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\S+)\s+(.+)$`)
var terminalDevice = regexp.MustCompile(`^(?:ttys?[A-Za-z0-9]+|pts/[0-9]+)$`)

// Claude's background renderer broadcasts one screen to every attach client.
// A phone must not resize it to 32x100 while a desktop is using another size.
// Inspect only matching native attach clients; never read from their terminals.
func desktopAttachTerminals(processes, binary string, a agent, parent int) []string {
	var terminals []string
	for _, line := range strings.Split(processes, "\n") {
		m := processTerminal.FindStringSubmatch(line)
		if m == nil || m[2] == strconv.Itoa(parent) || !terminalDevice.MatchString(m[3]) {
			continue
		}
		if m[4] != binary+" attach "+a.ID && (a.SessionID == "" || m[4] != binary+" attach "+a.SessionID) {
			continue
		}
		terminals = append(terminals, filepath.Join("/dev", m[3]))
	}
	return terminals
}

func desktopTerminalSize(ctx context.Context, binary string, a agent) *pty.Winsize {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	// Do not log this output: unrelated process arguments may contain secrets.
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,tty=,args=").Output()
	if err != nil {
		return nil
	}
	return desktopSizeFromProcesses(string(out), binary, a, os.Getpid())
}

func desktopSizeFromProcesses(processes, binary string, a agent, parent int) *pty.Winsize {
	for _, path := range desktopAttachTerminals(processes, binary, a, parent) {
		f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOCTTY, 0)
		if err != nil {
			continue
		}
		size, err := pty.GetsizeFull(f)
		_ = f.Close()
		if err == nil && size.Rows > 0 && size.Rows <= 200 && size.Cols > 0 && size.Cols <= 500 {
			return size
		}
	}
	return nil
}

func (a *attachment) resize(size *pty.Winsize) error {
	if size == nil {
		return nil // Keep the last size when the desktop disconnects.
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dead {
		return nil
	}
	if a.size.Rows == size.Rows && a.size.Cols == size.Cols {
		return nil
	}
	if err := pty.Setsize(a.file, size); err != nil {
		return err
	}
	a.size = *size
	return nil
}

// A background worker shares its renderer between attached clients. A stale
// phone PTY can keep that renderer wide after the desktop is split in half.
// Follow the desktop for the lifetime of this client, even when no browser is
// polling it. Only explicit, live attachments run this loop, not discovered
// sessions. Do not hold the provider's controller lock during process lookup.
func (a *attachment) followSize(ctx context.Context, readSize func(context.Context) *pty.Winsize) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			if ctx.Err() != nil || !a.connected() {
				return
			}
			size := readSize(ctx)
			if ctx.Err() != nil {
				return
			}
			// Missing desktops and transient lookup failures keep the last size;
			// they must not reset it to the phone default or reconnect a client.
			_ = a.resize(size)
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return done
}

func (a *attachment) view() (string, string, int64) {
	a.mu.Lock()
	raw := append([]byte(nil), a.output...)
	size := a.size
	cursor := a.offset + int64(len(a.output))
	a.mu.Unlock()
	screen, suggestion := terminalViewSize(raw, int(size.Rows), int(size.Cols))
	return screen, suggestion, cursor
}
