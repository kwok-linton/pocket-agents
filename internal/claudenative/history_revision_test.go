package claudenative

import (
	"context"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"testing"
)

func TestHistoryRevisionTracksRewriteReplacementAndPhoneOverlay(t *testing.T) {
	dir := t.TempDir()
	id := "12345678-1234-1234-1234-123456789abc"
	project := filepath.Join(dir, "projects", "fixture")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(project, id+".jsonl")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Provider{profile: core.Profile{Home: dir, DataDir: filepath.Join(dir, "pocket")}}
	revision := func() string {
		t.Helper()
		v, err := p.HistoryRevision(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := revision()
	if _, err := os.ReadFile(file); err != nil {
		t.Fatal(err)
	}
	if revision() != first {
		t.Fatal("reading history invalidated its revision")
	}
	st, _ := os.Stat(file)
	if err := os.WriteFile(file, []byte("rewritte"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := revision()
	if second == first {
		t.Fatal("same size/restored mtime rewrite was missed")
	}
	replacement := file + ".replacement"
	if err := os.WriteFile(replacement, []byte("rewritte"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, file); err != nil {
		t.Fatal(err)
	}
	third := revision()
	if third == second {
		t.Fatal("replaced native file missed")
	}
	if err := os.MkdirAll(p.deliveryDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.deliveryDir(), "fixture.json"), []byte(`{"NativeID":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if revision() == third {
		t.Fatal("phone delivery overlay was missed")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := p.HistoryRevision(context.Background(), id); err == nil {
		t.Fatal("deleted transcript treated as unchanged")
	}
}
