package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"pocketagents/internal/config"
	"strings"
	"testing"
	"time"
)

func TestPushDeliveryDiagnosticsRedactTransportSecretsAndKeepOnlyLatest(t *testing.T) {
	h := testHub(t)
	endpoint := "https://fcm.googleapis.com/fcm/send/SECRET_SUBSCRIPTION"
	attempt := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	transport := &url.Error{Op: "Post", URL: endpoint, Err: errors.New("private auth=SECRET_KEY")}
	if err := h.recordPushDelivery(endpoint, 41, attempt, nil, transport); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := h.S.DB.QueryRow("SELECT value FROM kv WHERE key=?", "pushdelivery:"+config.Hash(endpoint)).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{endpoint, "SECRET_SUBSCRIPTION", "SECRET_KEY", "private auth"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("transport error leaked a subscription secret")
		}
	}
	items, err := h.pushDeliveries()
	if err != nil || len(items) != 1 || items[0].Accepted || items[0].HTTPStatus != 0 || items[0].TransportError == "" || items[0].EventID != 41 || !items[0].AttemptAt.Equal(attempt) {
		t.Fatal("transport failure not represented correctly", items, err)
	}
	if err = h.recordPushDelivery(endpoint, 42, attempt.Add(time.Second), &http.Response{StatusCode: 201}, nil); err != nil {
		t.Fatal(err)
	}
	items, err = h.pushDeliveries()
	if err != nil || len(items) != 1 || !items[0].Accepted || items[0].HTTPStatus != 201 || items[0].TransportError != "" || items[0].EventID != 42 {
		t.Fatal("latest accepted attempt did not replace failure", items, err)
	}
	if err = h.recordPushDelivery(endpoint, 43, attempt.Add(2*time.Second), &http.Response{StatusCode: 410}, nil); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.pushInfo(w, httptest.NewRequest("GET", "/api/push", nil))
	var response struct {
		LastDelivery []pushDelivery `json:"lastDelivery"`
		Note         string         `json:"note"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.LastDelivery) != 1 || response.LastDelivery[0].Accepted || response.LastDelivery[0].HTTPStatus != 410 {
		t.Fatal("upstream rejection missing from diagnostic API")
	}
	if strings.Contains(w.Body.String(), "SECRET_") || strings.Contains(w.Body.String(), endpoint) || !strings.Contains(response.Note, "not that the phone displayed") {
		t.Fatal("diagnostic API leaked a secret or claimed phone delivery")
	}
	request := httptest.NewRequest("DELETE", "/api/push/subscriptions", strings.NewReader(`{"endpoint":"`+endpoint+`"}`))
	h.unsubscribe(httptest.NewRecorder(), request)
	items, err = h.pushDeliveries()
	if err != nil || len(items) != 0 {
		t.Fatal("explicit unsubscribe retained its diagnostic", items, err)
	}
}

func TestPushRejectionDiagnosticsKeepOnlyKnownReasons(t *testing.T) {
	h := testHub(t)
	for _, tc := range []struct{ body, want string }{
		{`{"reason":"BadJwtToken","endpoint":"PRIVATE_ENDPOINT"}`, "BadJwtToken"},
		{`{"reason":"PRIVATE_ENDPOINT"}`, ""},
		{`<html>PRIVATE_ENDPOINT</html>`, ""},
		{strings.Repeat(" ", 2048) + `{"reason":"BadJwtToken"}`, ""},
	} {
		response := &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(tc.body))}
		if err := h.recordPushDelivery("https://web.push.apple.com/PRIVATE_ENDPOINT", 1, time.Now(), response, nil); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		items, err := h.pushDeliveries()
		if err != nil || len(items) != 1 || items[0].Reason != tc.want {
			t.Fatalf("reason mismatch: %+v %v", items, err)
		}
		raw, _ := json.Marshal(items)
		if strings.Contains(string(raw), "PRIVATE_ENDPOINT") {
			t.Fatal("upstream body leaked")
		}
	}
}

func TestPushTransportClassificationDoesNotExposeRequestURL(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&url.Error{Op: "Post", URL: "https://web.push.apple.com/SECRET", Err: context.DeadlineExceeded}, "TransportTimeout"},
		{context.Canceled, "TransportCanceled"},
		{errors.New("SECRET"), "TransportFailure"},
	} {
		if got := pushTransportReason(tc.err); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
