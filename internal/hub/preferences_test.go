package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/store"
	"reflect"
	"strings"
	"testing"
)

func TestPreferencesHTTPPersistenceValidationAndAuthorization(t *testing.T) {
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(c.DataDir, "hub.sqlite")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h.Handler(http.NotFoundHandler()))
	t.Cleanup(func() { server.Close(); h.Close(); s.Close() })
	request := func(method string, body any, owner bool, origin string) (int, []byte) {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, _ = json.Marshal(body)
		}
		r, err := http.NewRequest(method, server.URL+"/api/preferences", bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if owner {
			r.Header.Set("Authorization", "Bearer "+h.Owner)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	for _, method := range []string{"GET", "PUT"} {
		if code, _ := request(method, preferences{WorkspaceScope: "focused"}, false, ""); code != http.StatusUnauthorized {
			t.Fatal("unauthenticated preferences request accepted", method, code)
		}
		if code, _ := request(method, preferences{WorkspaceScope: "focused"}, true, "https://foreign.example"); code != http.StatusForbidden {
			t.Fatal("cross-origin preferences request accepted", method, code)
		}
	}
	assertValue := func(method string, body any, want preferences) {
		t.Helper()
		code, raw := request(method, body, true, server.URL)
		var got preferences
		if code != http.StatusOK || json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s preferences got HTTP %d %s, want %#v", method, code, raw, want)
		}
	}
	assertValue("GET", nil, preferences{WorkspaceScope: "all", Workspaces: []workspacePreference{}})
	for _, id := range []string{"node-a", "node-b"} {
		if err = h.Register(core.Device{ID: id, Name: id}, "test-node-credential"); err != nil {
			t.Fatal(err)
		}
	}
	want := preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{
		{NodeID: "node-a", Path: "/nonexistent-on-hub/project-alpha"},
		{NodeID: "node-b", Path: "/nonexistent-on-hub/project-alpha"},
	}}
	assertValue("PUT", preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{
		{NodeID: "node-a", Path: "/nonexistent-on-hub/project-alpha//"},
		{NodeID: "node-a", Path: "/nonexistent-on-hub/project-beta/../project-alpha/."},
		{NodeID: "node-b", Path: "/nonexistent-on-hub/project-alpha"},
	}}, want)
	assertValue("GET", nil, want)
	server.Close()
	h.Close()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	h, err = New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	server = httptest.NewServer(h.Handler(http.NotFoundHandler()))
	assertValue("GET", nil, want)
	badCases := []preferences{
		{WorkspaceScope: "invalid"},
		{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: "missing-node", Path: "/project"}}},
		{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: "node-a", Path: "relative/project"}}},
		{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: "node-a", Path: "/project\x00hidden"}}},
		{WorkspaceScope: "focused", Workspaces: []workspacePreference{{NodeID: "node-a", Path: "/" + strings.Repeat("x", 4096)}}},
		{WorkspaceScope: "focused", Workspaces: make([]workspacePreference, 101)},
	}
	for _, bad := range badCases {
		if code, _ := request("PUT", bad, true, ""); code != http.StatusBadRequest {
			t.Fatal("invalid workspace preference accepted", bad.WorkspaceScope, code)
		}
		assertValue("GET", nil, want)
	}
	empty := preferences{WorkspaceScope: "focused", Workspaces: []workspacePreference{}}
	assertValue("PUT", empty, empty)
	assertValue("GET", nil, empty)
	if code, _ := request("PATCH", empty, true, ""); code != http.StatusMethodNotAllowed {
		t.Fatal("unexpected method accepted", code)
	}
}
