package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserClientLifecycle(t *testing.T) {
	h := testHub(t)
	handler := h.Handler(http.NotFoundHandler())
	call := func(method, path, body string, cookie *http.Cookie, owner bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(body))
		r.Header.Set("Origin", "http://127.0.0.1:8787")
		r.Header.Set("User-Agent", "Mozilla/5.0 (iPhone)")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if owner {
			r.Header.Set("Authorization", "Bearer "+h.Owner)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	h.C.PublicURL = "https://hub.example"
	if w := call("POST", "/api/setup/link", "", nil, false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := call("POST", "/api/setup/link", "", nil, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var link struct {
		URL string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &link)
	parts := strings.Split(link.URL, "#login=")
	if len(parts) != 2 {
		t.Fatal("missing one-time link")
	}
	w = call("POST", "/api/login/qr/redeem", `{"code":"`+parts[1]+`"}`, nil, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if cookie.Secure {
		t.Fatal("loopback setup cookie unusable")
	}
	if w := call("POST", "/api/login/qr/redeem", `{"code":"`+parts[1]+`"}`, nil, false); w.Code != 403 {
		t.Fatal("replayed login accepted")
	}
	w = call("GET", "/api/clients", "", cookie, false)
	var clients []browserClient
	json.Unmarshal(w.Body.Bytes(), &clients)
	if len(clients) != 1 || clients[0].Name != "iPhone" || !clients[0].Current {
		t.Fatalf("unexpected clients %+v", clients)
	}
	id := clients[0].ID
	if w := call("PATCH", "/api/clients/"+id, `{"name":"My iPhone"}`, nil, true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call("DELETE", "/api/clients/"+id, "", nil, true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call("GET", "/api/devices", "", cookie, false); w.Code != 401 {
		t.Fatal("revoked cookie accepted", w.Code)
	}
}
