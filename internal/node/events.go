package node

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// Notifications have their own HTTP transport and durable cursor. In
// particular, a blocked history result or disconnected RPC websocket cannot
// hold this sender's write lock or consume its connection.
func (n *Node) forwardEvents(ctx context.Context) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("node event redirects refused")
	}}
	delay := 500 * time.Millisecond
	for ctx.Err() == nil {
		more, err := n.sendEvents(ctx, client)
		wait := 500 * time.Millisecond
		if err != nil {
			// Never log the request URL or its credentials.
			log.Print("notification channel unavailable; saved events will retry")
			wait = delay
			delay = min(15*time.Second, delay*2)
		} else {
			delay = 500 * time.Millisecond
			if more {
				continue
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (n *Node) sendEvents(ctx context.Context, client *http.Client) (bool, error) {
	var cursor int64
	if err := n.S.Get("event-ack", &cursor); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	events, err := n.S.Events(cursor, true)
	if err != nil || len(events) == 0 {
		return false, err
	}
	// Small batches let notifications make progress on constrained links.
	events = events[:min(len(events), 32)]
	data, err := json.Marshal(events)
	if err != nil {
		return false, err
	}
	u, err := url.Parse(n.C.HubURL)
	if err != nil {
		return false, err
	}
	u.Path, u.RawQuery, u.Fragment = "/api/node/events", "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+n.C.NodeToken)
	req.Header.Set("X-Pocket-Node", n.C.NodeID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("notification channel status %d", resp.StatusCode)
	}
	var reply struct {
		Ack int64 `json:"ack"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&reply); err != nil {
		return false, err
	}
	for _, ev := range events {
		if reply.Ack == ev.ID {
			// Persist only an explicit acknowledgement of this batch. Failed
			// persistence replays the same IDs; the receiver deduplicates them.
			return true, n.S.Put("event-ack", reply.Ack)
		}
	}
	return false, errors.New("invalid notification acknowledgement")
}
