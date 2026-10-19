package codexnative

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"pocketagents/internal/core"
)

// The native writer lock is removed on release. Its inode lifetime identifies
// an external ownership generation without claiming that our daemon owns it.
func (p *Provider) queueGeneration(id string) string {
	held, err := p.writerHeld(id)
	if err != nil || !held {
		return ""
	}
	st, err := os.Stat(filepath.Join(p.profile.Home, "thread-writer-locks", id+".lock"))
	if err != nil {
		return ""
	}
	raw, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%d", id, raw.Dev, raw.Ino, st.ModTime().UnixNano())))
	return "codex-queue:" + hex.EncodeToString(sum[:16])
}
func (p *Provider) offerExternalQueue(ctx context.Context, r *rpcClient, s core.Session) core.Session {
	if s.ExecutionState != "alive" || s.ParentID != "" {
		return s
	}
	generation := p.queueGeneration(s.NativeID)
	if generation == "" {
		return s
	}
	// Read-only feature probe; older daemons retain their truthful capability.
	var out json.RawMessage
	if r.call(ctx, "thread/queue/list", map[string]any{"threadId": s.NativeID, "limit": 1}, &out) != nil {
		return s
	}
	if p.queueGeneration(s.NativeID) != generation {
		return s
	}
	s.ExecutionID = generation
	s.Capabilities.Send = true
	s.Capabilities.InputMode = "queue"
	s.Capabilities.Reason = "Messages enter the native thread queue for the current owner to consume when idle. No resume or second writer. Approvals stay in the original terminal/app."
	return s
}
func (p *Provider) sendExternalQueue(ctx context.Context, r *rpcClient, a core.Action) (core.Result, error) {
	res := core.Result{ActionID: a.ID, NativeID: a.NativeID, ExecutionID: a.ExecutionID, Status: "rejected"}
	if a.ExecutionID == "" || p.queueGeneration(a.NativeID) != a.ExecutionID {
		return res, errors.New("native writer changed or exited; refresh before queueing")
	}
	var out json.RawMessage
	if err := r.call(ctx, "thread/queue/add", map[string]any{"threadId": a.NativeID, "clientUserMessageId": a.ID, "input": nativeInput(a)}, &out); err != nil {
		return delivery(res, err)
	}
	res.Status = "accepted"
	res.Message = "已进入原生会话队列，当前实例空闲后处理。没有恢复会话或启动其他实例；若原实例先退出，消息会保留在原生队列中。"
	res.Data, _ = json.Marshal(map[string]any{"delivery": "native_queue", "native": json.RawMessage(out)})
	return res, nil
}

func nativeInput(a core.Action) []map[string]any {
	input := []map[string]any{{"type": "text", "text": a.Text}}
	for _, f := range a.InputFiles {
		if f.MIME == "image/png" || f.MIME == "image/jpeg" || f.MIME == "image/webp" || f.MIME == "image/gif" {
			input = append(input, map[string]any{"type": "localImage", "path": f.Path})
		}
	}
	return input
}
