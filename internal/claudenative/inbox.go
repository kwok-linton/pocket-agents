package claudenative

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pocketagents/internal/core"
)

type inboxRegistry struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	Protocol   int    `json:"peerProtocol"`
	Socket     string `json:"messagingSocketPath"`
	ProcStart  string `json:"procStart"`
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
}

func ownedPrivate(path string, socket bool) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(sys.Uid) != os.Getuid() || st.Mode().Perm()&0077 != 0 || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("native inbox must be private and owned by current user")
	}
	if socket && st.Mode()&os.ModeSocket == 0 {
		return errors.New("native inbox is not a socket")
	}
	return nil
}
func (p *Provider) inbox(a agent) (inboxRegistry, error) {
	var v inboxRegistry
	b, err := os.ReadFile(filepath.Join(p.profile.Home, "sessions", strconv.Itoa(a.PID)+".json"))
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	if v.PID != a.PID || v.SessionID != a.SessionID || v.Protocol != 1 || !filepath.IsAbs(v.Socket) || filepath.Clean(v.Socket) != v.Socket {
		return v, errors.New("native inbox identity/protocol mismatch")
	}
	// Bind the registry to the kernel process start, not a reusable PID.
	start := ""
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", a.PID))
		if err == nil {
			fields := strings.Fields(string(b)[strings.LastIndex(string(b), ")")+1:])
			if len(fields) > 19 {
				start = fields[19]
			}
		}
	} else {
		cmd := exec.Command("ps", "-p", strconv.Itoa(a.PID), "-o", "lstart=")
		cmd.Env = append(os.Environ(), "TZ=UTC")
		b, _ := cmd.Output()
		start = strings.TrimSpace(string(b))
	}
	if v.ProcStart == "" || strings.Join(strings.Fields(v.ProcStart), " ") != strings.Join(strings.Fields(start), " ") || executionID(a) == "" {
		return v, errors.New("native inbox process changed")
	}
	if err = ownedPrivate(filepath.Dir(v.Socket), false); err != nil {
		return v, err
	}
	return v, ownedPrivate(v.Socket, true)
}

type phoneDelivery struct{ NativeID, MessageID, Text string }

func (p *Provider) deliveryDir() string { return filepath.Join(p.profile.DataDir, "phone-deliveries") }
func (p *Provider) displayPhoneDeliveries(h *core.History, id string) {
	if p.profile.DataDir == "" {
		return
	}
	files, _ := filepath.Glob(filepath.Join(p.deliveryDir(), "*.json"))
	byID := map[string]string{}
	for _, path := range files {
		var d phoneDelivery
		b, e := os.ReadFile(path)
		if e == nil && json.Unmarshal(b, &d) == nil && d.NativeID == id {
			byID[d.MessageID] = d.Text
		}
	}
	for i := range h.Messages {
		if text, ok := byID[h.Messages[i].ID]; ok {
			h.Messages[i].Role = "user"
			h.Messages[i].Kind = "phone_message"
			h.Messages[i].Text = text
		}
	}
}
func (p *Provider) sendInbox(ctx context.Context, a core.Action, current agent) (core.Result, error) {
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, ExecutionID: a.ExecutionID, Status: "rejected"}
	if strings.TrimSpace(a.Text) == "" || len(a.Text) > 64<<10 {
		return r, errors.New("message must contain 1–65536 bytes")
	}
	v, err := p.inbox(current)
	if err != nil {
		return r, err
	}
	// Native peer delivery preserves the receiver's inbound policy. It does
	// not impersonate a terminal keystroke or approve a native permission.
	digest := sha256.Sum256([]byte(a.ID))
	marker := "[Pocket Agents message " + hex.EncodeToString(digest[:]) + "]\n"
	body := marker + a.Text
	started := time.Now()
	if bridge, bridgeErr := p.bridge(a.ExecutionID); bridgeErr == nil {
		if err = sendThroughBridge(ctx, bridge, a, body); err != nil {
			r.Status = "unknown"
			r.Message = "会话桥接未确认投递。请核对原生会话，系统不会改走其他通道或自动重发。"
			return r, nil
		}
	} else {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", v.Socket)
		if err != nil {
			return r, err
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		enc := json.NewEncoder(conn)
		keyHash := sha256.Sum256([]byte(v.Socket))
		keyPath := filepath.Join(p.profile.Home, "sessions", fmt.Sprintf("%d.%x.key", v.PID, keyHash))
		if ownedPrivate(keyPath, false) == nil {
			var key struct {
				PeerToken string `json:"peerToken"`
				ProcStart string `json:"procStart"`
			}
			b, e := os.ReadFile(keyPath)
			if e == nil && json.Unmarshal(b, &key) == nil && key.ProcStart == v.ProcStart && key.PeerToken != "" {
				if err = enc.Encode(map[string]string{"type": "auth", "token": key.PeerToken}); err != nil {
					return r, err
				}
			}
		}
		// Recheck generation immediately before writing user content.
		if executionID(current) != a.ExecutionID {
			return r, errors.New("native execution changed before delivery")
		}
		frame := map[string]any{"msgV": 1, "type": "user", "msg_id": a.ID, "session_id": a.NativeID, "priority": "next", "message": map[string]string{"role": "user", "content": body}}
		if err = enc.Encode(frame); err != nil {
			r.Status = "unknown"
			r.Message = "Native socket write failed; delivery is unknown. Do not retry blindly."
			return r, nil
		}
		if unix, ok := conn.(*net.UnixConn); ok {
			_ = unix.CloseWrite()
		}
	}
	r.Status = "submitted"
	r.Message = "尚未确认 Claude 收到。原生终端可能正在等待跨会话投递确认；若看到 Held message，请在终端选择 Deliver 或 Deny。请勿重复发送，确认后点击查询结果。"
	r.Data = json.RawMessage(`{"delivery":"native_inbox"}`)
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		path, pathErr := p.findPath(a.NativeID)
		var h core.History
		e := pathErr
		if e == nil {
			h, e = readHistory(ctx, path, "", 30)
		}
		if e == nil {
			for _, m := range h.Messages {
				if (m.Role == "user" || m.Kind == "peer_message") && !m.At.Before(started) && strings.Contains(m.Text, body) {
					r.Status = "accepted"
					r.Message = "Native transcript confirms delivery to the same running process. Peer messages cannot approve permissions."
					if p.profile.DataDir != "" {
						_ = os.MkdirAll(p.deliveryDir(), 0700)
						b, _ := json.Marshal(phoneDelivery{a.NativeID, m.ID, a.Text})
						_ = os.WriteFile(filepath.Join(p.deliveryDir(), hex.EncodeToString(digest[:])+".json"), b, 0600)
					}
					return r, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return r, nil
		case <-timer.C:
			return r, nil
		case <-tick.C:
		}
	}
}

// Persisted with the action result so a slow native terminal can be confirmed
// later without sending again. Missing timestamps must never match old history.
type terminalDelivery struct {
	Delivery string    `json:"delivery"`
	SentAt   time.Time `json:"sentAt"`
}

// ReconcileResult is read-only: a successful socket write alone is never delivery.
// The unique action marker also reconciles messages approved after sendInbox times out.
func (p *Provider) ReconcileResult(ctx context.Context, a core.Action, previous core.Result) (core.Result, error) {
	if a.Kind == "terminate" && (previous.Status == "submitted" || previous.Status == "unknown") {
		previous.Status, previous.Message = "unknown", stopUnconfirmed
		if executionExited(a.NativeID, a.ExecutionID) {
			previous.Status, previous.Message = "accepted", stopConfirmed
		}
		return previous, nil
	}
	if a.Kind == "interrupt" && previous.Status == "submitted" {
		previous.Status = "unknown"
		previous.Message = "停止请求的结果未确认；原生接口没有停止回执，不会自动重试。请查看当前会话状态，可收起此提示继续操作。"
		return previous, nil
	}
	if a.Kind != "send" || (previous.Status != "submitted" && previous.Status != "unknown") {
		return previous, nil
	}
	path, err := p.findPath(a.NativeID)
	if err != nil {
		return previous, err
	}
	digest := sha256.Sum256([]byte(a.ID))
	marker := "[Pocket Agents message " + hex.EncodeToString(digest[:]) + "]\n"
	var terminal terminalDelivery
	_ = json.Unmarshal(previous.Data, &terminal)
	isTerminal := terminal.Delivery == "native_terminal"
	// An old unresolved receipt must not repeatedly parse a multi-megabyte
	// transcript. Cache misses only while its native file revision is unchanged.
	revision, revisionErr := p.HistoryRevision(ctx, a.NativeID)
	actionJSON, _ := json.Marshal(a)
	stamp := fmt.Sprintf("%s:%x:%x", revision, sha256.Sum256(actionJSON), sha256.Sum256(previous.Data))
	p.receiptMu.Lock()
	cached := revisionErr == nil && p.receiptMisses[a.ID] == stamp
	p.receiptMu.Unlock()
	if cached {
		return previous, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return previous, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return previous, err
		}
		// Inbox receipts have a unique marker; skip unrelated records without
		// JSON decoding. Search the entire file, not just the latest 4000 rows.
		if !isTerminal && !bytes.Contains(scan.Bytes(), []byte(strings.TrimSpace(marker))) {
			continue
		}
		m, valid := parseMessage(scan.Bytes())
		if !valid || (m.Role != "user" && m.Kind != "peer_message") {
			continue
		}
		if isTerminal {
			if terminal.SentAt.IsZero() || m.Role != "user" || !terminalTextMatches(m.Text, a.Text) || m.At.Before(terminal.SentAt) {
				continue
			}
		} else if !strings.Contains(m.Text, marker) {
			continue
		}
		previous.Status = "accepted"
		previous.Message = "原生对话记录已确认收到这条手机消息。"
		if !isTerminal {
			previous.Data = json.RawMessage(`{"delivery":"native_inbox"}`)
		}
		if p.profile.DataDir != "" {
			if err := os.MkdirAll(p.deliveryDir(), 0700); err != nil {
				return previous, err
			}
			b, _ := json.Marshal(phoneDelivery{a.NativeID, m.ID, a.Text})
			if err := os.WriteFile(filepath.Join(p.deliveryDir(), hex.EncodeToString(digest[:])+".json"), b, 0600); err != nil {
				return previous, err
			}
		}
		return previous, nil
	}
	if err := scan.Err(); err != nil {
		return previous, err
	}
	if revisionErr == nil {
		if after, err := p.HistoryRevision(ctx, a.NativeID); err == nil && after == revision {
			p.receiptMu.Lock()
			if len(p.receiptMisses) >= 64 {
				p.receiptMisses = nil
			}
			if p.receiptMisses == nil {
				p.receiptMisses = map[string]string{}
			}
			p.receiptMisses[a.ID] = stamp
			p.receiptMu.Unlock()
		}
	}
	if isTerminal {
		previous.Message = "原生终端尚未确认收到此消息，可能仍在忙或等待输入。系统不会自动重发；请查看原生终端后再决定是否继续。"
		return previous, nil
	}
	previous.Message = "尚未在原生对话中确认收到。若终端显示 Held message，请处理 Deliver / Deny 后再查询；也可能已拒绝、过期或仍在排队。系统不会自动重发。"
	return previous, nil
}

// Claude wraps expanded terminal pastes in paired native pasted_content tags.
// Match the entire original text after removing only that envelope, not whitespace
// or other XML-like content. Time and role checks remain at the call site.
var pastedEnvelope = regexp.MustCompile(`(?s)\n\n<pasted_content id="([a-zA-Z0-9_-]+)">\n(.*?)\n</pasted_content id="([a-zA-Z0-9_-]+)">\n`)

func terminalTextMatches(recorded, sent string) bool {
	if recorded == sent {
		return true
	}
	if strings.Contains(sent, "<pasted_content") || strings.Contains(sent, "</pasted_content") {
		return false
	}
	normalized := pastedEnvelope.ReplaceAllStringFunc(recorded, func(envelope string) string {
		parts := pastedEnvelope.FindStringSubmatch(envelope)
		if parts[1] != parts[3] {
			return envelope
		}
		return parts[2]
	})
	return normalized == sent
}
