package claudenative

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"pocketagents/internal/core"
	"strings"
	"time"
	"unicode"

	"github.com/creack/pty"
)

// renameHistory delegates persistence to Claude itself. No transcript edits,
// slash-command chat messages, or second writers for live sessions are allowed.
func (p *Provider) renameHistory(ctx context.Context, a core.Action) (core.Result, error) {
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, Status: "rejected"}
	name := strings.TrimSpace(a.Text)
	if name == "" || len([]rune(name)) > 120 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return r, errors.New("name must be a single line of 1–120 characters")
	}
	path, err := p.findPath(a.NativeID)
	if err != nil {
		return r, err
	}
	meta, err := readMetadata(path, p.profile.Name)
	if err != nil {
		return r, err
	}
	if meta.Cwd == "" || meta.Cwd != a.Cwd {
		return r, errors.New("native history working directory mismatch")
	}
	agents, err := p.agents(ctx)
	if err != nil {
		return r, err
	}
	for _, agent := range agents {
		if agent.SessionID == a.NativeID && executionID(agent) != "" {
			// Claude can leave a completed background worker resident at an idle
			// terminal that is no longer safe to type into. It has no live turn,
			// so release only that worker and use Claude's historical --name path.
			if agent.Kind == "background" && agent.State == "done" && agent.Status == "idle" {
				if !backgroundShortIDPattern.MatchString(agent.ID) {
					return r, errors.New("completed native session has an invalid background ID")
				}
				stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				cmd := p.command(stopCtx, "stop", agent.ID)
				cmd.WaitDelay = 2 * time.Second
				output, stopErr := cmd.CombinedOutput()
				cancel()
				if stopErr != nil {
					return r, fmt.Errorf("could not stop completed native session before rename: %w: %s", stopErr, strings.TrimSpace(string(output)))
				}
				break
			}
			return p.renameLive(ctx, a, agent, path, name)
		}
	}
	if meta.Title == name {
		r.Status = "accepted"
		r.Data, _ = json.Marshal(map[string]string{"name": name})
		return r, nil
	}
	renameCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := p.command(renameCtx, "--resume", a.NativeID, "--name", name)
	cmd.Dir = meta.Cwd
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 32, Cols: 100})
	if err != nil {
		return r, err
	}
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, f); close(drained) }()
	defer func() { _ = cmd.Process.Kill(); _ = f.Close(); _ = cmd.Wait(); <-drained }()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if updated, e := readMetadata(path, p.profile.Name); e == nil && updated.Title == name {
			r.Status = "accepted"
			r.Data, _ = json.Marshal(map[string]string{"name": name})
			return r, nil
		}
		select {
		case <-renameCtx.Done():
			r.Status = "unknown"
			r.Message = "原生改名尚未确认；可能正在等待 Claude 启动或信任提示，请核对原生历史后再操作"
			return r, nil
		case <-tick.C:
		}
	}
}

// Rename through the existing terminal; never start a second native writer.
func (p *Provider) renameLive(ctx context.Context, a core.Action, owner agent, path, name string) (core.Result, error) {
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, Status: "rejected"}
	id := executionID(owner)
	if owner.Kind != "background" || owner.Status != "idle" || owner.State == "working" {
		return r, errors.New("请等待 Claude 本轮结束并关闭原生提示后再改名")
	}
	if id == "" || a.ExecutionID != id {
		return r, errors.New("会话实例已变化，请刷新后再改名")
	}
	att, err := p.attach(ctx, owner, id)
	if err != nil {
		return r, err
	}
	fresh, err := p.agents(ctx)
	if err != nil {
		return r, err
	}
	valid := false
	for _, v := range fresh {
		if v.SessionID == a.NativeID && executionID(v) == id && v.Status == "idle" && v.State != "working" {
			valid = true
		}
	}
	if !valid {
		return r, errors.New("会话状态已变化，请稍后再改名")
	}
	output, _ := att.snapshot(0)
	screen, suggestion := terminalView(output)
	if !renamePromptEmpty(screen, suggestion) {
		return r, errors.New("原生终端有未发送内容或未就绪，请先在终端处理后再改名")
	}
	if _, err := att.write([]byte("/rename " + name + "\r")); err != nil {
		r.Status = "unknown"
		return r, err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if m, err := readMetadata(path, p.profile.Name); err == nil && m.Title == name {
			r.Status = "accepted"
			r.Data, _ = json.Marshal(map[string]string{"name": name})
			return r, nil
		}
		select {
		case <-tick.C:
			continue
		case <-ctx.Done():
		case <-timer.C:
		}
		r.Status = "unknown"
		r.Message = "原生改名尚未确认，请查看原生会话名称"
		return r, nil
	}
}

func renamePromptEmpty(screen, suggestion string) bool {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "❯") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(line, "❯"))
		if text != "" && (suggestion == "" || text != suggestion) {
			return false
		}
		// Reject wrapped drafts too: an input ends at the next horizontal border.
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if strings.HasPrefix(next, "─") || strings.HasPrefix(next, "━") {
				return true
			}
			if next != "" {
				return false
			}
		}
		return false
	}
	return false
}
