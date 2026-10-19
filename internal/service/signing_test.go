package service

import (
	"os"
	"runtime"
	"testing"
)

func TestInstalledSigningIdentity(t *testing.T) {
	good, bad := os.Getenv("POCKET_SIGNED_PROBE"), os.Getenv("POCKET_UNSIGNED_PROBE")
	if runtime.GOOS != "darwin" || good == "" || bad == "" {
		t.Skip("explicit macOS signing fixtures required")
	}
	home, _ := os.UserHomeDir()
	installed := home + "/.local/bin/pocket"
	if err := checkUpgradeIdentity(installed, good); err != nil {
		t.Fatalf("same identity update refused: %v", err)
	}
	if err := checkUpgradeIdentity(installed, bad); err == nil {
		t.Fatal("unsigned replacement accepted")
	}
}
