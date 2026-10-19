package main

import (
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/hub"
	"pocketagents/internal/store"
	"strings"
	"testing"
	"time"
)

func TestLoginQRCLIProducesPrivatePNGFromLocalHub(t *testing.T) {
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.PublicURL = "https://hub.example"
	s, err := store.Open(filepath.Join(c.DataDir, "hub.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h, err := hub.New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	server := httptest.NewServer(h.Handler(http.NotFoundHandler()))
	defer server.Close()
	c.Listen = strings.TrimPrefix(server.URL, "http://")
	for _, output := range []string{"", filepath.Join(c.DataDir, "custom.png")} {
		path, expires, err := createLoginQRFile(context.Background(), c, output)
		if err != nil || !expires.After(time.Now()) {
			t.Fatal(path, expires, err)
		}
		if output == "" && path != filepath.Join(c.DataDir, "login-qr.png") {
			t.Fatal("unexpected default output", path)
		}
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0600 {
			t.Fatal("QR file is not private", err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal("invalid generated QR PNG", err)
		}
	}
	c.PublicURL = ""
	if _, _, err := createLoginQRFile(context.Background(), c, ""); err == nil {
		t.Fatal("missing publicUrl silently generated a loopback QR")
	}
}
