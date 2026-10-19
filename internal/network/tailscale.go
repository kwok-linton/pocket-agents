package network

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Peer struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OS         string `json:"os"`
	Online     bool   `json:"online"`
	Registered bool   `json:"registered"`
}
type Status struct {
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason"`
	SelfID  string `json:"selfId"`
	DNSName string `json:"dnsName"`
	Version string `json:"version"`
	Peers   []Peer `json:"peers"`
}

func Inspect(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	binary, e := exec.LookPath("tailscale")
	if e != nil {
		const mac = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
		if _, err := os.Stat(mac); err == nil {
			binary = mac
		} else {
			return Status{Reason: "Tailscale未安装或命令不可用，请安装并登录", Peers: []Peer{}}
		}
	}
	b, e := exec.CommandContext(ctx, binary, "status", "--json").Output()
	if e != nil {
		return Status{Reason: "Tailscale unavailable: install/start Tailscale and sign into your tailnet", Peers: []Peer{}}
	}
	type tsPeer struct {
		ID, HostName, OS, DNSName string
		Online                    bool
	}
	var v struct {
		BackendState, Version string
		Self                  tsPeer
		Peer                  map[string]tsPeer
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return Status{Reason: e.Error(), Peers: []Peer{}}
	}
	s := Status{Ready: v.BackendState == "Running", Reason: v.BackendState, SelfID: v.Self.ID, DNSName: v.Self.DNSName, Version: v.Version, Peers: []Peer{}}
	for _, p := range v.Peer {
		name := p.HostName
		if name == "" {
			name = strings.TrimSuffix(p.DNSName, ".")
		}
		if name == "" {
			name = p.ID
		}
		s.Peers = append(s.Peers, Peer{ID: p.ID, Name: name, OS: p.OS, Online: p.Online})
	}
	return s
}
