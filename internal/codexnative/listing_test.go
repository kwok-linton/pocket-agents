package codexnative

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// A fake thread/list: newest first, pages of 2.
type fakeListing struct {
	threads []nativeThread
	calls   []string
}

func (f *fakeListing) fetch(cursor string, repair bool) (threadPage, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s/%v", cursor, repair))
	start := 0
	if cursor != "" {
		fmt.Sscan(cursor, &start)
	}
	end := min(start+2, len(f.threads))
	page := threadPage{Data: append([]nativeThread(nil), f.threads[start:end]...)}
	if end < len(f.threads) {
		page.Next = fmt.Sprint(end)
	}
	return page, nil
}
func thread(id string, updated int64) nativeThread {
	return nativeThread{ID: id, Name: id, Updated: updated}
}
func ids(list []nativeThread) string {
	s := ""
	for _, t := range list {
		s += t.ID + " "
	}
	return s
}

func TestIncrementalThreadListing(t *testing.T) {
	p := &Provider{lastRepair: time.Now()}
	f := &fakeListing{threads: []nativeThread{thread("e", 5), thread("d", 4), thread("c", 3), thread("b", 2), thread("a", 1)}}
	list, fresh, err := p.threads(f.fetch)
	if err != nil || ids(list) != "e d c b a " || len(fresh) != 5 || len(f.calls) != 3 {
		t.Fatal("first scan must be a full listing", ids(list), len(fresh), f.calls, err)
	}

	// One thread changed: only the first page is read, the rest is reused.
	f.calls = nil
	f.threads = []nativeThread{thread("b", 6), thread("e", 5), thread("d", 4), thread("c", 3), thread("a", 1)}
	list, fresh, err = p.threads(f.fetch)
	if err != nil || len(f.calls) != 1 || ids(list) != "b e d c a " || !fresh["b"] || fresh["a"] {
		t.Fatal("incremental scan", f.calls, ids(list), fresh, err)
	}

	// The whole first page changed: later pages may have too, so list fully.
	f.calls = nil
	f.threads = []nativeThread{thread("x", 8), thread("y", 7), thread("b", 6), thread("e", 5), thread("d", 4)}
	list, _, err = p.threads(f.fetch)
	if err != nil || len(f.calls) != 4 || ids(list) != "x y b e d " {
		t.Fatal("fully changed first page must fall back to a full listing", f.calls, ids(list), err)
	}

	// Deletions are only visible to a full listing; an action forces one.
	f.calls = nil
	f.threads = f.threads[:4]
	p.listedAt = time.Time{}
	list, _, _ = p.threads(f.fetch)
	if ids(list) != "x y b e " {
		t.Fatal("deleted thread kept after a forced full listing", ids(list))
	}

	// A single-page workspace is always complete.
	single := &Provider{lastRepair: time.Now()}
	f = &fakeListing{threads: []nativeThread{thread("a", 1)}}
	single.threads(f.fetch)
	f.threads = nil
	f.calls = nil
	if list, _, _ = single.threads(f.fetch); len(list) != 0 || len(f.calls) != 1 {
		t.Fatal("single page listing kept a deleted thread", ids(list), f.calls)
	}

	// The periodic repair listing still runs through the full path.
	p.lastRepair = time.Time{}
	f = &fakeListing{threads: []nativeThread{thread("x", 8)}}
	p.threads(f.fetch)
	if f.calls[0] != "/true" {
		t.Fatal("repair listing not requested", f.calls)
	}
}

func TestLockedThreadsReadsDirectoryOnce(t *testing.T) {
	p := &Provider{}
	p.profile.Home = t.TempDir()
	if locks, ok := p.lockedThreads(); !ok || len(locks) != 0 {
		t.Fatal("missing lock directory must mean no external writers", locks, ok)
	}
	dir := p.profile.Home + "/thread-writer-locks"
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/0123456789ab.lock", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if locks, ok := p.lockedThreads(); !ok || !locks["0123456789ab"] || len(locks) != 1 {
		t.Fatal("lock file not found", locks, ok)
	}
}
