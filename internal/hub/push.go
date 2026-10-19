package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type vapid struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

// Subscription IDs are endpoint hashes. Never persist transport error strings:
// net/http errors commonly contain the endpoint's secret capability URL.
type pushDelivery struct {
	SubscriptionID string    `json:"subscriptionId"`
	EventID        int64     `json:"eventId"`
	AttemptAt      time.Time `json:"attemptAt"`
	HTTPStatus     int       `json:"httpStatus"`
	Accepted       bool      `json:"accepted"`
	TransportError string    `json:"transportError,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

func (h *Hub) recordPushDelivery(endpoint string, eventID int64, attempted time.Time, response *http.Response, sendError error) error {
	d := pushDelivery{SubscriptionID: config.Hash(endpoint), EventID: eventID, AttemptAt: attempted.UTC()}
	if response != nil {
		d.HTTPStatus = response.StatusCode
		d.Reason = pushRejectionReason(response)
	}
	d.Accepted = sendError == nil && d.HTTPStatus >= 200 && d.HTTPStatus < 300
	if sendError != nil {
		d.TransportError = "push request failed before acceptance; check Hub connectivity and subscription"
		d.Reason = pushTransportReason(sendError)
	}
	return h.S.Put("pushdelivery:"+d.SubscriptionID, d)
}

func (h *Hub) pushDeliveries() ([]pushDelivery, error) {
	rows, err := h.S.DB.Query("SELECT value FROM kv WHERE key LIKE 'pushdelivery:%' ORDER BY json_extract(value,'$.attemptAt') DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []pushDelivery{}
	for rows.Next() {
		var data []byte
		var d pushDelivery
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if json.Unmarshal(data, &d) == nil {
			items = append(items, d)
		}
	}
	return items, rows.Err()
}

func (h *Hub) keys() (vapid, error) {
	h.vapidMu.Lock()
	defer h.vapidMu.Unlock()
	var v vapid
	if e := h.S.Get("vapid", &v); e == nil {
		return v, nil
	}
	private, public, e := webpush.GenerateVAPIDKeys()
	if e != nil {
		return v, e
	}
	v = vapid{public, private}
	return v, h.S.Put("vapid", v)
}
func (h *Hub) pushInfo(w http.ResponseWriter, r *http.Request) {
	v, e := h.keys()
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	deliveries, e := h.pushDeliveries()
	if e != nil {
		fail(w, 500, "unable to read push delivery diagnostics")
		return
	}
	activeIDs, e := h.activePushSubscriptionIDs()
	if e != nil {
		fail(w, 500, "unable to read active push subscriptions")
		return
	}
	checks, e := h.pushChecks()
	if e != nil {
		fail(w, 500, "unable to read push tests")
		return
	}
	replyConditional(w, r, map[string]any{"tests": checks, "enabled": true, "publicKey": v.Public, "lastDelivery": deliveries, "activeSubscriptionIds": activeIDs, "note": "Requires HTTPS, browser permission, and Hub outbound access. iPhone requires installation on Home Screen. accepted means the push service accepted a request, not that the phone displayed it."})
}

func (h *Hub) activePushSubscriptionIDs() ([]string, error) {
	rows, e := h.S.DB.Query("SELECT endpoint FROM push ORDER BY endpoint")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var endpoint string
		if e := rows.Scan(&endpoint); e != nil {
			return nil, e
		}
		ids = append(ids, config.Hash(endpoint))
	}
	return ids, rows.Err()
}
func validEndpoint(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"fcm.googleapis.com", "updates.push.services.mozilla.com", "push.services.mozilla.com", "web.push.apple.com", "notify.windows.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}
func (h *Hub) subscribe(w http.ResponseWriter, r *http.Request) {
	var sub webpush.Subscription
	if !decode(w, r, &sub) {
		return
	}
	if !validEndpoint(sub.Endpoint) || sub.Keys.Auth == "" || sub.Keys.P256dh == "" {
		fail(w, 400, "invalid or unsupported push service endpoint")
		return
	}
	b, _ := json.Marshal(sub)
	tx, e := h.S.DB.Begin()
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	defer tx.Rollback()
	res, e := tx.Exec("INSERT OR IGNORE INTO push VALUES(?,?)", sub.Endpoint, b)
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		var last int64
		if e = tx.QueryRow("SELECT COALESCE(MAX(id),0) FROM events").Scan(&last); e == nil {
			cursor, _ := json.Marshal(last)
			_, e = tx.Exec("INSERT INTO kv VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "pushcursor:"+config.Hash(sub.Endpoint), cursor)
		}
	} else {
		// A browser can refresh keys without changing its endpoint. Keep the
		// existing delivery cursor, but always store the current encryption keys.
		_, e = tx.Exec("UPDATE push SET data=? WHERE endpoint=?", b, sub.Endpoint)
	}
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, e.Error())
		return
	}
	reply(w, map[string]bool{"enabled": true})
}
func (h *Hub) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &v) {
		return
	}
	_, e := h.S.DB.Exec("DELETE FROM push WHERE endpoint=?", v.Endpoint)
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	_, _ = h.S.DB.Exec("DELETE FROM kv WHERE key=?", "pushdelivery:"+config.Hash(v.Endpoint))
	reply(w, map[string]bool{"enabled": false})
}
func (h *Hub) RunPush(ctx context.Context) {
	v, err := h.keys()
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("push redirects refused") }}
	h.runPush(ctx, client, v)
}

func (h *Hub) pushSubscription(ctx context.Context, client webpush.HTTPClient, v vapid, s webpush.Subscription) {
	cursorKey := "pushcursor:" + config.Hash(s.Endpoint)
	var cursor int64
	if err := h.S.Get(cursorKey, &cursor); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	events, err := h.S.Events(cursor, true)
	if err != nil {
		return
	}
	for _, ev := range events {
		if ctx.Err() != nil {
			return
		}
		allowed, err := h.pushEventAllowed(ev)
		if err != nil {
			return
		}
		// Claim before sending. A crash, lost response or expired retry budget
		// must not turn this realtime alert into a replay after reconnect.
		if h.S.Put(cursorKey, ev.ID) != nil {
			return
		}
		if !allowed {
			continue
		}
		if !h.deliverRealtime(ctx, client, v, s, ev) {
			return
		}
	}
}

func (h *Hub) pushSessionAllowed(sessionID string) (bool, error) {
	return h.pushEventAllowed(core.Event{SessionID: sessionID})
}

func pushEventTag(ev core.Event) string {
	// Distinct events must not replace one another at the gateway or on the
	// phone. Retries of one event still share a stable topic/tag. Keep kind
	// last for the service worker's language selection.
	return strconv.FormatInt(ev.ID, 10) + ":" + ev.SessionID + ":" + ev.Kind
}

func (h *Hub) pushEventAllowed(ev core.Event) (bool, error) {
	p, err := h.loadPreferences()
	if err != nil {
		return false, err
	}
	var data []byte
	var session core.Session
	err = h.S.DB.QueryRow("SELECT data FROM sessions WHERE id=?", ev.SessionID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		var valid bool
		session, valid = notificationSession(ev)
		if !valid {
			return false, nil // legacy event for a removed session
		}
	} else if err != nil {
		return false, err
	} else if err = json.Unmarshal(data, &session); err != nil {
		return false, err
	}
	if session.ParentID != "" || (ev.Kind == "completed" && session.ExecutionState == "exited") {
		return false, nil
	}
	var metadata core.Metadata
	err = h.S.DB.QueryRow("SELECT data FROM metadata WHERE id=?", ev.SessionID).Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		if err = json.Unmarshal(data, &metadata); err != nil {
			return false, err
		}
	}
	if metadata.Hidden {
		return false, nil
	}
	if p.WorkspaceScope == "all" {
		return true, nil
	}
	if !path.IsAbs(session.Cwd) || strings.ContainsRune(session.Cwd, '\x00') {
		return false, nil
	}
	cwd := path.Clean(session.Cwd)
	for _, workspace := range p.Workspaces {
		if session.NodeID != workspace.NodeID {
			continue
		}
		root := path.Clean(workspace.Path)
		if cwd == root || strings.HasPrefix(cwd, strings.TrimSuffix(root, "/")+"/") {
			return true, nil
		}
	}
	return false, nil
}

func (h *Hub) pushOptions(client webpush.HTTPClient, v vapid, topic string) *webpush.Options {
	subject := strings.TrimSpace(h.C.PushSubject)
	// Older installations used a localhost placeholder that Apple rejects as
	// BadJwtToken. Keep real operator contacts; otherwise use this Hub's HTTPS
	// identity. Do not rotate VAPID keys or invalidate browser subscriptions.
	if subject == "" || subject == "mailto:owner@localhost" || subject == "owner@localhost" {
		subject = h.C.PublicURL
	}
	return &webpush.Options{
		HTTPClient: client,
		// webpush-go v1.4.0 adds mailto: itself for non-HTTPS subscribers.
		Subscriber:      strings.TrimPrefix(subject, "mailto:"),
		VAPIDPublicKey:  v.Public,
		VAPIDPrivateKey: v.Private,
		TTL:             0, // deliver now; gateways must not retain it for an offline phone
		Topic:           config.Hash(topic)[:32],
		// These events always result in visible notifications and need timely
		// attention, including when an Android device is idle.
		Urgency: webpush.UrgencyHigh,
	}
}

func pushLabel(kind string) string {
	switch kind {
	case "completed":
		return "本轮已结束，点击查看"
	case "waiting_approval":
		return "有一项原生请求等待批准"
	case "waiting_input":
		return "Agent 正在等待你的回答"
	case "interrupted":
		return "当前轮次已中断"
	default:
		return "会话需要你查看"
	}
}
