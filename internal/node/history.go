package node

import (
	"context"
	"encoding/json"
	"errors"
	"pocketagents/internal/core"
	"sync"
	"time"
)

type historyRequest struct {
	Provider, Profile, NativeID, Cursor, Known string
	Limit                                      int
}

type historyEntry struct {
	history           core.History
	version, revision string
	size              int
	used              time.Time
}

type historyGate struct {
	ready chan struct{}
	users int
}

// Bounded, in-memory native pages. The native provider remains authoritative;
// a provider without a cheap revision always rereads its native page.
type historyPages struct {
	mu      sync.Mutex
	entries map[string]historyEntry
	gates   map[string]*historyGate
}

func (n *Node) syncHistory(ctx context.Context, req core.Wire) (core.HistorySync, error) {
	var input historyRequest
	if err := json.Unmarshal(req.Data, &input); err != nil {
		return core.HistorySync{}, err
	}
	p := n.Providers[input.Provider+":"+input.Profile]
	if p == nil {
		return core.HistorySync{}, errors.New("provider profile unavailable")
	}
	if input.Limit < 1 || input.Limit > 200 {
		input.Limit = 50
	}
	keyBytes, _ := json.Marshal([]any{input.Provider, input.Profile, input.NativeID, input.Cursor, input.Limit})
	key := string(keyBytes)
	c := &n.history
	c.mu.Lock()
	if c.gates == nil {
		c.gates = map[string]*historyGate{}
		c.entries = map[string]historyEntry{}
	}
	gate := c.gates[key]
	if gate == nil {
		gate = &historyGate{ready: make(chan struct{}, 1)}
		c.gates[key] = gate
	}
	gate.users++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		gate.users--
		if gate.users == 0 {
			delete(c.gates, key)
		}
	}()
	select {
	case gate.ready <- struct{}{}:
		defer func() { <-gate.ready }()
	case <-ctx.Done():
		return core.HistorySync{}, ctx.Err()
	}
	c.mu.Lock()
	old := c.entries[key]
	c.mu.Unlock()
	revision := ""
	versioned, cheap := p.(interface {
		HistoryRevision(context.Context, string) (string, error)
	})
	if cheap {
		var err error
		revision, err = versioned.HistoryRevision(ctx, input.NativeID)
		if err != nil {
			return core.HistorySync{}, err
		}
	}
	current := old
	if revision == "" || revision != old.revision || old.version == "" {
		h, err := p.History(ctx, input.NativeID, input.Cursor, input.Limit)
		if err != nil {
			return core.HistorySync{}, err
		}
		if cheap {
			after, err := versioned.HistoryRevision(ctx, input.NativeID)
			if err != nil || after != revision {
				revision = ""
			} // changed during read; reread next time
		}
		b, _ := json.Marshal(h)
		current = historyEntry{history: h, version: core.HistoryVersion(h), revision: revision, size: len(b)}
	}
	current.used = time.Now()
	c.mu.Lock()
	c.entries[key] = current
	for {
		total, oldestKey := 0, ""
		var oldest time.Time
		for k, entry := range c.entries {
			total += entry.size
			if oldestKey == "" || entry.used.Before(oldest) {
				oldestKey, oldest = k, entry.used
			}
		}
		if len(c.entries) <= 32 && total <= 16<<20 {
			break
		}
		delete(c.entries, oldestKey)
	}
	c.mu.Unlock()
	return core.SyncHistory(current.history, current.version, old.history, old.version, input.Known), nil
}
