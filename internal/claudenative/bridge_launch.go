package claudenative

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// StartBridgeHook completes promptly, including for events where native hook
// processing waits for command completion. The child registers before we exit.
func StartBridgeHook(ctx context.Context, executable, data, profile string, input io.Reader) error {
	body, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 1<<20 {
		return errors.New("hook input too large")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		writer.Close()
		return err
	}
	defer devnull.Close()
	cmd := exec.Command(executable, "claude-bridge", "--data", data, "--profile", profile)
	cmd.Env = append(os.Environ(), "POCKET_BRIDGE_LAUNCH=1")
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.ExtraFiles = []*os.File{writer}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		writer.Close()
		return err
	}
	writer.Close()
	result := make(chan error, 1)
	go func() {
		var reply bridgeReply
		e := json.NewDecoder(reader).Decode(&reply)
		if e == nil && !reply.Sent {
			e = errors.New(reply.Error)
		}
		result <- e
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err = <-result:
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = errors.New("native bridge registration timed out")
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	return cmd.Process.Release()
}

// Ready is sent only to the inherited pipe, never to stdout/model context.
var bridgeReadyOnce sync.Once

func BridgeReady(err error) {
	if os.Getenv("POCKET_BRIDGE_LAUNCH") != "1" {
		return
	}
	bridgeReadyOnce.Do(func() {
		f := os.NewFile(3, "bridge-ready")
		if f == nil {
			return
		}
		defer f.Close()
		reply := bridgeReply{Sent: err == nil}
		if err != nil {
			reply.Error = err.Error()
		}
		_ = json.NewEncoder(f).Encode(reply)
	})
}
