package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketagents/internal/core"
)

func TestBridgeHooksPreserveSettingsAndRevoke(t *testing.T) {
	p := core.Profile{Name: "default", Home: t.TempDir(), DataDir: t.TempDir()}
	path := filepath.Join(p.Home, "settings.json")
	original := `{"crossSessionInbound":"hold","permissions":{"deny":["Bash"]},"unknown":{"keep":true},"hooks":{"SessionStart":[{"matcher":"resume","hooks":[{"type":"command","command":"echo keep"}]}]}}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	var first []byte
	for i := 0; i < 2; i++ {
		if _, err := ConfigureBridgeHooks(p, "/test/pocket", "/test/data", true); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		if i == 0 {
			first = b
		} else if string(b) != string(first) {
			t.Fatal("install not idempotent")
		}
		var settings map[string]json.RawMessage
		_ = json.Unmarshal(b, &settings)
		if string(settings["crossSessionInbound"]) != `"hold"` || !strings.Contains(string(settings["permissions"]), "Bash") || !strings.Contains(string(settings["unknown"]), "true") {
			t.Fatal("unrelated settings changed")
		}
	}
	if _, err := ConfigureBridgeHooks(p, "/test/pocket", "/test/data", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "claude-hook") || !strings.Contains(string(b), "echo keep") {
		t.Fatal("uninstall did not isolate own hooks")
	}
	if !(&Provider{profile: p}).bridgeDisabled() {
		t.Fatal("existing bridge not revoked")
	}
}
func TestBridgeRequiresGenuineNativeHook(t *testing.T) {
	t.Setenv("CLAUDE_CODE_MESSAGING_SOCKET", "")
	t.Setenv("CLAUDE_CODE_MESSAGING_TOKEN", "")
	if err := RunBridgeHook(context.Background(), core.Profile{Home: t.TempDir(), DataDir: t.TempDir()}, strings.NewReader(`{"session_id":"11111111-1111-4111-8111-111111111111"}`)); err == nil {
		t.Fatal("missing hook context accepted")
	}
	if nativeAncestor(os.Getpid()) {
		t.Fatal("self counted as parent")
	}
}
func TestBridgeRejectsWrongGenerationAndPublicRecord(t *testing.T) {
	p := &Provider{profile: core.Profile{DataDir: t.TempDir()}}
	path := p.bridgePath("one")
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	b, _ := json.Marshal(bridgeRecord{NativeID: "session", ExecutionID: "two", PID: os.Getpid(), Socket: "/tmp/nonexistent"})
	_ = os.WriteFile(path, b, 0600)
	if _, err := p.bridge("one"); err == nil {
		t.Fatal("wrong generation accepted")
	}
	_ = os.Chmod(path, 0644)
	if _, err := p.bridge("one"); err == nil {
		t.Fatal("public bridge accepted")
	}
}
