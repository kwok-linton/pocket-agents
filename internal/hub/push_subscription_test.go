package hub

import (
	"bytes"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"pocketagents/internal/config"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func TestPushSubscriptionRefreshKeepsCursorAndReportsOnlyHashes(t *testing.T) {
	h := testHub(t)
	first, err := h.S.AddEvent("completed", "session", "node", nil)
	if err != nil {
		t.Fatal(err)
	}
	sub := webpush.Subscription{Endpoint: "https://fcm.googleapis.com/fcm/send/PRIVATE_ENDPOINT", Keys: webpush.Keys{Auth: "FIRST_AUTH", P256dh: "FIRST_DH"}}
	register := func() {
		t.Helper()
		body, _ := json.Marshal(sub)
		w := httptest.NewRecorder()
		h.subscribe(w, httptest.NewRequest("POST", "/api/push/subscriptions", bytes.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	register()
	if _, err = h.S.AddEvent("waiting_input", "session", "node", nil); err != nil {
		t.Fatal(err)
	}
	sub.Keys = webpush.Keys{Auth: "REFRESHED_AUTH", P256dh: "REFRESHED_DH"}
	register()
	var raw []byte
	if err = h.S.DB.QueryRow("SELECT data FROM push WHERE endpoint=?", sub.Endpoint).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved webpush.Subscription
	if json.Unmarshal(raw, &saved) != nil || saved.Keys != sub.Keys {
		t.Fatal("same-endpoint registration retained obsolete encryption keys")
	}
	var cursor int64
	if err = h.S.Get("pushcursor:"+config.Hash(sub.Endpoint), &cursor); err != nil || cursor != first.ID {
		t.Fatal("refresh skipped an event pending since the initial subscription", cursor, err)
	}
	w := httptest.NewRecorder()
	h.pushInfo(w, httptest.NewRequest("GET", "/api/push", nil))
	var info struct {
		ActiveSubscriptionIDs []string `json:"activeSubscriptionIds"`
	}
	if json.Unmarshal(w.Body.Bytes(), &info) != nil || len(info.ActiveSubscriptionIDs) != 1 || info.ActiveSubscriptionIDs[0] != config.Hash(sub.Endpoint) {
		t.Fatal("active subscription identity not returned")
	}
	for _, secret := range []string{sub.Endpoint, "PRIVATE_ENDPOINT", sub.Keys.Auth, sub.Keys.P256dh} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("push info exposed a subscription capability or key")
		}
	}
	body, _ := json.Marshal(map[string]string{"endpoint": sub.Endpoint})
	h.unsubscribe(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/api/push/subscriptions", bytes.NewReader(body)))
	w = httptest.NewRecorder()
	h.pushInfo(w, httptest.NewRequest("GET", "/api/push", nil))
	if !strings.Contains(w.Body.String(), `"activeSubscriptionIds":[]`) {
		t.Fatal("deleted subscription still presented as active", w.Body.String())
	}
}

type pushCaptureClient func(*http.Request) (*http.Response, error)

func (f pushCaptureClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestPushWireHeadersAndVAPIDContact(t *testing.T) {
	h := testHub(t)
	h.C.PublicURL = "https://control.example.test"
	v, err := h.keys()
	if err != nil {
		t.Fatal(err)
	}
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := webpush.Subscription{Endpoint: "https://fcm.googleapis.com/fcm/send/TEST_ONLY", Keys: webpush.Keys{
		Auth:   base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16)),
		P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)),
	}}
	for _, tc := range []struct{ subject, expected string }{
		{"", "https://control.example.test"},
		{"mailto:owner@localhost", "https://control.example.test"},
		{"owner@localhost", "https://control.example.test"},
		{"mailto:push@example.test", "mailto:push@example.test"},
		{"push@example.test", "mailto:push@example.test"},
		{"https://example.test/contact", "https://example.test/contact"},
	} {
		t.Run(tc.subject, func(t *testing.T) {
			h.C.PushSubject = tc.subject
			called := false
			client := pushCaptureClient(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.Header.Get("Urgency") != "high" || r.Header.Get("TTL") != "0" || r.Header.Get("Topic") != config.Hash("sessioncompleted")[:32] {
					t.Fatal("push delivery headers changed", r.Header)
				}
				if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("Content-Type") != "application/octet-stream" {
					t.Fatal("payload encryption headers missing")
				}
				encrypted, err := io.ReadAll(r.Body)
				if err != nil || bytes.Contains(encrypted, []byte("PRIVATE_NOTIFICATION_BODY")) {
					t.Fatal("notification was not encrypted", err)
				}
				authorization := r.Header.Get("Authorization")
				token := strings.SplitN(strings.TrimPrefix(authorization, "vapid t="), ",", 2)[0]
				parts := strings.Split(token, ".")
				if len(parts) != 3 {
					t.Fatal("VAPID authorization JWT missing")
				}
				claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
				if err != nil {
					t.Fatal(err)
				}
				var claims map[string]any
				if json.Unmarshal(claimsJSON, &claims) != nil || claims["sub"] != tc.expected || claims["aud"] != "https://fcm.googleapis.com" {
					t.Fatal("VAPID subject or audience incorrect", claims)
				}
				return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			// The capture client performs no network I/O or real push delivery.
			resp, err := webpush.SendNotification([]byte("PRIVATE_NOTIFICATION_BODY"), &sub, h.pushOptions(client, v, "sessioncompleted"))
			if err != nil || !called {
				t.Fatal("notification request was not constructed", err)
			}
			resp.Body.Close()
		})
	}
}
