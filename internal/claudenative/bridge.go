package claudenative

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pocketagents/internal/config"
	"pocketagents/internal/core"
)

type bridgeRecord struct {
	NativeID    string `json:"nativeId"`
	ExecutionID string `json:"executionId"`
	Socket      string `json:"socket"`
	PID         int    `json:"pid"`
}
type bridgeRequest struct{ NativeID, ExecutionID, ID, Body string }
type bridgeReply struct {
	Sent  bool
	Error string
}

func bridgeKey(generation string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(generation)))[:24]
}
func (p *Provider) bridgePath(generation string) string {
	return filepath.Join(p.profile.DataDir, "bridges", bridgeKey(generation)+".json")
}
func (p *Provider) bridge(generation string) (bridgeRecord, error) {
	var record bridgeRecord
	if p.profile.DataDir == "" {
		return record, errors.New("profile data missing")
	}
	path := p.bridgePath(generation)
	if err := ownedPrivate(path, false); err != nil {
		return record, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err = json.Unmarshal(b, &record); err != nil {
		return record, err
	}
	if record.ExecutionID != generation || record.PID <= 0 {
		return record, errors.New("bridge generation mismatch")
	}
	if err = ownedPrivate(filepath.Dir(record.Socket), false); err != nil {
		return record, err
	}
	return record, ownedPrivate(record.Socket, true)
}

// Only a real native child may register. Tokens come from the official hook
// environment, remain in that child's memory, and are never stored or sent to Hub.
func nativeAncestor(pid int) bool {
	current := os.Getpid()
	for i := 0; i < 20 && current > 1; i++ {
		b, err := exec.Command("ps", "-p", strconv.Itoa(current), "-o", "ppid=").Output()
		if err != nil {
			return false
		}
		parent, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			return false
		}
		if parent == pid {
			return true
		}
		if parent == current {
			return false
		}
		current = parent
	}
	return false
}

// RunBridgeHook runs as an async command hook. It prints no model context and
// cannot approve permissions. A native explicit inbound hold/refuse still wins.
func RunBridgeHook(ctx context.Context, profile core.Profile, input io.Reader) error {
	var event struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&event); err != nil {
		return err
	}
	socket, token := os.Getenv("CLAUDE_CODE_MESSAGING_SOCKET"), os.Getenv("CLAUDE_CODE_MESSAGING_TOKEN")
	if socket == "" || token == "" || !nativeIDPattern.MatchString(event.SessionID) {
		return errors.New("native hook messaging context unavailable")
	}
	p := &Provider{profile: profile}
	if p.bridgeDisabled() {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(profile.Home, "sessions", "*.json"))
	var current agent
	var registry inboxRegistry
	for _, path := range paths {
		var candidate inboxRegistry
		b, e := os.ReadFile(path)
		if e == nil && json.Unmarshal(b, &candidate) == nil && candidate.Socket == socket && candidate.SessionID == event.SessionID {
			a := agent{PID: candidate.PID, SessionID: candidate.SessionID}
			if v, e := p.inbox(a); e == nil && nativeAncestor(a.PID) {
				current = a
				registry = v
				break
			}
		}
	}
	if current.PID == 0 {
		return errors.New("hook does not belong to the registered native process")
	}
	generation := executionID(current)
	dir := filepath.Dir(p.bridgePath(generation))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := ownedPrivate(dir, false); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, bridgeKey(generation)+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	relayPath := filepath.Join(filepath.Dir(socket), "pocket-"+bridgeKey(generation)+".sock")
	if len(relayPath) > 100 {
		relayDir := filepath.Join("/tmp", fmt.Sprintf("pocket-bridges-%d", os.Getuid()))
		if err = os.MkdirAll(relayDir, 0700); err != nil {
			return err
		}
		if err = ownedPrivate(relayDir, false); err != nil {
			return err
		}
		relayPath = filepath.Join(relayDir, bridgeKey(generation)+".sock")
	}
	if _, err = os.Lstat(relayPath); err == nil {
		if err = ownedPrivate(relayPath, true); err != nil {
			return err
		}
		if err = os.Remove(relayPath); err != nil {
			return err
		}
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: relayPath, Net: "unix"})
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(relayPath, 0600); err != nil {
		return err
	}
	record := bridgeRecord{current.SessionID, generation, relayPath, os.Getpid()}
	b, _ := json.Marshal(record)
	if err = config.Atomic(p.bridgePath(generation), b); err != nil {
		return err
	}
	defer os.Remove(p.bridgePath(generation))
	BridgeReady(nil)
	for ctx.Err() == nil {
		if p.bridgeDisabled() {
			return nil
		}
		// /clear, /resume, exit and PID reuse revoke the bridge automatically.
		v, e := p.inbox(current)
		if e != nil || v.ProcStart != registry.ProcStart || executionID(current) != generation {
			return nil
		}
		_ = listener.SetDeadline(time.Now().Add(2 * time.Second))
		conn, e := listener.AcceptUnix()
		if e != nil {
			if timeout, ok := e.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return e
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		var request bridgeRequest
		reply := bridgeReply{}
		if e = json.NewDecoder(io.LimitReader(conn, 256<<10)).Decode(&request); e != nil {
			reply.Error = "invalid bridge request"
		} else if request.NativeID != current.SessionID || request.ExecutionID != generation || request.ID == "" || strings.TrimSpace(request.Body) == "" || len(request.Body) > 66<<10 {
			reply.Error = "bridge target or message mismatch"
		} else if v, e = p.inbox(current); e != nil || v.ProcStart != registry.ProcStart {
			reply.Error = "native instance changed"
		} else {
			reply = postFromChild(ctx, socket, token, request)
		}
		_ = json.NewEncoder(conn).Encode(reply)
		_ = conn.Close()
	}
	return nil
}
func postFromChild(ctx context.Context, socket, token string, request bridgeRequest) bridgeReply {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return bridgeReply{Error: "native inbox unavailable"}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	enc := json.NewEncoder(conn)
	if err = enc.Encode(map[string]string{"type": "auth", "token": token}); err != nil {
		return bridgeReply{Error: "native authentication write failed"}
	}
	frame := map[string]any{"msgV": 1, "type": "user", "msg_id": request.ID, "session_id": request.NativeID, "priority": "next", "message": map[string]string{"role": "user", "content": request.Body}}
	if err = enc.Encode(frame); err != nil {
		return bridgeReply{Error: "native delivery uncertain"}
	}
	return bridgeReply{Sent: true}
}
func sendThroughBridge(ctx context.Context, record bridgeRecord, a core.Action, body string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", record.Socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	if err = json.NewEncoder(conn).Encode(bridgeRequest{a.NativeID, a.ExecutionID, a.ID, body}); err != nil {
		return err
	}
	var reply bridgeReply
	if err = json.NewDecoder(conn).Decode(&reply); err != nil {
		return err
	}
	if !reply.Sent {
		return errors.New(reply.Error)
	}
	return nil
}
