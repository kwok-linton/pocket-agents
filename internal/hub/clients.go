package hub

import (
	"encoding/json"
	"net/http"
	"pocketagents/internal/config"
	"strings"
	"time"
)

type browserClient struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	UserAgent string    `json:"userAgent"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
	ExpiresAt time.Time `json:"expiresAt"`
	Current   bool      `json:"current"`
}

func clientRecord(r *http.Request, hash string, expiry time.Time) browserClient {
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	name := "浏览器"
	switch {
	case strings.Contains(ua, "iPhone"):
		name = "iPhone"
	case strings.Contains(ua, "iPad"):
		name = "iPad"
	case strings.Contains(ua, "Android"):
		name = "Android"
	case strings.Contains(ua, "Macintosh"):
		name = "Mac 浏览器"
	case strings.Contains(ua, "Windows"):
		name = "Windows 浏览器"
	case strings.Contains(ua, "Linux"):
		name = "Linux 浏览器"
	}
	return browserClient{ID: hash, Name: name, UserAgent: ua, CreatedAt: time.Now().UTC(), LastSeen: time.Now().UTC(), ExpiresAt: expiry}
}
func (h *Hub) touchClient(r *http.Request, hash string, expiry time.Time) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	var c browserClient
	if h.S.Get("client:"+hash, &c) != nil {
		c = clientRecord(r, hash, expiry)
	} else if time.Since(c.LastSeen) < time.Minute {
		return
	}
	c.LastSeen = time.Now().UTC()
	_ = h.S.Put("client:"+hash, c)
}
func (h *Hub) listClients(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.DB.Query("SELECT key,value FROM kv WHERE key LIKE 'auth:%'")
	if err != nil {
		fail(w, 500, "cannot read browser logins")
		return
	}
	type auth struct {
		id     string
		expiry time.Time
	}
	entries := []auth{}
	for rows.Next() {
		var k string
		var b []byte
		var e time.Time
		if rows.Scan(&k, &b) == nil && json.Unmarshal(b, &e) == nil && time.Now().Before(e) {
			entries = append(entries, auth{strings.TrimPrefix(k, "auth:"), e})
		}
	}
	rows.Close()
	current := ""
	if c, e := r.Cookie("pocket_session"); e == nil {
		current = config.Hash(c.Value)
	}
	out := []browserClient{}
	for _, a := range entries {
		var c browserClient
		if h.S.Get("client:"+a.id, &c) != nil {
			c = browserClient{ID: a.id, Name: "旧版登录（设备未知）"}
		}
		c.ExpiresAt = a.expiry
		c.Current = a.id == current
		out = append(out, c)
	}
	reply(w, out)
}
func (h *Hub) changeClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
		fail(w, 400, "invalid client id")
		return
	}
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	if r.Method == "DELETE" {
		tx, e := h.S.DB.Begin()
		if e != nil {
			fail(w, 500, "cannot revoke login")
			return
		}
		defer tx.Rollback()
		if _, e = tx.Exec("DELETE FROM kv WHERE key IN (?,?)", "auth:"+id, "client:"+id); e != nil {
			fail(w, 500, "cannot revoke login")
			return
		}
		if tx.Commit() != nil {
			fail(w, 500, "cannot revoke login")
			return
		}
		reply(w, map[string]bool{"revoked": true})
		return
	}
	var v struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &v) {
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len([]rune(v.Name)) > 80 {
		fail(w, 400, "名称应为1–80个字符")
		return
	}
	var expiry time.Time
	if h.S.Get("auth:"+id, &expiry) != nil || !time.Now().Before(expiry) {
		fail(w, 404, "login expired or revoked")
		return
	}
	var c browserClient
	if h.S.Get("client:"+id, &c) != nil {
		c = browserClient{ID: id, ExpiresAt: expiry}
	}
	c.Name = v.Name
	if h.S.Put("client:"+id, c) != nil {
		fail(w, 500, "cannot save name")
		return
	}
	reply(w, c)
}
