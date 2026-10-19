package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"strings"
	"sync/atomic"
	"testing"
)

type syncProvider struct {
	recoveryProvider
	phase atomic.Int32
	reads atomic.Int32
}

func (p *syncProvider) HistoryRevision(context.Context, string) (string, error) {
	return fmt.Sprint(p.phase.Load()), nil
}
func (p *syncProvider) History(context.Context, string, string, int) (core.History, error) {
	p.reads.Add(1)
	messages := []core.Message{}
	for i := 0; i < 20; i++ {
		messages = append(messages, core.Message{ID: fmt.Sprint(i), Text: strings.Repeat("native text ", 300)})
	}
	if p.phase.Load() > 0 {
		messages[19].Text = "new output"
	}
	return core.History{Messages: messages}, nil
}
func TestHistorySyncHTTPReturns304AndSmallDeltaAcrossNodeRPC(t *testing.T) {
	p := &syncProvider{}
	rig := openRecoveryRig(t, t.TempDir(), p, false)
	s := core.Session{Provider: "codex", Profile: "default", NativeID: "fixture-native"}
	rig.h.snapshot(rig.n.C.NodeID, core.Snapshot{Device: core.Device{ID: rig.n.C.NodeID}, Sessions: []core.Session{s}})
	sid := config.SessionID(rig.n.C.NodeID, s.Provider, s.Profile, s.NativeID)
	request := func(tag string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest("GET", rig.server.URL+"/api/sessions/"+sid+"/history?limit=20&sync=1", nil)
		req.Header.Set("Authorization", "Bearer "+rig.h.Owner)
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		res, err := rig.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res, b
	}
	first, full := request("")
	if first.StatusCode != 200 || first.Header.Get("Server-Timing") == "" {
		t.Fatal("initial history", first.StatusCode)
	}
	tag := first.Header.Get("ETag")
	unchanged, body := request(tag)
	if unchanged.StatusCode != 304 || len(body) != 0 || p.reads.Load() != 1 {
		t.Fatal("unchanged history reread/transferred", unchanged.StatusCode, p.reads.Load())
	}
	p.phase.Store(1)
	changed, body := request(tag)
	var delta core.HistorySync
	if json.Unmarshal(body, &delta) != nil || changed.Header.Get("X-Pocket-History-Delta") != "1" || len(delta.Messages) != 1 || len(delta.Order) != 20 {
		t.Fatal("delta not propagated through HTTP/RPC")
	}
	if len(body)*10 >= len(full) {
		t.Fatal("delta too large", len(body), len(full))
	}
	t.Logf("HTTP decoded history: full=%d bytes; delta=%d bytes; unchanged=0 bytes", len(full), len(body))
	req, _ := http.NewRequest("GET", rig.server.URL+"/api/sessions/"+sid, nil)
	req.Header.Set("Authorization", "Bearer "+rig.h.Owner)
	res, err := rig.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var detail struct {
		Session core.Session
		Device  core.Device
	}
	if json.NewDecoder(res.Body).Decode(&detail) != nil || detail.Session.ID != sid || !detail.Device.Online {
		t.Fatal("focused session detail missing")
	}
}
