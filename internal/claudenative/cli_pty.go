package claudenative

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// The native launcher must see a terminal on every standard stream. In
// particular, piping stdout to capture its acknowledgement suppresses the
// first-workspace trust dialog. This proxy only exists during --bg startup;
// attach uses the same proxy with pipe stdin to suppress the global picker.
func runCLILauncherPTY(cmd *exec.Cmd, stdin, stdout *os.File) ([]byte, error) {
	return runCLIProxy(cmd, stdin, stdout, false, nil)
}

func runCLIProxy(cmd *exec.Cmd, stdin, stdout *os.File, singleTarget bool, intent *exitInput) (output []byte, err error) {
	if !term.IsTerminal(int(stdin.Fd())) {
		return nil, errors.New("interactive Claude startup requires a terminal on stdin; use an interactive terminal, or an explicit native --print/--bg command")
	}
	size, err := pty.GetsizeFull(stdin)
	if err != nil {
		return nil, err
	}
	if size.Rows == 0 || size.Cols == 0 {
		size = &pty.Winsize{Rows: 32, Cols: 100}
	}
	previous, err := term.MakeRaw(int(stdin.Fd()))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, term.Restore(int(stdin.Fd()), previous)) }()
	var f, input *os.File
	if singleTarget {
		f, input, err = startSingleTargetPTY(cmd, size)
	} else {
		f, err = pty.StartWithSize(cmd, size)
		input = f
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if input != f {
		defer input.Close()
	}
	inputFD, masterFD, writeFD := int(stdin.Fd()), int(f.Fd()), int(input.Fd())
	if err = unix.SetNonblock(writeFD, true); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	// Nonblocking reads/writes let both pumps stop before native attach,
	// including when the detached worker retains the slave descriptor.
	if err = unix.SetNonblock(masterFD, true); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	done := make(chan struct{})
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		defer func() {
			if singleTarget {
				select {
				case <-done:
					return
				default:
				}
				// Losing the caller's terminal ends this client only. EOF is never /exit.
				_ = input.Close()
				_ = cmd.Process.Kill()
			}
		}()
		buf := make([]byte, 4096)
		fds := []unix.PollFd{{Fd: int32(inputFD), Events: unix.POLLIN}}
		for {
			select {
			case <-done:
				return
			default:
			}
			// A permanent io.Copy(stdin) could keep consuming keyboard input
			// after startup and race the native attach process. Polling lets us
			// stop and join this reader before handing the terminal back.
			n, e := unix.Poll(fds, 50)
			if e == unix.EINTR {
				continue
			}
			if e != nil {
				return
			}
			if n == 0 {
				continue
			}
			select {
			case <-done:
				return
			default:
			}
			if fds[0].Revents&unix.POLLIN == 0 {
				return
			}
			n, e = unix.Read(inputFD, buf)
			if e != nil || n == 0 {
				return
			}
			for pending := buf[:n]; len(pending) > 0; {
				select {
				case <-done:
					return
				default:
				}
				sentAt := time.Now().UTC()
				written, writeErr := unix.Write(writeFD, pending)
				if written > 0 {
					if intent != nil {
						intent.observe(pending[:written], sentAt)
					}
					pending = pending[written:]
				}
				if writeErr == unix.EINTR {
					continue
				}
				if writeErr == unix.EAGAIN {
					_, _ = unix.Poll([]unix.PollFd{{Fd: int32(writeFD), Events: unix.POLLOUT}}, 50)
					continue
				}
				if writeErr != nil || written == 0 {
					return
				}
			}
		}
	}()
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	resizeDone := make(chan struct{})
	go func() {
		defer close(resizeDone)
		for {
			select {
			case <-done:
				return
			case <-resize:
				_ = pty.InheritSize(stdin, f)
			}
		}
	}()
	outputDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 8192)
		var copyErr error
		var ended time.Time
		var display *attachDisplay
		if singleTarget && term.IsTerminal(int(stdout.Fd())) {
			display = &attachDisplay{size: func() (int, int) {
				size, err := pty.GetsizeFull(stdin)
				if err != nil {
					return 0, 0
				}
				return int(size.Rows), int(size.Cols)
			}}
		}
		write := func(b []byte) {
			if copyErr == nil && len(b) > 0 {
				_, copyErr = stdout.Write(b)
			}
		}
		fds := []unix.PollFd{{Fd: int32(masterFD), Events: unix.POLLIN}}
		defer func() {
			if display != nil {
				write(display.finish())
			}
			outputDone <- copyErr
		}()
		for {
			select {
			case <-done:
				if ended.IsZero() {
					ended = time.Now()
				}
			default:
			}
			if !ended.IsZero() && time.Since(ended) > 250*time.Millisecond {
				return
			}
			n, pollErr := unix.Poll(fds, 50)
			if pollErr == unix.EINTR {
				continue
			}
			if pollErr != nil {
				copyErr = errors.Join(copyErr, pollErr)
				return
			}
			if n == 0 {
				if display != nil {
					write(display.idle(time.Now()))
				}
				if !ended.IsZero() {
					return
				}
				continue
			}
			n, readErr := unix.Read(masterFD, buf)
			if n > 0 {
				output = append(output, buf[:n]...)
				if len(output) > 256*1024 {
					output = append([]byte(nil), output[len(output)-256*1024:]...)
				}
				if display != nil {
					write(display.feed(buf[:n], time.Now()))
				} else {
					write(buf[:n])
				}
			}
			if readErr == unix.EAGAIN || readErr == unix.EINTR {
				continue
			}
			if readErr != nil {
				if !errors.Is(readErr, syscall.EIO) {
					copyErr = errors.Join(copyErr, readErr)
				}
				return
			}
			if n == 0 {
				return
			}
		}
	}()
	err = cmd.Wait()
	close(done)
	<-inputDone
	signal.Stop(resize)
	<-resizeDone
	// The output reader drains the acknowledgement and exits even if a
	// detached worker briefly retains the slave descriptor.
	err = errors.Join(err, <-outputDone)
	return output, err
}

// Native attach returns to the global agents picker only when both stdin and
// stdout are TTYs. Pipe stdin keeps this client scoped to its target; stdout
// remains a PTY for native rendering and resize. Verified with Claude 2.1.302.
func startSingleTargetPTY(cmd *exec.Cmd, size *pty.Winsize) (*os.File, *os.File, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()
	cmd.Stdin = reader
	f, err := pty.StartWithAttrs(cmd, size, &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 1})
	if err != nil {
		writer.Close()
		return nil, nil, err
	}
	return f, writer, nil
}
