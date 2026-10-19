package claudenative

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pocketagents/internal/core"
)

// Claude documents JSONL location but explicitly does not promise its schema.
// This read-only fallback supports the 2.1.193/2.1.302 records exercised in M0.
// Unknown records are skipped; no native files are ever edited.
type record struct {
	Origin struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Type              string `json:"type"`
	UUID              string `json:"uuid"`
	SessionID         string `json:"sessionId"`
	Cwd               string `json:"cwd"`
	Timestamp         string `json:"timestamp"`
	IsSidechain       bool   `json:"isSidechain"`
	IsMeta            bool   `json:"isMeta"`
	IsAPIErrorMessage bool   `json:"isApiErrorMessage"`
	IsCompactSummary  bool   `json:"isCompactSummary"`
	CustomTitle       string `json:"customTitle"`
	AITitle           string `json:"aiTitle"`
	Summary           string `json:"summary"`
	PromptID          string `json:"promptId"`
	PromptSource      string `json:"promptSource"`
	ScheduledTaskID   string `json:"scheduledTaskId"`
	Message           struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
	} `json:"message"`
}

func contentText(raw json.RawMessage) (string, string) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, "message"
	}
	var blocks []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
		Content  json.RawMessage `json:"content"`
		Thinking string          `json:"thinking"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", ""
	}
	var parts []string
	kind := "message"
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "tool_use":
			parts = append(parts, b.Name+" "+string(b.Input))
			kind = "tool"
		case "tool_result":
			t, _ := contentText(b.Content)
			parts = append(parts, t)
			kind = "tool_result"
		case "thinking": /* Native private reasoning is not needed in the mobile transcript. */
		}
	}
	return strings.Join(parts, "\n"), kind
}
func parseMessage(line []byte) (core.Message, bool) {
	var r record
	if json.Unmarshal(line, &r) != nil || r.IsSidechain {
		return core.Message{}, false
	}
	if r.Type != "user" && r.Type != "assistant" && r.Type != "summary" {
		return core.Message{}, false
	}
	text, kind := contentText(r.Message.Content)
	role := r.Type
	if kind == "tool_result" {
		role = "tool"
	}
	if r.Type == "summary" {
		text = r.Summary
		kind = "compaction"
		role = "system"
	}
	if r.Type == "user" && kind != "tool_result" {
		switch {
		case r.Origin.Kind == "peer":
			role, kind = "system", "peer_message"
		case r.ScheduledTaskID != "" && r.PromptSource == "system":
			role, kind = "system", "scheduled_trigger"
		case r.IsCompactSummary:
			role, kind = "system", "compaction"
		case r.IsMeta || r.PromptSource == "system":
			role, kind = "system", "native_context"
		}
		// Older native logs may omit source flags. Recognize only native
		// envelope prefixes; an explicitly typed/queued message stays user text.
		if r.PromptSource == "" || role == "system" {
			trimmed := strings.TrimSpace(text)
			for _, tag := range []string{"task-notification", "teammate-message", "system-reminder", "local-command-caveat", "local-command-stdout", "local-command-stderr", "command-name", "command-message", "bash-input", "bash-stdout", "bash-stderr"} {
				if strings.HasPrefix(trimmed, "<"+tag+">") || strings.HasPrefix(trimmed, "<"+tag+" ") {
					role, kind = "system", "native_context"
					if tag == "task-notification" || tag == "teammate-message" {
						kind = "background_notification"
					}
					break
				}
			}
		}
	}
	if text == "" {
		return core.Message{}, false
	}
	id := r.UUID
	if kind == "tool" {
		var blocks []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if json.Unmarshal(r.Message.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "tool_use" && b.ID != "" {
					id += ":" + b.ID
				}
			}
		}
	}
	if id == "" {
		h := sha256.Sum256(line)
		id = hex.EncodeToString(h[:12])
	}
	at, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
	return core.Message{ID: id, Role: role, Text: text, Kind: kind, At: at}, true
}

// promptTitle is a display-only fallback. Native control records and injected
// context may have role=user, but are not the user's task or a session name.
// Keep this separate from contentText so native history remains unchanged.
func promptTitle(row record) string {
	if row.Type != "user" || row.IsSidechain || row.IsMeta || row.IsCompactSummary || row.PromptSource == "system" {
		return ""
	}
	text, kind := contentText(row.Message.Content)
	if kind != "message" || strings.Contains(text, "<command-name>") || strings.Contains(text, "<command-message>") {
		return ""
	}
	for _, tag := range []string{"local-command-caveat", "local-command-stdout", "local-command-stderr", "system-reminder", "task-notification", "teammate-message"} {
		open, close := "<"+tag, "</"+tag+">"
		for {
			start := strings.Index(text, open)
			if start < 0 {
				break
			}
			end := strings.Index(text[start:], close)
			if end < 0 {
				text = text[:start]
				break
			}
			text = text[:start] + " " + text[start+end+len(close):]
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 120 {
		text = string(runes[:120])
	}
	return text
}

func readMetadata(path, profile string) (core.Session, error) {
	f, e := os.Open(path)
	if e != nil {
		return core.Session{}, e
	}
	defer f.Close()
	s := core.Session{Provider: "claude", Profile: profile, NativeID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Title: "Claude session", ExecutionState: "exited", TurnState: "unknown", Capabilities: baseCapabilities()}
	s.Capabilities.Create = false
	var customTitle, aiTitle, firstPrompt string
	read := func(r io.Reader) {
		scan := bufio.NewScanner(r)
		scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scan.Scan() {
			var row record
			if json.Unmarshal(scan.Bytes(), &row) != nil || row.IsSidechain {
				continue
			}
			if row.Cwd != "" {
				s.Cwd = row.Cwd
			}
			if row.Type == "user" && row.PromptID != "" {
				s.TurnID = row.PromptID
			}
			// Idle is not completion. Preserve explicit native end_turn evidence
			// for interactive CLI instances, whose live registry omits State.
			if row.Type == "user" || row.Type == "assistant" || row.Type == "api-request" {
				s.TurnState = "unknown"
				if row.Type == "assistant" && row.Message.StopReason == "end_turn" {
					s.TurnState = "completed"
					text, _ := contentText(row.Message.Content)
					if strings.HasPrefix(strings.TrimSpace(text), "API Error:") {
						s.TurnState = "error"
					}
				}
				if row.Type == "assistant" && row.IsAPIErrorMessage {
					s.TurnState = "error"
				}
			}
			if title := strings.TrimSpace(row.CustomTitle); title != "" {
				customTitle = title
			}
			if title := strings.TrimSpace(row.AITitle); title != "" {
				aiTitle = title
			}
			if firstPrompt == "" {
				firstPrompt = promptTitle(row)
			}
		}
		if scan.Err() != nil {
			s.TurnState = "unknown"
		}
	}
	read(io.LimitReader(f, 512*1024))
	st, e := f.Stat()
	if e != nil {
		return s, e
	}
	if st.Size() > 512*1024 {
		// The omitted middle may contain a later prompt: a completion from the
		// head is not evidence for the tail unless the tail confirms it too.
		s.TurnState = "unknown"
		start := st.Size() - 64*1024
		_, _ = f.Seek(start, io.SeekStart)
		br := bufio.NewReader(f)
		_, _ = br.ReadBytes('\n')
		read(br)
	}
	switch {
	case customTitle != "":
		s.Title = customTitle
	case aiTitle != "":
		s.Title = aiTitle
	case firstPrompt != "":
		s.Title = firstPrompt
	}
	if s.Cwd == "" {
		s.Capabilities.Resume = false
		s.Error = "Transcript has no recognized working directory; native format may have changed"
	}
	return s, nil
}

type historyCursor struct {
	Offset     int64  `json:"o"`
	Head       string `json:"h"`
	HeadLength int    `json:"n"`
}

func readHistory(ctx context.Context, path, cursor string, limit int) (core.History, error) {
	h := core.History{Messages: []core.Message{}, Boundary: "Read-only Claude native JSONL (version-sensitive schema); compaction and unknown metadata remain native history boundaries."}
	if limit <= 0 {
		limit = 60
	}
	if limit > 200 {
		limit = 200
	}
	f, e := os.Open(path)
	if e != nil {
		return h, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return h, e
	}
	var c historyCursor
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(b, &c) != nil || c.HeadLength < 0 || c.HeadLength > 1024 {
			return h, errors.New("invalid history cursor")
		}
	}
	headLength := 1024
	if cursor != "" {
		headLength = c.HeadLength
	}
	head := make([]byte, headLength)
	n, _ := f.ReadAt(head, 0)
	hash := sha256.Sum256(head[:n])
	fingerprint := hex.EncodeToString(hash[:8])
	offset := st.Size()
	if cursor != "" {
		if c.Offset < 0 || c.Offset > st.Size() || c.Head != fingerprint {
			return h, errors.New("native transcript changed/truncated; reload latest history")
		}
		offset = c.Offset
	}
	// Scan backward in bounded chunks. The cursor is a byte boundary before the
	// oldest returned record, so appends cannot shift an earlier page.
	end := offset
	carry := []byte{}
	next := int64(0)
	done := false
	for end > 0 && !done {
		if e := ctx.Err(); e != nil {
			return h, e
		}
		start := end - 64*1024
		if start < 0 {
			start = 0
		}
		buf := make([]byte, end-start)
		if _, e = f.ReadAt(buf, start); e != nil && e != io.EOF {
			return h, e
		}
		buf = append(buf, carry...)
		bound := len(buf)
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] != '\n' {
				continue
			}
			lineStart := i + 1
			if lineStart < bound {
				if m, ok := parseMessage(buf[lineStart:bound]); ok {
					h.Messages = append(h.Messages, m)
					if len(h.Messages) == limit {
						next = start + int64(lineStart)
						done = true
						break
					}
				}
			}
			bound = i
		}
		if done {
			break
		}
		if start == 0 {
			if bound > 0 {
				if m, ok := parseMessage(buf[:bound]); ok {
					h.Messages = append(h.Messages, m)
				}
			}
			break
		}
		carry = append([]byte(nil), buf[:bound]...)
		if len(carry) > 8*1024*1024 {
			return h, fmt.Errorf("native transcript record exceeds 8 MiB; use Claude CLI /export")
		}
		end = start
	}
	for i, j := 0, len(h.Messages)-1; i < j; i, j = i+1, j-1 {
		h.Messages[i], h.Messages[j] = h.Messages[j], h.Messages[i]
	}
	if next > 0 {
		h.HasMore = true
		b, _ := json.Marshal(historyCursor{next, fingerprint, n})
		h.Cursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return h, nil
}
