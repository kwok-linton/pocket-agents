package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"pocketagents/internal/claudenative"
	"pocketagents/internal/codexnative"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/hub"
	"pocketagents/internal/network"
	"pocketagents/internal/node"
	"pocketagents/internal/service"
	"pocketagents/internal/store"
	"pocketagents/internal/web"
	"strings"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func output(v any) { enc := json.NewEncoder(os.Stdout); enc.SetIndent("", "  "); _ = enc.Encode(v) }
func run() error {
	if len(os.Args) < 2 {
		usage()
		return nil
	}
	command := os.Args[1]
	if command == "--version" || command == "version" {
		fmt.Println("Pocket Agents 0.1.0")
		return nil
	}
	if command == "help" || command == "--help" {
		usage()
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "claude" || command == "codex" {
		c, e := config.Load(config.DefaultDir())
		if e != nil {
			return e
		}
		profileName := os.Getenv("POCKET_PROFILE")
		if profileName == "" {
			profileName = "default"
		}
		for _, p := range c.Profiles {
			if p.Provider == command && p.Name == profileName {
				if command == "claude" {
					return claudenative.RunCLI(ctx, p, os.Args[2:], os.Stdin, os.Stdout, os.Stderr)
				}
				args := codexCLIArgs(p, os.Args[2:])
				cmd := exec.CommandContext(ctx, p.Binary, args...)
				cmd.Env = append(append(os.Environ(), p.Env...), "CODEX_HOME="+p.Home)
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				return cmd.Run()
			}
		}
		return errors.New("provider profile not configured")
	}
	args := os.Args[2:]
	operation := ""
	if command == "service" || command == "claude-hooks" || command == "ui" {
		if len(args) == 0 {
			return errors.New(command + " requires an operation (install/uninstall for claude-hooks)")
		}
		operation = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	data := flags.String("data", config.DefaultDir(), "private application data directory")
	listen := flags.String("listen", "", "Hub loopback listen address")
	public := flags.String("public-url", "", "Tailscale HTTPS origin")
	hubURL := flags.String("hub", "", "Hub HTTPS origin (pair)")
	code := flags.String("code", "", "short-lived pairing code")
	role := flags.String("role", "both", "service role: hub/node/both")
	provider := flags.String("provider", "", "claude or codex")
	profile := flags.String("profile", "default", "provider profile name")
	native := flags.String("native-id", "", "native session ID")
	root := flags.String("root", "", "allowed working root (overrides configured roots)")
	uiSource := flags.String("from", "", "local compiled UI directory (ui install)")
	qrOutput := flags.String("output", "", "local QR PNG output path (login-qr)")
	_ = flags.Bool("json", false, "emit JSON")
	if e := flags.Parse(args); e != nil {
		return e
	}
	c, e := config.Load(*data)
	if e != nil {
		return e
	}
	if *listen != "" {
		c.Listen = *listen
	}
	if *public != "" {
		if e = config.CheckHub(*public); e != nil {
			return e
		}
		c.PublicURL = strings.TrimRight(*public, "/")
	}
	if *root != "" {
		abs, e := filepath.Abs(*root)
		if e != nil {
			return e
		}
		c.Roots = []string{abs}
	}
	if *listen != "" || *public != "" || *root != "" {
		if e = c.Save(); e != nil {
			return e
		}
	}
	switch command {
	case "setup", "open":
		return openSetup(ctx, c, command == "setup", *role)
	case "ui":
		if operation != "install" || *uiSource == "" {
			return errors.New("usage: pocket ui install --from /path/to/web/dist [--data DIR]")
		}
		version, err := web.InstallUI(c.DataDir, *uiSource)
		if err != nil {
			return err
		}
		fmt.Println("UI installed without replacing or restarting the service:", version)
		return nil
	case "claude-hooks":
		if operation != "install" && operation != "uninstall" {
			return errors.New("claude-hooks requires install or uninstall")
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		for _, p := range c.Profiles {
			if p.Provider == "claude" && p.Name == *profile {
				path, err := claudenative.ConfigureBridgeHooks(p, executable, c.DataDir, operation == "install")
				if err != nil {
					return err
				}
				fmt.Println("Native bridge hooks", operation+"ed:", path)
				return nil
			}
		}
		return errors.New("Claude profile unavailable")
	case "claude-hook":
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		return claudenative.StartBridgeHook(ctx, executable, c.DataDir, *profile, os.Stdin)
	case "claude-bridge":
		for _, p := range c.Profiles {
			if p.Provider == "claude" && p.Name == *profile {
				err := claudenative.RunBridgeHook(ctx, p, os.Stdin)
				claudenative.BridgeReady(err)
				return err
			}
		}
		return errors.New("Claude profile unavailable")
	case "init":
		fmt.Println("Configuration:", filepath.Join(c.DataDir, "config.json"))
		return nil
	case "pair":
		if *hubURL == "" || *code == "" {
			return errors.New("pair requires --hub and --code")
		}
		if e = node.Pair(ctx, c, *hubURL, *code); e != nil {
			return e
		}
		fmt.Println("Node paired. Run pocket node, or install its user service.")
		return nil
	case "token":
		token, e := config.Secret(c.DataDir, "owner-token")
		if e != nil {
			return e
		}
		fmt.Println(token)
		return nil
	case "login-qr":
		path, expires, e := createLoginQRFile(ctx, c, *qrOutput)
		if e != nil {
			return e
		}
		fmt.Println("Scan-login QR:", path)
		fmt.Println("Expires:", expires.Local().Format(time.RFC3339))
		fmt.Println("Open the PNG locally and scan with your phone connected to the tailnet. Single use; generating another QR revokes this one.")
		return nil
	case "service":
		if operation == "install" {
			if *role != "hub" {
				names, e := config.CaptureNodeEnvironment(c)
				if e != nil {
					return e
				}
				if len(names) > 0 {
					fmt.Println("Saved native environment variable names to private node-env.json:", strings.Join(names, ", "))
				}
			}
			path, e := service.Install(c, *role)
			if e != nil {
				return e
			}
			fmt.Println("Installed", path)
			fmt.Println("Start with: pocket service start")
			return nil
		}
		return service.Manage(operation)
	case "shell-init":
		exe, e := os.Executable()
		if e != nil {
			return e
		}
		fmt.Printf("# Pocket Agents native launcher. Remove these two functions to undo.\n# Set POCKET_HOME if using a non-default data directory.\nclaude() { %s claude \"$@\"; }\ncodex() { %s codex \"$@\"; }\n", shellquote(exe), shellquote(exe))
		return nil
	case "doctor", "discover", "history":
		ps := providers(c)
		defer closeProviders(ps)
		if command == "doctor" {
			ds := []core.Diagnostic{}
			for _, p := range ps {
				ds = append(ds, p.Doctor(ctx))
			}
			output(map[string]any{"version": "0.1.0", "nodeId": c.NodeID, "name": c.Name, "dataDir": c.DataDir, "roots": c.Roots, "tailscale": network.Inspect(ctx), "providers": ds, "hubUrl": c.HubURL})
			return nil
		}
		if command == "history" {
			p := ps[*provider+":"+*profile]
			if p == nil || *native == "" {
				return errors.New("history requires --provider and --native-id")
			}
			h, e := p.History(ctx, *native, "", 50)
			if e != nil {
				return e
			}
			output(h)
			return nil
		}
		ss := []core.Session{}
		for key, p := range ps {
			if *provider != "" && !strings.HasPrefix(key, *provider+":") {
				continue
			}
			found, e := p.Discover(ctx)
			if e != nil {
				log.Printf("%s: %v", key, e)
				continue
			}
			for i := range found {
				found[i].NodeID = c.NodeID
				found[i].ID = config.SessionID(c.NodeID, found[i].Provider, found[i].Profile, found[i].NativeID)
			}
			ss = append(ss, found...)
		}
		output(ss)
		return nil
	case "hub", "node", "both":
		return serve(ctx, c, command)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}
func shellquote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Only the interactive TUI accepts --remote. Keep native administration and
// noninteractive commands intact, including when global options precede them.
func codexCLIArgs(profile core.Profile, args []string) []string {
	seenPositional := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--remote" || strings.HasPrefix(arg, "--remote=") || arg == "--no-daemon" || arg == "--help" || arg == "-h" || arg == "--version" || arg == "-V" {
			return append([]string(nil), args...)
		}
		switch arg {
		case "-c", "--config", "--enable", "--disable", "--remote-auth-token-env", "-i", "--image", "-m", "--model", "--local-provider", "-p", "--profile", "-s", "--sandbox", "-C", "--cd", "--add-dir", "-a", "--ask-for-approval":
			i++ // This option's value may itself be a native command name.
			continue
		}
		if strings.HasPrefix(arg, "-") || seenPositional {
			continue
		}
		seenPositional = true
		switch arg {
		case "agents", "exec", "e", "review", "login", "logout", "mcp", "plugin", "app-server", "remote-control", "app", "completion", "update", "doctor", "sandbox", "debug", "apply", "a", "queue", "archive", "delete", "migrate-rollouts", "unarchive", "cloud", "exec-server", "features", "help":
			return append([]string(nil), args...)
		}
	}
	endpoint := profile.Endpoint
	if endpoint == "" {
		endpoint = "unix://" + filepath.Join(profile.Home, "app-server-control", "app-server-control.sock")
	} else if !strings.Contains(endpoint, "://") {
		endpoint = "unix://" + endpoint
	}
	return append([]string{"--remote", endpoint}, args...)
}

func providers(c *config.Config) map[string]core.Provider {
	ps := map[string]core.Provider{}
	env, envError := config.NodeEnvironment(c)
	for _, p := range c.Profiles {
		if envError != nil {
			ps[p.Provider+":"+p.Name] = &unavailable{p, envError}
			continue
		}
		p.Env = append(append([]string{}, env...), p.Env...)
		if p.DataDir == "" {
			p.DataDir = filepath.Join(c.DataDir, p.Provider+"-"+p.Name)
		}
		var adapter core.Provider
		var e error
		switch p.Provider {
		case "claude":
			adapter, e = claudenative.New(p)
		case "codex":
			adapter, e = codexnative.New(p)
		}
		if e != nil {
			adapter = &unavailable{p, e}
		}
		ps[p.Provider+":"+p.Name] = adapter
	}
	return ps
}
func closeProviders(ps map[string]core.Provider) {
	for _, p := range ps {
		_ = p.Close()
	}
}
func serve(ctx context.Context, c *config.Config, role string) error {
	lock, e := os.OpenFile(filepath.Join(c.DataDir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return errors.New("another Pocket service already owns this data directory")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var h *hub.Hub
	var listener net.Listener
	var server *http.Server
	if role != "node" {
		host, _, e := net.SplitHostPort(c.Listen)
		if e != nil {
			return e
		}
		if host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return errors.New("Hub must bind loopback; expose it privately using Tailscale Serve")
		}
		listener, e = net.Listen("tcp", c.Listen)
		if e != nil {
			return e
		}
		db, e := store.Open(filepath.Join(c.DataDir, "hub.sqlite"))
		if e != nil {
			return e
		}
		defer db.Close()
		h, e = hub.New(c, db)
		if e != nil {
			return e
		}
		defer h.Close()
		server = &http.Server{Handler: h.Handler(web.HandlerAt(c.DataDir)), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
		go h.RunPush(ctx)
		go func() {
			if e := server.Serve(listener); e != nil && e != http.ErrServerClosed {
				log.Printf("Hub: %v", e)
				cancel()
			}
		}()
		defer func() {
			stop, c := context.WithTimeout(context.Background(), 3*time.Second)
			defer c()
			_ = server.Shutdown(stop)
		}()
		log.Printf("Hub listening on http://%s; owner credential: %s (use pocket token; never share in URL)", listener.Addr(), filepath.Join(c.DataDir, "owner-token"))
	}
	if role != "hub" {
		if role == "both" {
			c.HubURL = "http://" + listener.Addr().String()
			freshLocal := c.NodeToken == ""
			if freshLocal {
				c.NodeToken = config.Random()
			}
			var hash string
			e := h.S.DB.QueryRow("SELECT token FROM devices WHERE id=?", c.NodeID).Scan(&hash)
			if e != nil {
				if !freshLocal {
					return errors.New("local node association is missing or revoked; pair explicitly using pocket pair")
				}
				if e = h.Register(core.Device{ID: c.NodeID, Name: c.Name}, c.NodeToken); e != nil {
					return e
				}
			} else if hash != config.Hash(c.NodeToken) {
				return errors.New("local node credentials revoked or mismatched; re-pair explicitly")
			}
			if e = c.Save(); e != nil {
				return e
			}
		}
		db, e := store.Open(filepath.Join(c.DataDir, "node.sqlite"))
		if e != nil {
			return e
		}
		defer db.Close()
		n := node.New(c, db, providers(c))
		return n.Run(ctx)
	}
	<-ctx.Done()
	return nil
}

type unavailable struct {
	p core.Profile
	e error
}

func (u *unavailable) Discover(context.Context) ([]core.Session, error) { return nil, u.e }
func (u *unavailable) History(context.Context, string, string, int) (core.History, error) {
	return core.History{}, u.e
}
func (u *unavailable) Execute(context.Context, core.Action) (core.Result, error) {
	return core.Result{Status: "rejected"}, u.e
}
func (u *unavailable) Doctor(context.Context) core.Diagnostic {
	return core.Diagnostic{Provider: u.p.Provider, Profile: u.p.Name, Details: u.e.Error(), Capabilities: core.Capabilities{Reason: u.e.Error()}}
}
func (u *unavailable) Close() error { return nil }
func usage() {
	fmt.Print(`Pocket Agents 0.1.0 — native Claude Code/Codex console

pocket init [--data DIR] [--root DIR]
pocket doctor [--json]
pocket both [--listen 127.0.0.1:8787] [--public-url https://name.tailnet.ts.net]
pocket hub | node
pocket token                       Print owner credential for login
pocket login-qr [--output FILE]     Generate a private, one-time phone login QR
pocket pair --hub HTTPS_ORIGIN --code ONE_TIME_CODE
pocket discover [--provider claude|codex]
pocket history --provider PROVIDER --native-id ID
pocket claude [native options]      Native background session + terminal attach
pocket codex [native options]       Native shared daemon CLI
pocket claude-hooks install         Install native same-session messaging hooks
pocket claude-hooks uninstall       Remove hooks and revoke active bridges
pocket shell-init                   Print reversible shell integration
pocket ui install --from /path/to/web/dist
pocket service install --role both|hub|node
pocket service start|restart|stop|status|uninstall

Common flags: --data DIR, --root DIR. Profiles and allowed roots: config.json.
Never pass provider credentials to Hub. Existing uncontrolled live owners remain read-only.
`)
}
