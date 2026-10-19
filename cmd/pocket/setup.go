package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/service"
	"runtime"
	"strings"
	"time"
)

func openSetup(ctx context.Context, c *config.Config, install bool, role string) error {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("setup requires a numeric loopback listen address")
	}
	if install {
		if role != "both" && role != "hub" {
			return errors.New("setup supports hub or both; use pair then service install --role node on execution devices")
		}
		if role == "both" {
			if _, err = config.CaptureNodeEnvironment(c); err != nil {
				return err
			}
		}
		if _, err = service.Install(c, role); err != nil {
			return err
		}
		probe := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
		resp, e := probe.Get("http://" + c.Listen + "/api/bootstrap")
		if e == nil {
			resp.Body.Close()
			if err = service.Manage("restart"); err != nil {
				return err
			}
		} else if err = service.Manage("start"); err != nil {
			return err
		}
	}
	credential, err := os.ReadFile(filepath.Join(c.DataDir, "owner-token"))
	// First service startup creates the owner secret asynchronously.
	for attempt := 0; err != nil && install && attempt < 20; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		credential, err = os.ReadFile(filepath.Join(c.DataDir, "owner-token"))
	}
	if err != nil {
		return errors.New("local Hub unavailable; start its service first")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	defer client.CloseIdleConnections()
	var link struct {
		URL string `json:"url"`
	}
	for attempt := 0; attempt < 20; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+c.Listen+"/api/setup/link", nil)
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(credential)))
		resp, e := client.Do(req)
		if e != nil {
			if !install {
				return e
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("setup link refused (HTTP %d); upgrade/start the local Hub", resp.StatusCode)
		}
		e = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&link)
		resp.Body.Close()
		if e != nil {
			return e
		}
		break
	}
	prefix := "http://" + c.Listen + "/setup#login="
	if !strings.HasPrefix(link.URL, prefix) || len(strings.TrimPrefix(link.URL, prefix)) != 64 {
		return errors.New("invalid local setup URL")
	}
	var command string
	if runtime.GOOS == "darwin" {
		command = "open"
	} else if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		command = "xdg-open"
	}
	if command != "" {
		if err = exec.CommandContext(ctx, command, link.URL).Run(); err == nil {
			fmt.Println("已打开本地接入向导。登录链接5分钟有效、仅使用一次。")
			return nil
		}
	}
	fmt.Println("在本机浏览器打开以下一次性链接（5分钟有效，请勿分享）：\n" + link.URL)
	fmt.Println("无桌面服务器：从自己的电脑 SSH 转发此 loopback 端口，再打开链接；不要将 8787 暴露到公网。")
	return nil
}
