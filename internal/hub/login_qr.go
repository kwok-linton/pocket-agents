package hub

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"pocketagents/internal/config"
	"pocketagents/internal/loginqr"
	"strings"
	"time"
)

const loginQRSlot = "login-qr:pending"

type pendingLoginQR struct {
	Hash      string    `json:"hash"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *Hub) createLoginQR(w http.ResponseWriter, r *http.Request) {
	origin, err := loginqr.PublicOrigin(h.C.PublicURL)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	code := config.Random() // 256 random bits, independent of the long-lived owner token.
	link := origin + "/#login=" + code
	png, err := loginqr.PNG(link)
	if err != nil {
		fail(w, 500, "unable to encode local login QR")
		return
	}
	expires := time.Now().UTC().Add(5 * time.Minute)
	// A single slot bounds storage and revokes any previously displayed QR.
	if err = h.S.Put(loginQRSlot, pendingLoginQR{Hash: config.Hash(code), ExpiresAt: expires}); err != nil {
		fail(w, 500, "unable to save one-time login")
		return
	}
	reply(w, loginqr.Response{URL: link, ExpiresAt: expires, ImageDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
}

func (h *Hub) redeemLoginQR(w http.ResponseWriter, r *http.Request) {
	if !h.origin(r) {
		fail(w, 403, "origin rejected")
		return
	}
	var v struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &v) {
		return
	}
	tx, err := h.S.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "unable to start login")
		return
	}
	defer tx.Rollback()
	var data []byte
	var pending pendingLoginQR
	err = tx.QueryRow("SELECT value FROM kv WHERE key=?", loginQRSlot).Scan(&data)
	if err != nil || json.Unmarshal(data, &pending) != nil || !time.Now().Before(pending.ExpiresAt) || !equal(pending.Hash, config.Hash(v.Code)) {
		fail(w, 403, "login code invalid, expired, or already used")
		return
	}
	token := config.Random()
	expires, _ := json.Marshal(time.Now().Add(30 * 24 * time.Hour))
	if _, err = tx.Exec("INSERT INTO kv VALUES(?,?)", "auth:"+config.Hash(token), expires); err == nil {
		_, err = tx.Exec("DELETE FROM kv WHERE key=?", loginQRSlot)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 500, "unable to complete one-time login")
		return
	}
	h.touchClient(r, config.Hash(token), time.Now().Add(30*24*time.Hour))
	h.sessionCookie(w, r, token)
	reply(w, map[string]bool{"authenticated": true})
}

func (h *Hub) sessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: "pocket_session", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil || (strings.HasPrefix(h.C.PublicURL, "https://") && !loopbackHost(r.Host)), SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
}

func loopbackHost(host string) bool {
	h, _, e := net.SplitHostPort(host)
	if e != nil {
		h = host
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Local owner CLI requests a short-lived link; no long-lived token enters the URL.
func (h *Hub) setupLink(w http.ResponseWriter, r *http.Request) {
	if !equal(bearer(r), h.Owner) || !loopbackHost(r.Host) {
		fail(w, 403, "local owner CLI required")
		return
	}
	code := config.Random()
	expires := time.Now().UTC().Add(5 * time.Minute)
	if h.S.Put(loginQRSlot, pendingLoginQR{Hash: config.Hash(code), ExpiresAt: expires}) != nil {
		fail(w, 500, "cannot create setup login")
		return
	}
	reply(w, map[string]any{"url": "http://" + h.C.Listen + "/setup#login=" + code, "expiresAt": expires})
}
