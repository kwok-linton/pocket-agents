package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"pocketagents/internal/config"
	"regexp"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// No endpoint, message content or transport error is exposed in a check.
// Claim durably before sending: after a crash or ambiguous transport outcome,
// never automatically resend a notification that may already have arrived.
type pushCheck struct {
	ID             string    `json:"id"`
	SubscriptionID string    `json:"subscriptionId"`
	DueAt          time.Time `json:"dueAt"`
	State          string    `json:"state"`
	HTTPStatus     int       `json:"httpStatus"`
	Reason         string    `json:"reason,omitempty"`
}

var pushCheckID = regexp.MustCompile(`^[a-f0-9-]{36}$`)
var pushSubscriptionID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (h *Hub) queuePushCheck(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID             string `json:"id"`
		SubscriptionID string `json:"subscriptionId"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !pushCheckID.MatchString(input.ID) || !pushSubscriptionID.MatchString(input.SubscriptionID) {
		fail(w, 400, "invalid push test request")
		return
	}
	ids, err := h.activePushSubscriptionIDs()
	if err != nil {
		fail(w, 500, "unable to read active push subscriptions")
		return
	}
	found := false
	for _, id := range ids {
		if id == input.SubscriptionID {
			found = true
		}
	}
	if !found {
		fail(w, 409, "push subscription missing; register this device first")
		return
	}
	tx, err := h.S.DB.Begin()
	if err != nil {
		fail(w, 500, "unable to save push test")
		return
	}
	defer tx.Rollback()
	var previous []byte
	err = tx.QueryRow("SELECT value FROM kv WHERE key=?", "pushtest:"+input.ID).Scan(&previous)
	if err == nil {
		var check pushCheck
		if json.Unmarshal(previous, &check) != nil || check.SubscriptionID != input.SubscriptionID {
			fail(w, 409, "push test ID already used")
			return
		}
		reply(w, check)
		return
	}
	if err != sql.ErrNoRows {
		fail(w, 500, "unable to save push test")
		return
	}
	var count int
	err = tx.QueryRow(`SELECT COUNT(*) FROM kv WHERE key LIKE 'pushtest:%' AND json_extract(value,'$.subscriptionId')=? AND json_extract(value,'$.dueAt')>?`, input.SubscriptionID, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)).Scan(&count)
	if err != nil {
		fail(w, 500, "unable to save push test")
		return
	}
	if count > 0 {
		fail(w, 429, "wait one minute before another push test")
		return
	}
	check := pushCheck{ID: input.ID, SubscriptionID: input.SubscriptionID, DueAt: time.Now().Add(10 * time.Second).UTC(), State: "queued"}
	b, _ := json.Marshal(check)
	if _, err = tx.Exec("INSERT INTO kv VALUES(?,?)", "pushtest:"+check.ID, b); err != nil {
		fail(w, 500, "unable to save push test")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "unable to save push test")
		return
	}
	reply(w, check)
}

func (h *Hub) pushChecks() ([]pushCheck, error) {
	return h.readPushChecks(false)
}

func (h *Hub) readPushChecks(pending bool) ([]pushCheck, error) {
	query := `SELECT value FROM kv WHERE key LIKE 'pushtest:%' ORDER BY json_extract(value,'$.dueAt') DESC LIMIT 100`
	if pending {
		query = `SELECT value FROM kv WHERE key LIKE 'pushtest:%' AND json_extract(value,'$.state')='queued' ORDER BY json_extract(value,'$.dueAt') ASC LIMIT 100`
	}
	rows, err := h.S.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := []pushCheck{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var check pushCheck
		if json.Unmarshal(b, &check) == nil {
			checks = append(checks, check)
		}
	}
	return checks, rows.Err()
}

func (h *Hub) sendPushChecks(ctx context.Context, client webpush.HTTPClient, v vapid, subs []webpush.Subscription) {
	checks, err := h.readPushChecks(true)
	if err != nil {
		return
	}
	for _, check := range checks {
		if check.State != "queued" || time.Now().Before(check.DueAt) {
			continue
		}
		// Optimistic claim also protects against overlapping workers.
		before, _ := json.Marshal(check)
		check.State = "unknown"
		claimed, _ := json.Marshal(check)
		result, err := h.S.DB.Exec("UPDATE kv SET value=? WHERE key=? AND value=?", claimed, "pushtest:"+check.ID, before)
		if err != nil {
			continue
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			continue
		}
		var sub *webpush.Subscription
		for i := range subs {
			if config.Hash(subs[i].Endpoint) == check.SubscriptionID {
				sub = &subs[i]
				break
			}
		}
		if sub == nil || !validEndpoint(sub.Endpoint) {
			check.State = "expired"
			_ = h.S.Put("pushtest:"+check.ID, check)
			continue
		}
		payload, _ := json.Marshal(map[string]any{"title": "Pocket Agents", "body": "测试通知已到达，点击查看通知设置", "tag": check.ID + ":test", "testId": check.ID, "url": "/settings"})
		options := h.pushOptions(client, v, check.ID)
		options.TTL = 120
		response, sendErr := webpush.SendNotificationWithContext(ctx, payload, sub, options)
		check.Reason = pushTransportReason(sendErr)
		if response != nil {
			check.HTTPStatus = response.StatusCode
			if sendErr == nil {
				check.Reason = pushRejectionReason(response)
			}
			if response.Body != nil {
				response.Body.Close()
			}
		}
		if sendErr == nil && response != nil {
			check.State = "rejected"
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				check.State = "accepted"
			}
			if response.StatusCode == 404 || response.StatusCode == 410 {
				check.State = "expired"
				_, _ = h.S.DB.Exec("DELETE FROM push WHERE endpoint=?", sub.Endpoint)
			}
		}
		_ = h.S.Put("pushtest:"+check.ID, check)
	}
}
