package hub

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	webpush "github.com/SherClockHolmes/webpush-go"
	"io"
	"net/http"
	"net/http/httptest"
	"pocketagents/internal/config"
	"strings"
	"testing"
	"time"
)

func checkSubscription(t *testing.T, h *Hub, suffix string) webpush.Subscription {
	t.Helper()
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := webpush.Subscription{Endpoint: "https://web.push.apple.com/TEST_ONLY_" + suffix, Keys: webpush.Keys{Auth: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16)), P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y))}}
	b, _ := json.Marshal(sub)
	if _, err = h.S.DB.Exec("INSERT INTO push VALUES(?,?)", sub.Endpoint, b); err != nil {
		t.Fatal(err)
	}
	return sub
}
func queueCheck(t *testing.T, h *Hub, id, subID string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"id": id, "subscriptionId": subID})
	w := httptest.NewRecorder()
	h.queuePushCheck(w, httptest.NewRequest("POST", "/api/push/tests", bytes.NewReader(b)))
	return w
}
func TestPushCheckTargetsOnlyRegisteredDeviceAndDeduplicates(t *testing.T) {
	h := testHub(t)
	a := checkSubscription(t, h, "A")
	b := checkSubscription(t, h, "B")
	id := "12345678-1234-1234-1234-123456789abc"
	if w := queueCheck(t, h, id, config.Hash("missing")); w.Code != 409 {
		t.Fatal(w.Code)
	}
	w := queueCheck(t, h, id, config.Hash(a.Endpoint))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if duplicate := queueCheck(t, h, id, config.Hash(a.Endpoint)); duplicate.Body.String() != w.Body.String() {
		t.Fatal("duplicate changed job")
	}
	if conflict := queueCheck(t, h, id, config.Hash(b.Endpoint)); conflict.Code != 409 {
		t.Fatal("id retargeted")
	}
	if limited := queueCheck(t, h, "22345678-1234-1234-1234-123456789abc", config.Hash(a.Endpoint)); limited.Code != 429 {
		t.Fatal("rate limit missing")
	}
	v, _ := h.keys()
	calls := 0
	client := pushCaptureClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != a.Endpoint {
			t.Fatal("sent to other device")
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	h.sendPushChecks(context.Background(), client, v, []webpush.Subscription{b, a})
	if calls != 0 {
		t.Fatal("sent before lock-screen delay")
	}
	var check pushCheck
	if err := h.S.Get("pushtest:"+id, &check); err != nil {
		t.Fatal(err)
	}
	check.DueAt = time.Now().Add(-time.Second).UTC()
	_ = h.S.Put("pushtest:"+id, check)
	h.sendPushChecks(context.Background(), client, v, []webpush.Subscription{b, a})
	h.sendPushChecks(context.Background(), client, v, []webpush.Subscription{b, a})
	if calls != 1 {
		t.Fatal("duplicate send", calls)
	}
	_ = h.S.Get("pushtest:"+id, &check)
	if check.State != "accepted" {
		t.Fatal(check)
	}
	info := httptest.NewRecorder()
	h.pushInfo(info, httptest.NewRequest("GET", "/api/push", nil))
	if strings.Contains(info.Body.String(), "TEST_ONLY") || strings.Contains(info.Body.String(), a.Keys.Auth) {
		t.Fatal("secret leaked")
	}
	var events int
	_ = h.S.DB.QueryRow("SELECT COUNT(*) FROM events").Scan(&events)
	if events != 0 {
		t.Fatal("test created fake agent event")
	}
}
func TestPushCheckRejectionsAndUnknownNeverRetry(t *testing.T) {
	for _, tc := range []struct {
		status int
		fail   bool
		state  string
	}{{403, false, "rejected"}, {410, false, "expired"}, {0, true, "unknown"}} {
		t.Run(tc.state, func(t *testing.T) {
			h := testHub(t)
			sub := checkSubscription(t, h, "SECRET")
			v, _ := h.keys()
			check := pushCheck{ID: "12345678-1234-1234-1234-123456789abc", SubscriptionID: config.Hash(sub.Endpoint), DueAt: time.Now().Add(-time.Second).UTC(), State: "queued"}
			_ = h.S.Put("pushtest:"+check.ID, check)
			calls := 0
			client := pushCaptureClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.fail {
					return nil, errors.New(sub.Endpoint)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("private error"))}, nil
			})
			for range 2 {
				h.sendPushChecks(context.Background(), client, v, []webpush.Subscription{sub})
			}
			_ = h.S.Get("pushtest:"+check.ID, &check)
			if calls != 1 || check.State != tc.state {
				t.Fatal(calls, check)
			}
			ids, _ := h.activePushSubscriptionIDs()
			if (len(ids) == 0) != (tc.status == 410) {
				t.Fatal("wrong subscription deletion")
			}
			b, _ := json.Marshal(check)
			if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "private") {
				t.Fatal("secret leaked")
			}
		})
	}
}
