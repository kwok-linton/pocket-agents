package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/loginqr"
	"strings"
	"time"
)

func createLoginQRFile(ctx context.Context, c *config.Config, output string) (string, time.Time, error) {
	origin, err := loginqr.PublicOrigin(c.PublicURL)
	if err != nil {
		return "", time.Time{}, err
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || (host != "localhost" && !net.ParseIP(host).IsLoopback()) {
		return "", time.Time{}, errors.New("login-qr must contact a local loopback Hub")
	}
	credential, err := os.ReadFile(filepath.Join(c.DataDir, "owner-token"))
	if err != nil {
		return "", time.Time{}, errors.New("local owner credential unavailable; run login-qr on the Hub machine")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+c.Listen+"/api/login/qr", nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(credential)))
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("login QR redirects refused") }}
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("local Hub unavailable; start its service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("local Hub refused QR login (HTTP %d); check its publicUrl and owner credential", res.StatusCode)
	}
	var qr loginqr.Response
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&qr); err != nil {
		return "", time.Time{}, errors.New("invalid login QR response")
	}
	u, err := url.Parse(qr.URL)
	if err != nil || u.Scheme+"://"+u.Host != origin || u.Path != "/" || u.RawQuery != "" || !strings.HasPrefix(u.Fragment, "login=") || len(strings.TrimPrefix(u.Fragment, "login=")) != 64 || !time.Now().Before(qr.ExpiresAt) {
		return "", time.Time{}, errors.New("login QR response does not match the configured HTTPS origin or expiry")
	}
	// Encode locally; never fetch an image or send the link to an external service.
	png, err := loginqr.PNG(qr.URL)
	if err != nil {
		return "", time.Time{}, err
	}
	if output == "" {
		output = filepath.Join(c.DataDir, "login-qr.png")
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return "", time.Time{}, err
	}
	if err = config.Atomic(output, png); err != nil {
		return "", time.Time{}, err
	}
	return output, qr.ExpiresAt, nil
}
