package codexnative

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pocketagents/internal/core"
)

type observedTurn struct {
	path     string
	info     os.FileInfo
	offset   int64
	turnID   string
	state    string
	at       time.Time
	verified bool
}

// External owners do not publish live RPC status to our daemon. Read only their
// append-only lifecycle events; never resume them or infer approval permissions.
func (p *Provider) observeExternalTurn(s core.Session, path string) core.Session {
	if s.ExecutionState != "alive" || path == "" {
		return s
	}
	root, err := filepath.EvalSymlinks(p.profile.Home)
	if err != nil {
		return s
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return s
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || (!strings.HasPrefix(rel, "sessions"+string(filepath.Separator)) && !strings.HasPrefix(rel, "archived_sessions"+string(filepath.Separator))) {
		return s
	}
	f, err := os.Open(real)
	if err != nil {
		return s
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return s
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.observedTurns == nil {
		p.observedTurns = map[string]observedTurn{}
	}
	prev := p.observedTurns[s.NativeID]
	cached := prev.path == real && prev.info != nil && os.SameFile(prev.info, info) && info.Size() >= prev.offset
	if !cached || (info.Size() == prev.info.Size() && !info.ModTime().Equal(prev.info.ModTime())) {
		prev = observedTurn{path: real}
	}
	if _, err := f.Seek(prev.offset, io.SeekStart); err != nil {
		return s
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 16*1024*1024)
	// Leave an unfinished last JSON line for the next poll.
	scan.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i+1], nil
		}
		return 0, nil, nil
	})
	for scan.Scan() {
		line := scan.Bytes()
		prev.offset += int64(len(line))
		var row struct {
			Timestamp time.Time `json:"timestamp"`
			Type      string    `json:"type"`
			Payload   struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				TurnID string `json:"turn_id"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if row.Type == "session_meta" {
			prev.verified = row.Payload.ID == s.NativeID
			if !prev.verified {
				delete(p.observedTurns, s.NativeID)
				return s
			}
		}
		if !prev.verified || row.Type != "event_msg" || row.Payload.TurnID == "" {
			continue
		}
		state := ""
		switch row.Payload.Type {
		case "task_started":
			state = "working"
		case "task_complete":
			state = "completed"
		case "turn_aborted":
			state = "interrupted"
		}
		if state == "" {
			continue
		}
		if state != "working" && prev.turnID != "" && prev.turnID != row.Payload.TurnID {
			continue
		}
		prev.turnID = row.Payload.TurnID
		prev.state = state
		prev.at = row.Timestamp
	}
	if scan.Err() != nil {
		delete(p.observedTurns, s.NativeID)
		return s
	}
	prev.info = info
	p.observedTurns[s.NativeID] = prev
	if prev.verified && prev.state != "" {
		s.TurnID = prev.turnID
		s.TurnState = prev.state
		s.Error = ""
		if prev.at.After(s.UpdatedAt) {
			s.UpdatedAt = prev.at
		}
		s.Capabilities.Reason = "A live native writer outside the shared daemon owns this thread. Turn status comes from its native history; control stays in the original app or terminal."
	}
	return s
}
