package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"pocketagents/internal/claudenative"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/store"
	"reflect"
	"testing"
	"time"
)

// Rebuild only the disposable Hub sessions index. Authentication, metadata,
// delivery journals and native history must survive unchanged.
func exerciseIndexRebuild(t *testing.T, factory func() map[string]core.Provider, targets map[string]string) {
	t.Helper()
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.DataDir, "hub.sqlite")
	open := func() *Hub {
		s, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		h, err := New(c, s)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	discover := func() ([]core.Session, map[string][32]byte) {
		ps := factory()
		defer func() {
			for _, p := range ps {
				_ = p.Close()
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		sessions := []core.Session{}
		hashes := map[string][32]byte{}
		for key, native := range targets {
			p := ps[key]
			if p == nil {
				t.Fatalf("missing profile %s", key)
			}
			ss, err := p.Discover(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, s := range ss {
				if s.NativeID == native {
					sessions = append(sessions, s)
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("dedicated test session missing in %s (discovered %d)", key, len(ss))
			}
			history, err := p.History(ctx, native, "", 50)
			if err != nil || len(history.Messages) == 0 {
				t.Fatalf("test native history unavailable: %s %v", key, err)
			}
			pages := []core.History{history}
			messageCount := len(history.Messages)
			for history.HasMore {
				if history.Cursor == "" {
					t.Fatal("native history promised another page without a cursor")
				}
				history, err = p.History(ctx, native, history.Cursor, 50)
				if err != nil {
					t.Fatal("native history pagination failed", err)
				}
				pages = append(pages, history)
				messageCount += len(history.Messages)
			}
			data, _ := json.Marshal(pages)
			hashes[key] = sha256.Sum256(data)
			idHash := sha256.Sum256([]byte(native))
			t.Logf("%s discovered=%d selected_id_sha256=%s full_history_messages=%d pages=%d", key, len(ss), hex.EncodeToString(idHash[:6]), messageCount, len(pages))
		}
		return sessions, hashes
	}
	d := core.Device{ID: "disposable-index-node", Name: "disposable index fixture"}
	metadata := core.Metadata{Alias: "保留别名🙂", Favorite: true, Done: true, Hidden: true, ReadAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	h := open()
	if err = h.Register(d, "disposable-index-node-token"); err != nil {
		t.Fatal(err)
	}
	before, historyBefore := discover()
	h.snapshot(d.ID, core.Snapshot{Device: d, Sessions: before})
	ids := map[string]bool{}
	for _, s := range before {
		id := config.SessionID(d.ID, s.Provider, s.Profile, s.NativeID)
		ids[id] = true
		if err = h.S.SetMetadata(id, metadata); err != nil {
			t.Fatal(err)
		}
	}
	a := core.Action{ID: "preserve-delivery-journal", NodeID: d.ID, Kind: "fixture"}
	if _, _, err = h.S.Claim("hub:", a); err != nil {
		t.Fatal(err)
	}
	if err = h.S.Finish("hub:", core.Result{ActionID: a.ID, Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	owner := h.Owner
	h.Close()
	if err = h.S.Close(); err != nil {
		t.Fatal(err)
	}
	// This is the documented maintenance operation, performed only on t.TempDir.
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DELETE FROM sessions"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	h = open()
	defer func() { h.Close(); h.S.Close() }()
	if len(h.sessions) != 0 || h.Owner != owner {
		t.Fatal("index was not cleared or owner identity changed")
	}
	var tokenHash string
	if err = h.S.DB.QueryRow("SELECT token FROM devices WHERE id=?", d.ID).Scan(&tokenHash); err != nil || tokenHash != config.Hash("disposable-index-node-token") {
		t.Fatal("node pairing lost during index rebuild", err)
	}
	after, historyAfter := discover() // New provider objects, no original adapter cache.
	if !reflect.DeepEqual(historyBefore, historyAfter) {
		t.Fatal("native transcript changed during application index rebuild")
	}
	h.snapshot(d.ID, core.Snapshot{Device: d, Sessions: after})
	w := httptest.NewRecorder()
	h.listSessions(w, httptest.NewRequest("GET", "/api/sessions", nil))
	var listed []core.Session
	if err = json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed) != len(ids) {
		t.Fatal("native sessions not rediscovered", len(listed), err)
	}
	for _, session := range listed {
		if !ids[session.ID] || !reflect.DeepEqual(session.Metadata, metadata) {
			t.Fatal("session identity or user metadata lost during rebuild")
		}
	}
	if result, fresh, err := h.S.Claim("hub:", a); err != nil || fresh || result.Status != "accepted" {
		t.Fatal("index rebuild lost acknowledged action", result, fresh, err)
	}
}

func TestIndexRebuildRediscoversNativeFilesAndPreservesMetadata(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	native := "10000000-0000-4000-8000-000000000001"
	project := filepath.Join(home, "projects", "fixture-project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	for i, text := range []string{"fixture user 中文", "fixture assistant 🙂"} {
		role := []string{"user", "assistant"}[i]
		row := map[string]any{"type": role, "uuid": role, "sessionId": native, "cwd": cwd, "timestamp": "2026-01-02T03:04:05Z", "message": map[string]any{"role": role, "content": text}}
		_ = json.NewEncoder(&data).Encode(row)
	}
	path := filepath.Join(project, native+".jsonl")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	factory := func() map[string]core.Provider {
		// Real native-file discovery/history parser, but deliberately no live CLI
		// or inference. The invalid binary makes the runtime state explicitly unknown.
		p, err := claudenative.New(core.Profile{Name: "fixture", Home: home, Binary: filepath.Join(home, "no-live-cli")})
		if err != nil {
			t.Fatal(err)
		}
		return map[string]core.Provider{"claude:fixture": p}
	}
	exerciseIndexRebuild(t, factory, map[string]string{"claude:fixture": native})
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data.Bytes()) {
		t.Fatal("disposable native file was modified", err)
	}
}
