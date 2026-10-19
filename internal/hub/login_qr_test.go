package hub

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"pocketagents/internal/config"
	"pocketagents/internal/loginqr"
	"strings"
	"sync"
	"testing"
	"time"
)

func qrRequest(h *Hub, path, body, owner, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	if owner != "" {
		r.Header.Set("Authorization", "Bearer "+owner)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.Handler(http.NotFoundHandler()).ServeHTTP(w, r)
	return w
}

func generateTestQR(t *testing.T, h *Hub) (loginqr.Response, string) {
	t.Helper()
	w := qrRequest(h, "/api/login/qr", "", h.Owner, h.C.PublicURL)
	if w.Code != 200 {
		t.Fatalf("QR generation failed: %d %s", w.Code, w.Body.String())
	}
	var qr loginqr.Response
	if err := json.Unmarshal(w.Body.Bytes(), &qr); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(qr.URL)
	if err != nil {
		t.Fatal(err)
	}
	code := strings.TrimPrefix(u.Fragment, "login=")
	if len(code) != 64 || code == h.Owner || strings.Contains(w.Body.String(), h.Owner) || u.RawQuery != "" || u.Scheme+"://"+u.Host != h.C.PublicURL {
		t.Fatal("unsafe or invalid login link")
	}
	remaining := time.Until(qr.ExpiresAt)
	if remaining < 4*time.Minute || remaining > 5*time.Minute {
		t.Fatal("unexpected QR lifetime", remaining)
	}
	imageData, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(qr.ImageDataURL, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(imageData))
	if err != nil || image.Bounds().Dx() != 384 || image.Bounds().Dy() != 384 {
		t.Fatal("QR image is not a local 384px PNG", err)
	}
	return qr, code
}

func TestLoginQRRequiresOwnerHTTPSAndOrigin(t *testing.T) {
	h := testHub(t)
	if w := qrRequest(h, "/api/login/qr", "", "", ""); w.Code != 401 {
		t.Fatal("unauthenticated QR generation allowed", w.Code)
	}
	for _, origin := range []string{"", "http://127.0.0.1:8787", "https://localhost", "https://127.0.0.1", "https://hub.example/path"} {
		h.C.PublicURL = origin
		if w := qrRequest(h, "/api/login/qr", "", h.Owner, ""); w.Code != 409 {
			t.Fatal("unusable phone origin accepted", origin, w.Code)
		}
	}
	h.C.PublicURL = "https://hub.example"
	if w := qrRequest(h, "/api/login/qr", "", h.Owner, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-origin generation allowed", w.Code)
	}
	_, code := generateTestQR(t, h)
	body, _ := json.Marshal(map[string]string{"code": code})
	if w := qrRequest(h, "/api/login/qr/redeem", string(body), "", "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-origin redemption allowed", w.Code)
	}
	if w := qrRequest(h, "/api/login/qr/redeem", string(body), "", h.C.PublicURL); w.Code != 200 {
		t.Fatal("cross-origin request consumed valid code", w.Code)
	}
}

func TestLoginQRSingleUseReplacementExpiryAndCookie(t *testing.T) {
	h := testHub(t)
	h.C.PublicURL = "https://hub.example"
	_, old := generateTestQR(t, h)
	_, code := generateTestQR(t, h)
	if w := qrRequest(h, "/api/login/qr/redeem", `{"code":"`+old+`"}`, "", h.C.PublicURL); w.Code != 403 {
		t.Fatal("previous QR remained valid", w.Code)
	}
	var stored []byte
	if err := h.S.DB.QueryRow("SELECT value FROM kv WHERE key=?", loginQRSlot).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(code)) || bytes.Contains(stored, []byte(h.Owner)) {
		t.Fatal("database retained plaintext QR/owner secret")
	}
	var pending pendingLoginQR
	if json.Unmarshal(stored, &pending) != nil || pending.Hash != config.Hash(code) {
		t.Fatal("missing hashed one-time credential")
	}
	pending.ExpiresAt = time.Now().Add(-time.Second)
	if err := h.S.Put(loginQRSlot, pending); err != nil {
		t.Fatal(err)
	}
	if w := qrRequest(h, "/api/login/qr/redeem", `{"code":"`+code+`"}`, "", h.C.PublicURL); w.Code != 403 {
		t.Fatal("expired QR accepted", w.Code)
	}
	_, code = generateTestQR(t, h)
	var wg sync.WaitGroup
	out := make(chan *httptest.ResponseRecorder, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out <- qrRequest(h, "/api/login/qr/redeem", `{"code":"`+code+`"}`, "", h.C.PublicURL)
		}()
	}
	wg.Wait()
	close(out)
	accepted := 0
	for w := range out {
		if w.Code == 403 {
			continue
		}
		if w.Code != 200 {
			t.Fatal("unexpected redeem response", w.Code)
		}
		accepted++
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Value == h.Owner || cookies[0].Value == code {
			t.Fatal("unsafe QR-issued session cookie")
		}
		r := httptest.NewRequest("GET", "/api/devices", nil)
		r.AddCookie(cookies[0])
		if !h.authenticated(r) {
			t.Fatal("QR did not establish a durable owner session")
		}
	}
	if accepted != 1 {
		t.Fatal("one-time code accepted more or less than once", accepted)
	}
	var count int
	if err := h.S.DB.QueryRow("SELECT COUNT(*) FROM kv WHERE key=?", loginQRSlot).Scan(&count); err != nil || count != 0 {
		t.Fatal("used QR slot retained", count, err)
	}
}
