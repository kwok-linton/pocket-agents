package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildFixture(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"pocket-ui.json": `{"protocol":1}`, "index.html": `<html><script src="/assets/` + name + `.js"></script></html>`, "sw.js": name, "manifest.webmanifest": "{}", "assets/" + name + ".js": name}
	for p, b := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func getUI(h http.Handler, p string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
	return w
}
func TestLiveUIInstall(t *testing.T) {
	data := t.TempDir()
	h := HandlerAt(data)
	if w := getUI(h, "/"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	first := buildFixture(t, "first")
	v, err := InstallUI(data, first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := InstallUI(data, first)
	if err != nil || v != again {
		t.Fatalf("idempotence: %s %v", again, err)
	}
	for _, p := range []string{"/", "/session/example"} {
		w := getUI(h, p)
		if !strings.Contains(w.Body.String(), "first.js") || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal(w)
		}
	}
	if _, err = InstallUI(data, buildFixture(t, "second")); err != nil {
		t.Fatal(err)
	}
	if w := getUI(h, "/sw.js"); w.Body.String() != "second" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal(w)
	}
	if w := getUI(h, "/assets/first.js"); w.Body.String() != "first" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatal(w)
	}
	if w := getUI(h, "/"); !strings.Contains(w.Body.String(), "second.js") {
		t.Fatal(w)
	}
}
func TestInvalidUILeavesCurrentUntouched(t *testing.T) {
	for _, kind := range []string{"protocol", "missing", "symlink", "collision", "reference"} {
		t.Run(kind, func(t *testing.T) {
			data := t.TempDir()
			v, err := InstallUI(data, buildFixture(t, "first"))
			if err != nil {
				t.Fatal(err)
			}
			src := buildFixture(t, "second")
			switch kind {
			case "protocol":
				err = os.WriteFile(filepath.Join(src, "pocket-ui.json"), []byte(`{"protocol":2}`), 0600)
			case "missing":
				err = os.Remove(filepath.Join(src, "sw.js"))
			case "symlink":
				err = os.Symlink("index.html", filepath.Join(src, "link"))
			case "collision":
				err = os.WriteFile(filepath.Join(src, "assets/first.js"), []byte("changed"), 0600)
			case "reference":
				err = os.Remove(filepath.Join(src, "assets/second.js"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = InstallUI(data, src); err == nil {
				t.Fatal("accepted invalid release")
			}
			current, err := os.ReadFile(filepath.Join(data, "web-ui/current"))
			if err != nil || string(current) != v {
				t.Fatalf("changed active release: %v", err)
			}
		})
	}
}
