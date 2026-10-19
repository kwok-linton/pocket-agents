// Package loginqr encodes one-time login links entirely on the local machine.
package loginqr

import (
	"errors"
	"net"
	"net/url"
	"pocketagents/internal/config"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type Response struct {
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expiresAt"`
	ImageDataURL string    `json:"imageDataUrl"`
}

func PublicOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || config.CheckHub(raw) != nil || u.Scheme != "https" || strings.TrimSuffix(strings.ToLower(u.Hostname()), ".") == "localhost" {
		return "", errors.New("scan login requires a configured phone-accessible HTTPS publicUrl; configure Tailscale Serve and run pocket init --public-url HTTPS_ORIGIN")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		return "", errors.New("scan login publicUrl must be reachable from the phone, not loopback")
	}
	return strings.TrimRight(raw, "/"), nil
}

func PNG(raw string) ([]byte, error) {
	return qrcode.Encode(raw, qrcode.Medium, 384)
}
