package main

import (
	"pocketagents/internal/core"
	"reflect"
	"testing"
)

func TestCodexLauncherPreservesNativeAdministration(t *testing.T) {
	profile := core.Profile{Home: "/test/native-home"}
	for _, args := range [][]string{
		{"login", "status"}, {"logout"}, {"app-server", "daemon", "version"},
		{"help", "resume"}, {"--help"}, {"-V"}, {"exec", "hello"},
		{"-c", "model=example", "app-server", "daemon", "start"},
		{"--config=model=example", "mcp", "list"}, {"--cd", "/work", "doctor"},
		{"--remote", "unix:///chosen.sock", "resume", "native-thread"},
		{"resume", "native-thread", "--remote=unix:///chosen.sock"},
		{"--no-daemon", "hello"},
	} {
		t.Run(args[0]+"/"+args[len(args)-1], func(t *testing.T) {
			got := codexCLIArgs(profile, args)
			if !reflect.DeepEqual(got, args) {
				t.Fatalf("native arguments changed: got %q; want %q", got, args)
			}
		})
	}
}

func TestCodexLauncherConnectsInteractivePathsToSharedDaemon(t *testing.T) {
	profile := core.Profile{Home: "/test/native-home", Endpoint: "/test/custom.sock"}
	for _, args := range [][]string{
		nil, {"你好🙂"}, {"resume", "native-thread"}, {"fork", "--last"},
		{"-p", "login", "hello"}, {"--cd", "app-server", "hello"},
		{"--", "login"}, {"explain login status", "--no-alt-screen"},
	} {
		got := codexCLIArgs(profile, args)
		want := append([]string{"--remote", "unix:///test/custom.sock"}, args...)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("interactive target mismatch: got %q; want %q", got, want)
		}
	}
	got := codexCLIArgs(core.Profile{Home: "/test/home"}, nil)
	if got[1] != "unix:///test/home/app-server-control/app-server-control.sock" {
		t.Fatal("default endpoint does not use profile home", got)
	}
}
