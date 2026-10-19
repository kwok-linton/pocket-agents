package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrecompressedAssetsNegotiateCodecWithoutChangingContentType(t *testing.T) {
	src := buildFixture(t, "compressed")
	original := strings.Repeat("const fixture='content';\n", 100)
	if err := os.WriteFile(filepath.Join(src, "assets/compressed.js"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	z.Write([]byte(original))
	z.Close()
	if err := os.WriteFile(filepath.Join(src, "assets/compressed.js.gz"), b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "assets/compressed.js.br"), []byte("fixture-brotli-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	if _, err := InstallUI(data, src); err != nil {
		t.Fatal(err)
	}
	h := HandlerAt(data)
	for _, tc := range []struct{ accept, codec string }{{"br, gzip", "br"}, {"br;q=0, gzip;q=1", "gzip"}, {"gzip;q=0, br;q=0", ""}, {"gzip;q=0.9, br;q=0.5", "gzip"}} {
		req := httptest.NewRequest("GET", "/assets/compressed.js", nil)
		req.Header.Set("Accept-Encoding", tc.accept)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 || w.Header().Get("Content-Encoding") != tc.codec || !strings.Contains(w.Header().Get("Content-Type"), "javascript") || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Fatal(tc, w.Header())
		}
		if tc.codec == "gzip" {
			reader, err := gzip.NewReader(w.Body)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(decoded) != original {
				t.Fatal("gzip changed source", err)
			}
		}
		if tc.codec == "" && w.Body.String() != original {
			t.Fatal("identity response changed source")
		}
	}
}
