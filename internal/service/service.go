package service

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"pocketagents/internal/config"
	"runtime"
	"strconv"
	"strings"
)

const label = "local.pocket-agents"

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func Install(c *config.Config, role string) (string, error) {
	if role != "hub" && role != "node" && role != "both" {
		return "", errors.New("role must be hub, node or both")
	}
	home, _ := os.UserHomeDir()
	exe, e := os.Executable()
	if e != nil {
		return "", e
	}
	dir := filepath.Join(home, ".local", "bin")
	if e = os.MkdirAll(dir, 0755); e != nil {
		return "", e
	}
	target := filepath.Join(dir, "pocket")
	if exe != target {
		src, e := os.Open(exe)
		if e != nil {
			return "", e
		}
		defer src.Close()
		tmp, e := os.CreateTemp(dir, ".pocket-install-*")
		if e != nil {
			return "", e
		}
		defer os.Remove(tmp.Name())
		_, e = io.Copy(tmp, src)
		if e == nil {
			e = tmp.Chmod(0755)
		}
		ce := tmp.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return "", e
		}
		if e = checkUpgradeIdentity(target, tmp.Name()); e != nil {
			return "", e
		}
		if e = os.Rename(tmp.Name(), target); e != nil {
			return "", e
		}
	}
	logdir := filepath.Join(c.DataDir, "logs")
	if e = os.MkdirAll(logdir, 0700); e != nil {
		return "", e
	}
	switch runtime.GOOS {
	case "darwin":
		if role != "node" {
			if e = installLauncher(home, target, c.DataDir); e != nil {
				return "", e
			}
		}
		path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return "", e
		}
		s := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>%s</string><string>--data</string><string>%s</string></array><key>EnvironmentVariables</key><dict><key>HOME</key><string>%s</string><key>PATH</key><string>%s</string></dict><key>WorkingDirectory</key><string>%s</string><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string><key>ProcessType</key><string>Background</string></dict></plist>`, label, esc(target), role, esc(c.DataDir), esc(home), esc(c.PATH), esc(home), esc(filepath.Join(logdir, "service.log")), esc(filepath.Join(logdir, "service.log")))
		return path, config.Atomic(path, []byte(s))
	case "linux":
		path := filepath.Join(home, ".config", "systemd", "user", "pocket-agents.service")
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return "", e
		}
		s := fmt.Sprintf("[Unit]\nDescription=Pocket Agents\nAfter=network-online.target\n[Service]\nType=simple\nExecStart=%s %s --data %s\nEnvironment=%s\nWorkingDirectory=%s\nRestart=on-failure\nRestartSec=3\nKillMode=process\nUMask=0077\n[Install]\nWantedBy=default.target\n", strconv.Quote(target), role, strconv.Quote(c.DataDir), strconv.Quote("PATH="+c.PATH), strconv.Quote(home))
		return path, config.Atomic(path, []byte(s))
	default:
		return "", errors.New("service installation supported on macOS/Linux only")
	}
}
func Manage(action string) error {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
		switch action {
		case "start":
			return run("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), path)
		case "restart":
			return run("launchctl", "kickstart", "-k", target)
		case "stop":
			return run("launchctl", "bootout", target)
		case "status":
			return run("launchctl", "print", target)
		case "uninstall":
			_ = run("launchctl", "bootout", target)
			if e := os.Remove(path); e != nil && !os.IsNotExist(e) {
				return e
			}
			return nil
		}
	case "linux":
		switch action {
		case "start":
			if e := run("systemctl", "--user", "daemon-reload"); e != nil {
				return e
			}
			return run("systemctl", "--user", "enable", "--now", "pocket-agents.service")
		case "restart", "stop", "status":
			return run("systemctl", "--user", action, "pocket-agents.service")
		case "uninstall":
			_ = run("systemctl", "--user", "disable", "--now", "pocket-agents.service")
			if e := os.Remove(filepath.Join(home, ".config", "systemd", "user", "pocket-agents.service")); e != nil && !os.IsNotExist(e) {
				return e
			}
			return run("systemctl", "--user", "daemon-reload")
		}
	}
	return errors.New("unknown service action/platform")
}
func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
