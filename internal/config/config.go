package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
)

type Config struct {
	DataDir     string         `json:"-"`
	NodeID      string         `json:"nodeId"`
	Name        string         `json:"name"`
	Listen      string         `json:"listen"`
	HubURL      string         `json:"hubUrl,omitempty"`
	PublicURL   string         `json:"publicUrl,omitempty"`
	NodeToken   string         `json:"nodeToken,omitempty"`
	Roots       []string       `json:"roots"`
	Profiles    []core.Profile `json:"profiles"`
	PATH        string         `json:"path"`
	PushSubject string         `json:"pushSubject,omitempty"`
}

func Random() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func DefaultDir() string {
	if x := os.Getenv("POCKET_HOME"); x != "" {
		return x
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".pocket-agents")
}
func Load(dir string) (*Config, error) {
	abs, e := filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(abs, 0700); e != nil {
		return nil, e
	}
	if e = os.Chmod(abs, 0700); e != nil {
		return nil, e
	}
	c := &Config{DataDir: abs}
	b, e := os.ReadFile(filepath.Join(abs, "config.json"))
	if e == nil {
		if e = json.Unmarshal(b, c); e != nil {
			return nil, e
		}
		c.DataDir = abs
		return c, c.Validate()
	}
	if !os.IsNotExist(e) {
		return nil, e
	}
	home, _ := os.UserHomeDir()
	name, _ := os.Hostname()
	c.NodeID = Random()
	c.Name = name
	c.Listen = "127.0.0.1:8787"
	c.Roots = []string{home}
	c.PATH = os.Getenv("PATH")
	for _, p := range []string{"claude", "codex"} {
		h := os.Getenv(strings.ToUpper(p) + "_HOME")
		if p == "claude" {
			h = os.Getenv("CLAUDE_CONFIG_DIR")
		}
		if h == "" {
			h = filepath.Join(home, "."+p)
		}
		bin, e := exec.LookPath(p)
		if e != nil {
			bin = p
		}
		c.Profiles = append(c.Profiles, core.Profile{Name: "default", Provider: p, Home: h, Binary: bin, DataDir: filepath.Join(abs, p)})
	}
	return c, c.Save()
}
func (c *Config) Validate() error {
	if c.NodeID == "" || c.Name == "" {
		return errors.New("config nodeId/name missing")
	}
	if len(c.Roots) == 0 {
		return errors.New("at least one allowed root required")
	}
	seen := map[string]bool{}
	for _, p := range c.Profiles {
		if p.ClaudeCreatePermissionMode != "" && (p.Provider != "claude" || (p.ClaudeCreatePermissionMode != "default" && p.ClaudeCreatePermissionMode != "bypassPermissions")) {
			return errors.New("claudeCreatePermissionMode requires Claude and default or bypassPermissions")
		}
		if p.Provider != "codex" && p.Provider != "claude" {
			return errors.New("unsupported provider")
		}
		if p.Name == "" || !filepath.IsAbs(p.Home) {
			return errors.New("profile requires name and absolute home")
		}
		k := p.Provider + ":" + p.Name
		if seen[k] {
			return errors.New("duplicate profile")
		}
		seen[k] = true
	}
	return nil
}
func (c *Config) Save() error {
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return Atomic(filepath.Join(c.DataDir, "config.json"), b)
}
func Atomic(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".pocket-tmp-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func Secret(dir, name string) (string, error) {
	p := filepath.Join(dir, name)
	b, e := os.ReadFile(p)
	if e == nil {
		return strings.TrimSpace(string(b)), nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	s := Random()
	return s, Atomic(p, []byte(s+"\n"))
}
func SessionID(node, provider, profile, native string) string {
	return Hash(node + "\x00" + provider + "\x00" + profile + "\x00" + native)
}
func CheckHub(raw string) error {
	u, e := url.Parse(raw)
	if e != nil {
		return e
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" || u.Path != "" && u.Path != "/" {
		return errors.New("hub URL must be an origin without credentials, query or path")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()) {
		return nil
	}
	return errors.New("remote Hub requires HTTPS; only loopback permits HTTP")
}
func ValidatePath(roots []string, path string, create bool) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("absolute working directory required")
	}
	real, e := filepath.EvalSymlinks(path)
	if e != nil && create && os.IsNotExist(e) {
		parent, pe := filepath.EvalSymlinks(filepath.Dir(path))
		if pe != nil {
			return "", pe
		}
		real = filepath.Join(parent, filepath.Base(path))
		e = nil
	}
	if e != nil {
		return "", fmt.Errorf("working directory unavailable: %w", e)
	}
	allowed := false
	for _, root := range roots {
		r, e := filepath.EvalSymlinks(root)
		if e != nil {
			continue
		}
		rel, e := filepath.Rel(r, real)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", errors.New("directory outside configured allowed roots")
	}
	if create {
		if e := os.Mkdir(real, 0755); e != nil && !os.IsExist(e) {
			return "", e
		}
	}
	st, e := os.Stat(real)
	if e != nil {
		return "", e
	}
	if !st.IsDir() {
		return "", errors.New("working directory is not a directory")
	}
	return real, nil
}

// CaptureNodeEnvironment persists only explicitly recognized native runtime
// settings on the execution device. Hub APIs never read or transmit this file.
func CaptureNodeEnvironment(c *Config) ([]string, error) {
	names := []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_BASE_URL", "OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "OPENAI_BASE_URL"}
	existing := map[string]string{}
	path := filepath.Join(c.DataDir, "node-env.json")
	if b, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(b, &existing); e != nil {
			return nil, e
		}
	}
	var captured []string
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			existing[name] = value
			captured = append(captured, name)
		}
	}
	if len(existing) == 0 {
		return captured, nil
	}
	b, e := json.Marshal(existing)
	if e != nil {
		return nil, e
	}
	return captured, Atomic(path, b)
}
func NodeEnvironment(c *Config) ([]string, error) {
	b, e := os.ReadFile(filepath.Join(c.DataDir, "node-env.json"))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var values map[string]string
	if e = json.Unmarshal(b, &values); e != nil {
		return nil, e
	}
	var env []string
	for key, value := range values {
		if strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return nil, errors.New("invalid node environment file")
		}
		if _, exists := os.LookupEnv(key); !exists {
			env = append(env, key+"="+value)
		}
	}
	return env, nil
}
