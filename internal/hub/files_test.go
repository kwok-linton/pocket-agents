package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFilesRequireOwnerAndSameOrigin(t *testing.T) {
	h := testHub(t)
	handler := h.Handler(http.NotFoundHandler())
	for _, method := range []string{"GET", "POST"} {
		r := httptest.NewRequest(method, "/api/sessions/example/files", strings.NewReader("payload"))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("%s unauthenticated returned %d", method, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/sessions/example/files", strings.NewReader("payload"))
	r.Header.Set("Authorization", "Bearer "+h.Owner)
	r.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin upload returned %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/sessions/missing/file?path=secret", nil)
	r.Header.Set("Authorization", "Bearer "+h.Owner)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("missing session returned %d", w.Code)
	}
}
