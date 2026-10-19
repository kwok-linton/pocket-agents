package codexnative

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"pocketagents/internal/core"
)

func (p *Provider) History(ctx context.Context, id, cursor string, limit int) (core.History, error) {
	h := core.History{Messages: []core.Message{}, Boundary: "Authoritative native Codex item history; newest page first, items within each page chronological."}
	if !validID(id) {
		return h, errors.New("invalid native thread ID")
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	r, e := p.client(ctx)
	if e != nil {
		return h, e
	}
	// Opening a live conversation subscribes to its existing native execution.
	// History of an unloaded thread stays read-only and never invokes resume.
	loaded, e := p.loaded(ctx, r)
	if e != nil {
		return h, e
	}
	if loaded[id] {
		p.mu.Lock()
		subscribed := p.subscribed[id]
		p.mu.Unlock()
		if !subscribed {
			if _, e = p.join(ctx, r, id, false); e != nil {
				return h, e
			}
		}
	}
	params := map[string]any{"threadId": id, "limit": limit, "sortDirection": "desc"}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var page struct {
		Data []struct {
			Item      json.RawMessage `json:"item"`
			TurnID    string          `json:"turnId"`
			Started   int64           `json:"startedAtMs"`
			Completed int64           `json:"completedAtMs"`
		} `json:"data"`
		Next string `json:"nextCursor"`
	}
	if e = r.call(ctx, "thread/items/list", params, &page); e != nil {
		return h, e
	}
	h.Cursor = page.Next
	h.HasMore = page.Next != ""
	for i := len(page.Data) - 1; i >= 0; i-- {
		entry := page.Data[i]
		msg := renderItem(entry.Item)
		if msg.ID == "" {
			continue
		}
		if entry.Started > 0 {
			msg.At = time.UnixMilli(entry.Started)
		} else if entry.Completed > 0 {
			msg.At = time.UnixMilli(entry.Completed)
		}
		h.Messages = append(h.Messages, msg)
	}
	if cursor == "" {
		var turns struct {
			Data []nativeTurn `json:"data"`
		}
		if r.call(ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"}, &turns) == nil && len(turns.Data) > 0 {
			t := turns.Data[0]
			p.mu.Lock()
			s, ok := p.sessions[id]
			if ok {
				s.TurnID = t.ID
				s.TurnState = turnState(t.Status)
				if t.Error != nil {
					s.Error = t.Error.Message
				}
				p.sessions[id] = s
			}
			p.mu.Unlock()
		}
	}
	return h, nil
}
func renderItem(raw json.RawMessage) core.Message {
	var f map[string]json.RawMessage
	if json.Unmarshal(raw, &f) != nil {
		return core.Message{}
	}
	kind := rawString(f, "type")
	m := core.Message{ID: rawString(f, "id"), Kind: kind, Role: "tool"}
	switch kind {
	case "userMessage":
		m.Role = "user"
		var content []map[string]json.RawMessage
		_ = json.Unmarshal(f["content"], &content)
		var parts []string
		for _, c := range content {
			if s := rawString(c, "text"); s != "" {
				parts = append(parts, s)
			} else {
				parts = append(parts, "["+rawString(c, "type")+"]")
			}
		}
		m.Text = strings.Join(parts, "\n")
	case "agentMessage":
		m.Role = "assistant"
		m.Text = rawString(f, "text")
	case "plan":
		m.Role = "assistant"
		m.Text = rawString(f, "text")
	case "commandExecution":
		m.Text = rawString(f, "command")
		if out := rawString(f, "aggregatedOutput"); out != "" {
			m.Text += "\n" + out
		}
	case "reasoning":
		var summary []string
		_ = json.Unmarshal(f["summary"], &summary)
		m.Text = strings.Join(summary, "\n")
		if m.Text == "" {
			return core.Message{}
		}
	case "contextCompaction":
		m.Role = "system"
		m.Text = "Native conversation compacted."
	case "mcpToolCall":
		m.Text = rawString(f, "server") + " / " + rawString(f, "tool") + "\n" + string(f["arguments"])
		if string(f["result"]) != "null" {
			m.Text += "\n" + string(f["result"])
		}
	case "fileChange":
		m.Text = "File changes\n" + string(f["changes"])
	default:
		m.Text = string(raw)
	}
	return m
}
