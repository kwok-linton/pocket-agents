package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// HistorySync replaces only changed messages in the latest page. Order is the
// complete page order, so removals and native rewrites are represented too.
type HistorySync struct {
	History
	Version   string   `json:"version"`
	Base      string   `json:"base,omitempty"`
	Order     []string `json:"order,omitempty"`
	Unchanged bool     `json:"unchanged,omitempty"`
}

func HistoryVersion(h History) string {
	b, _ := json.Marshal(h)
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:12]) + `"`
}

func SyncHistory(current History, version string, previous History, previousVersion, known string) HistorySync {
	out := HistorySync{History: current, Version: version}
	if known == version {
		return HistorySync{Version: version, Unchanged: true}
	}
	if known == "" || known != previousVersion || len(current.Messages) == 0 {
		return out
	}
	old := make(map[string]Message, len(previous.Messages))
	for _, m := range previous.Messages {
		old[m.ID] = m
	}
	changed := []Message{}
	order := make([]string, 0, len(current.Messages))
	seen := map[string]bool{}
	for _, m := range current.Messages {
		if m.ID == "" || seen[m.ID] {
			return out
		}
		seen[m.ID] = true
		order = append(order, m.ID)
		if before, ok := old[m.ID]; !ok || before != m {
			changed = append(changed, m)
		}
	}
	delta := HistorySync{History: current, Version: version, Base: known, Order: order}
	delta.Messages = changed
	fullBytes, _ := json.Marshal(out)
	deltaBytes, _ := json.Marshal(delta)
	if len(deltaBytes) < len(fullBytes) {
		return delta
	}
	return out
}
