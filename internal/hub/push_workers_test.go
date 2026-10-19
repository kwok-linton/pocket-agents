package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlowPushTargetAndDiagnosticDoNotBlockOtherNotifications(t *testing.T) {
	h := testHub(t)
	slow := checkSubscription(t, h, "slow")
	fast := checkSubscription(t, h, "fast")
	ev := fixtureEvent()
	old, err := h.S.AddEvent(ev.Kind, ev.SessionID, ev.NodeID, json.RawMessage(ev.Data))
	if err != nil {
		t.Fatal(err)
	}
	check := pushCheck{ID: "12345678-1234-1234-1234-123456789abc", SubscriptionID: config.Hash(slow.Endpoint), DueAt: time.Now().Add(-time.Minute), State: "queued"}
	if err := h.S.Put("pushtest:"+check.ID, check); err != nil {
		t.Fatal(err)
	}
	v, err := h.keys()
	if err != nil {
		t.Fatal(err)
	}
	var slowCalls atomic.Int32
	testStarted := make(chan struct{}, 1)
	fastTopics := make(chan string, 4)
	client := pushCaptureClient(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("TTL") == "120" {
			testStarted <- struct{}{}
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if r.URL.String() == slow.Endpoint {
			slowCalls.Add(1)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		fastTopics <- r.Header.Get("Topic")
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); h.runPush(ctx, client, v) }()
	defer func() { cancel(); <-done }()
	select {
	case <-testStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("diagnostic did not start")
	}
	var cursor int64
	waitRecovery(t, func() bool {
		return h.S.Get("pushcursor:"+config.Hash(fast.Endpoint), &cursor) == nil && cursor == old.ID
	})
	if _, err := h.S.AddEvent(ev.Kind, ev.SessionID, ev.NodeID, json.RawMessage(ev.Data)); err != nil {
		t.Fatal(err)
	}
	h.signal(ev.Kind)
	seen := map[string]bool{}
	select {
	case topic := <-fastTopics:
		seen[topic] = true
	case <-time.After(2 * time.Second):
		t.Fatal("slow target blocked healthy phone")
	}
	// A real event arriving while a diagnostic is stuck still reaches the phone.
	next, err := h.S.AddEvent(ev.Kind, ev.SessionID, ev.NodeID, json.RawMessage(ev.Data))
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		h.signal(ev.Kind)
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case topic := <-fastTopics:
		if seen[topic] {
			t.Fatal("new event reused old topic")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("diagnostic blocked new notification")
	}
	if slowCalls.Load() != 1 {
		t.Fatal("overlapping workers sent to one subscription", slowCalls.Load())
	}
	waitRecovery(t, func() bool {
		return h.S.Get("pushcursor:"+config.Hash(fast.Endpoint), &cursor) == nil && cursor == next.ID
	})
}

func TestPushRetriesThreeTimesThenDropsWithoutReplaying(t *testing.T) {
	h := testHub(t)
	sub := checkSubscription(t, h, "retry")
	v, _ := h.keys()
	ev := fixtureEvent()
	stored, err := h.S.AddEvent(ev.Kind, ev.SessionID, ev.NodeID, json.RawMessage(ev.Data))
	if err != nil {
		t.Fatal(err)
	}
	var topics []string
	client := pushCaptureClient(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("TTL") != "0" {
			t.Fatal("offline gateway queue enabled")
		}
		topics = append(topics, r.Header.Get("Topic"))
		return nil, errors.New("fixture unreachable")
	})
	h.pushSubscription(context.Background(), client, v, sub)
	h.pushSubscription(context.Background(), client, v, sub)
	if len(topics) != 4 {
		t.Fatal("must send once and retry exactly three times", len(topics))
	}
	for _, topic := range topics {
		if topic != topics[0] {
			t.Fatal("unstable retry topic")
		}
	}
	var cursor int64
	if err := h.S.Get("pushcursor:"+config.Hash(sub.Endpoint), &cursor); err != nil || cursor != stored.ID {
		t.Fatal("dropped alert retained for replay")
	}
	events, err := h.S.Events(0, true)
	if err != nil || len(events) != 1 {
		t.Fatal("dropping push deleted inbox")
	}
}

func TestHandledNativeRequestCancelsRetryAndReadResultsAreSkipped(t *testing.T) {
	h := testHub(t)
	sub := checkSubscription(t, h, "handled")
	v, _ := h.keys()
	ev := fixtureEvent()
	if _, err := h.S.AddEvent(ev.Kind, ev.SessionID, ev.NodeID, json.RawMessage(ev.Data)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := pushCaptureClient(func(r *http.Request) (*http.Response, error) {
		calls++
		// The user answers on the computer between attempts.
		h.mu.Lock()
		h.sessions[ev.SessionID] = core.Session{ID: ev.SessionID, TurnState: "working", ExecutionState: "alive"}
		h.sessionObserved[ev.SessionID] = time.Now()
		h.mu.Unlock()
		return nil, errors.New("temporary network failure")
	})
	h.pushSubscription(context.Background(), client, v, sub)
	if calls != 1 {
		t.Fatal("already handled request was retried", calls)
	}
	completed, err := h.S.AddEvent("completed", ev.SessionID, ev.NodeID, json.RawMessage(ev.Data))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.S.SetMetadata(ev.SessionID, core.Metadata{ReadAt: completed.At.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	h.pushSubscription(context.Background(), client, v, sub)
	if calls != 1 {
		t.Fatal("already read result was sent")
	}
}

func TestStaleAndSupersededAlertsAreNotDelivered(t *testing.T) {
	h := testHub(t)
	sub := checkSubscription(t, h, "old")
	v, _ := h.keys()
	ev := fixtureEvent()
	first, _ := h.S.AddEvent(ev.Kind, ev.SessionID, ev.NodeID, json.RawMessage(ev.Data))
	second, _ := h.S.AddEvent("completed", ev.SessionID, ev.NodeID, json.RawMessage(ev.Data))
	if h.pushEventCurrent(first) {
		t.Fatal("superseded request still eligible")
	}
	if _, err := h.S.DB.Exec("UPDATE events SET at=? WHERE id=?", time.Now().Add(-2*time.Minute).UTC().Format(time.RFC3339Nano), second.ID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := pushCaptureClient(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected send") })
	h.pushSubscription(context.Background(), client, v, sub)
	if calls != 0 {
		t.Fatal("old alert replayed")
	}
}
