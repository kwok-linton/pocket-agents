package codexnative

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("Codex RPC %d: %s", e.Code, e.Message) }

type rpcClient struct {
	conn      *websocket.Conn
	write     sync.Mutex
	mu        sync.Mutex
	seq       atomic.Int64
	pending   map[string]chan rpcMessage
	done      chan struct{}
	onEvent   func(rpcMessage)
	closeOnce sync.Once
}

func dialRPC(ctx context.Context, socket string, event func(rpcMessage)) (*rpcClient, error) {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	c, _, err := d.DialContext(ctx, "ws://localhost/", http.Header{})
	if err != nil {
		return nil, fmt.Errorf("connect shared Codex daemon: %w; run codex app-server daemon start", err)
	}
	r := &rpcClient{conn: c, pending: map[string]chan rpcMessage{}, done: make(chan struct{}), onEvent: event}
	c.SetReadLimit(32 << 20)
	go r.read()
	var init json.RawMessage
	err = r.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "pocket_agents", "version": "0.1.0"}, "capabilities": map[string]any{"experimentalApi": true}}, &init)
	if err != nil {
		r.close()
		return nil, err
	}
	if err = r.send(map[string]any{"method": "initialized"}); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}
func (r *rpcClient) read() {
	defer r.close()
	for {
		var m rpcMessage
		if err := r.conn.ReadJSON(&m); err != nil {
			return
		}
		if m.Method != "" {
			if r.onEvent != nil {
				r.onEvent(m)
			}
			continue
		}
		r.mu.Lock()
		ch := r.pending[string(m.ID)]
		r.mu.Unlock()
		if ch != nil {
			select {
			case ch <- m:
			default:
			}
		}
	}
}
func (r *rpcClient) send(v any) error {
	r.write.Lock()
	defer r.write.Unlock()
	r.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return r.conn.WriteJSON(v)
}
func (r *rpcClient) call(ctx context.Context, method string, params any, out any) error {
	id := fmt.Sprintf("pocket-%d", r.seq.Add(1))
	raw, _ := json.Marshal(id)
	key := string(raw)
	ch := make(chan rpcMessage, 1)
	r.mu.Lock()
	r.pending[key] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, key); r.mu.Unlock() }()
	if err := r.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("delivery unknown: %w", err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out != nil {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("delivery unknown: %w", ctx.Err())
	case <-r.done:
		return errors.New("delivery unknown: Codex control connection closed")
	}
}
func (r *rpcClient) close() { r.closeOnce.Do(func() { close(r.done); r.conn.Close() }) }
