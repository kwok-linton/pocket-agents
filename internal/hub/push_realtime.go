package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"pocketagents/internal/core"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Realtime alerts expire locally too, so delayed Node events remain available
// in the inbox without becoming old lock-screen reminders.
func (h *Hub) pushEventCurrent(ev core.Event) bool {
	if ev.At.IsZero() || time.Since(ev.At) > time.Minute {
		return false
	}
	var latest int64
	if h.S.DB.QueryRow("SELECT COALESCE(MAX(id),0) FROM events WHERE session=? AND kind IN ('completed','waiting_input','waiting_approval','error','interrupted')", ev.SessionID).Scan(&latest) != nil || latest != ev.ID {
		return false
	}
	metadata := h.S.Metadata(ev.SessionID)
	waiting := ev.Kind == "waiting_input" || ev.Kind == "waiting_approval"
	if !waiting && !metadata.ReadAt.Before(ev.At) {
		return false
	}
	h.mu.RLock()
	session, exists := h.sessions[ev.SessionID]
	observed := h.sessionObserved[ev.SessionID]
	h.mu.RUnlock()
	// An older snapshot cannot cancel a new event that arrived on the faster
	// notification channel. A newer native observation can cancel it immediately.
	if exists && observed.After(ev.At) && (session.TurnState != ev.Kind || (waiting && session.ExecutionState == "exited")) {
		return false
	}
	return true
}

func (h *Hub) deliverRealtime(ctx context.Context, client webpush.HTTPClient, v vapid, sub webpush.Subscription, ev core.Event) bool {
	body, _ := json.Marshal(map[string]any{"title": "Pocket Agents", "body": pushLabel(ev.Kind), "tag": pushEventTag(ev), "url": "/session/" + ev.SessionID, "eventId": ev.ID})
	// Initial attempt plus at most three retries: 1s, 2s, 4s. Cursor is already
	// advanced, so cancellation/restart never resumes a partially tried alert.
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Second * time.Duration(1<<(attempt-1)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return false
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return false
		}
		allowed, err := h.pushEventAllowed(ev)
		if err != nil || !allowed || !h.pushEventCurrent(ev) {
			return true
		}
		attemptAt := time.Now()
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		response, sendErr := webpush.SendNotificationWithContext(sendCtx, body, &sub, h.pushOptions(client, v, pushEventTag(ev)))
		_ = h.recordPushDelivery(sub.Endpoint, ev.ID, attemptAt, response, sendErr)
		status := 0
		if response != nil {
			status = response.StatusCode
			if response.Body != nil {
				response.Body.Close()
			}
		}
		cancel()
		if status == http.StatusNotFound || status == http.StatusGone {
			_, _ = h.S.DB.Exec("DELETE FROM push WHERE endpoint=?", sub.Endpoint)
			return false
		}
		if sendErr == nil && status >= 200 && status < 300 {
			return true
		}
		if sendErr == nil && status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			return true
		}
	}
	return true // retry budget exhausted; do not queue this alert again
}
