package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestShellNavigationNeverRedirects(t *testing.T) {
	src := buildFixture(t, "navigation")
	index, err := os.ReadFile(filepath.Join(src, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	z.Write(index)
	z.Close()
	if err := os.WriteFile(filepath.Join(src, "index.html.gz"), compressed.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	if _, err := InstallUI(data, src); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", data} {
		h := HandlerAt(dir)
		for _, route := range []string{"/", "/index.html", "/index.html?pocket_update_check=test", "/settings", "/session/example"} {
			for _, encoding := range []string{"identity", "gzip"} {
				for _, method := range []string{"GET", "HEAD"} {
					req := httptest.NewRequest(method, route, nil)
					req.Header.Set("Accept-Encoding", encoding)
					w := httptest.NewRecorder()
					h.ServeHTTP(w, req)
					if w.Code != 200 || w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "no-cache" {
						t.Fatalf("%s %s (%s): code=%d headers=%v", method, route, encoding, w.Code, w.Header())
					}
					if method == "HEAD" {
						if w.Body.Len() != 0 {
							t.Fatal("HEAD returned a body")
						}
						continue
					}
					body := w.Body.Bytes()
					if w.Header().Get("Content-Encoding") == "gzip" {
						reader, err := gzip.NewReader(bytes.NewReader(body))
						if err != nil {
							t.Fatal(err)
						}
						body, err = io.ReadAll(reader)
						reader.Close()
						if err != nil {
							t.Fatal(err)
						}
					}
					if len(body) == 0 || (dir != "" && !bytes.Equal(body, index)) {
						t.Fatal("navigation did not return the shell")
					}
				}
			}
		}
	}
}
