package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"pocketagents/internal/core"
	"strings"
	"sync"
	"testing"
)

type versionedHistory struct {
	testProvider
	mu       sync.Mutex
	revision string
	page     core.History
	reads    int
	err      error
}

func (p *versionedHistory) HistoryRevision(context.Context, string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.revision, p.err
}
func (p *versionedHistory) History(context.Context, string, string, int) (core.History, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	return p.page, p.err
}
func syncRequest(known string) core.Wire {
	b, _ := json.Marshal(historyRequest{Provider: "claude", Profile: "default", NativeID: "fixture", Limit: 20, Known: known})
	return core.Wire{Type: "history_sync", Data: b}
}

func TestNativeHistoryRevisionAvoidsRepeatedReadsAndSendsOnlyChanges(t *testing.T) {
	n := eventNode(t)
	p := &versionedHistory{revision: "v1", page: core.History{Messages: []core.Message{}, Cursor: "older", HasMore: true}}
	for i := 0; i < 20; i++ {
		p.page.Messages = append(p.page.Messages, core.Message{ID: fmt.Sprint(i), Text: strings.Repeat("native history ", 300)})
	}
	n.Providers = map[string]core.Provider{"claude:default": p}
	first, err := n.syncHistory(context.Background(), syncRequest(""))
	if err != nil || len(first.Messages) != 20 {
		t.Fatal(first, err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, err := n.syncHistory(context.Background(), syncRequest(first.Version))
			if err != nil || !page.Unchanged {
				t.Errorf("unchanged read: %v", err)
			}
		}()
	}
	wg.Wait()
	if p.reads != 1 {
		t.Fatal("parsed unchanged native history", p.reads)
	}
	p.page.Messages = append([]core.Message(nil), p.page.Messages...)
	p.page.Messages[19].Text = "streamed update"
	p.revision = "v2"
	changed, err := n.syncHistory(context.Background(), syncRequest(first.Version))
	if err != nil || changed.Base != first.Version || len(changed.Messages) != 1 || len(changed.Order) != 20 || p.reads != 2 {
		t.Fatal("full page sent for one changed message", changed, err)
	}
	fullBytes, _ := json.Marshal(p.page)
	deltaBytes, _ := json.Marshal(changed)
	t.Logf("20-message fixture: full=%d bytes, delta=%d bytes", len(fullBytes), len(deltaBytes))
	if len(deltaBytes)*10 >= len(fullBytes) {
		t.Fatal("incremental transfer failed to reduce fixture payload")
	}
	// A device with an older/evicted base gets a complete native page.
	stale, err := n.syncHistory(context.Background(), syncRequest("\"unknown-base\""))
	if err != nil || stale.Base != "" || len(stale.Messages) != 20 {
		t.Fatal("stale base not repaired", err)
	}
	// Deletion/provider failure must not serve cached data as current.
	p.err = errors.New("native transcript absent")
	if _, err := n.syncHistory(context.Background(), syncRequest(changed.Version)); err == nil {
		t.Fatal("missing native history returned from cache")
	}
}

func TestHistoryDeltaRepresentsRemovalsAndRewrites(t *testing.T) {
	old := core.History{Messages: []core.Message{{ID: "a", Text: strings.Repeat("x", 4000)}, {ID: "b", Text: "removed"}, {ID: "c", Text: "old"}}}
	next := core.History{Messages: []core.Message{old.Messages[0], {ID: "c", Text: "rewritten"}, {ID: "d", Text: "new"}}}
	version := core.HistoryVersion(old)
	delta := core.SyncHistory(next, core.HistoryVersion(next), old, version, version)
	if delta.Base != version || len(delta.Messages) != 2 || strings.Join(delta.Order, ",") != "a,c,d" {
		t.Fatal("rewrite/removal not encoded", delta)
	}
	cleared := core.SyncHistory(core.History{Messages: []core.Message{}}, "empty", old, version, version)
	if cleared.Base != "" || len(cleared.Messages) != 0 {
		t.Fatal("empty native history retained stale messages")
	}
}
