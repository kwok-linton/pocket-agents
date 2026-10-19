package store

import (
	"path/filepath"
	"pocketagents/internal/core"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentActionDedupAndUnknownAfterCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	a := core.Action{ID: "request-one", Text: "你好\n🙂"}
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fresh, e := s.Claim("node:", a)
			if e != nil {
				t.Error(e)
			}
			if fresh {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("duplicate injection", count.Load())
	}
	a.Text = "different"
	if _, _, e = s.Claim("node:", a); e == nil {
		t.Fatal("request id reuse accepted")
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Result("node:", a.ID)
	if e != nil || r.Status != "unknown" {
		t.Fatal(r, e)
	}
}

func TestNotificationsShowNewestWhileCatchUpStaysOrdered(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	for range 260 {
		if _, e = s.AddEvent("completed", "s", "n", nil); e != nil {
			t.Fatal(e)
		}
	}
	inbox, e := s.Inbox(200)
	if e != nil || len(inbox) != 200 || inbox[0].ID != 61 || inbox[199].ID != 260 {
		t.Fatal("inbox is not the newest 200 in order", len(inbox), e)
	}
	first, e := s.Events(0, true)
	if e != nil || len(first) != 260 || first[0].ID != 1 {
		t.Fatal("cursor catch-up from zero changed", len(first), e)
	}
	after, e := s.Events(250, true)
	if e != nil || len(after) != 10 || after[0].ID != 251 {
		t.Fatal("cursor catch-up changed", len(after), e)
	}
}
