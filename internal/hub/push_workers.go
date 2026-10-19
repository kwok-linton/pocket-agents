package hub

import (
	"context"
	"encoding/json"
	"pocketagents/internal/config"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func (h *Hub) pushSubscriptions() ([]webpush.Subscription, error) {
	rows, err := h.S.DB.Query("SELECT data FROM push")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []webpush.Subscription
	for rows.Next() {
		var b []byte
		var sub webpush.Subscription
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if json.Unmarshal(b, &sub) == nil && validEndpoint(sub.Endpoint) {
			subs = append(subs, sub)
		}
	}
	return subs, rows.Err()
}

func (h *Hub) runPush(ctx context.Context, client webpush.HTTPClient, v vapid) {
	var workers sync.WaitGroup
	defer workers.Wait()
	jobs := make(chan webpush.Subscription)
	done := make(chan string, 8)
	// One queued/in-flight job per subscription; a slow phone does not hold up
	// other phones. Both network concurrency and each catch-up run are bounded.
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case sub := <-jobs:
					jobctx, cancel := context.WithTimeout(ctx, 30*time.Second)
					h.pushSubscription(jobctx, client, v, sub)
					cancel()
					select {
					case done <- config.Hash(sub.Endpoint):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	// Diagnostic tests have a separate worker and cannot block real events.
	workers.Add(1)
	go func() {
		defer workers.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if subs, err := h.pushSubscriptions(); err == nil {
					h.sendPushChecks(ctx, client, v, subs)
				}
			}
		}
	}()

	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	busy := map[string]bool{}
	var pending []webpush.Subscription
	queue := func() {
		subs, err := h.pushSubscriptions()
		if err != nil {
			return
		}
		for _, sub := range subs {
			id := config.Hash(sub.Endpoint)
			if !busy[id] {
				busy[id] = true
				pending = append(pending, sub)
			}
		}
	}
	// Startup never replays old notifications. The inbox keeps the events.
	if subs, err := h.pushSubscriptions(); err == nil {
		var newest int64
		if h.S.DB.QueryRow("SELECT COALESCE(MAX(id),0) FROM events").Scan(&newest) == nil {
			for _, sub := range subs {
				_ = h.S.Put("pushcursor:"+config.Hash(sub.Endpoint), newest)
			}
		}
	}
	queue()
	for {
		var ready chan webpush.Subscription
		var next webpush.Subscription
		if len(pending) > 0 {
			ready, next = jobs, pending[0]
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			queue()
		case <-h.pushWake:
			queue()
		case id := <-done:
			delete(busy, id)
		case ready <- next:
			pending = pending[1:]
		}
	}
}
