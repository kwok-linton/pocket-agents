package claudenative

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"pocketagents/internal/core"
)

// Native /exit is a detach in background mode. Only a newly persisted native
// command record, followed by a successful client exit, requests stop here.
// EOF, Ctrl+Z, left-arrow, cancellation and attach errors alone never stop work.
func (p *Provider) nativeExitSince(nativeID string, since time.Time) bool {
	path, err := p.findPath(nativeID)
	if err != nil {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 16*1024*1024)
	found := false
	for scan.Scan() {
		if !strings.Contains(scan.Text(), "/exit") {
			continue
		}
		var r struct {
			Type        string    `json:"type"`
			SessionID   string    `json:"sessionId"`
			IsSidechain bool      `json:"isSidechain"`
			Timestamp   time.Time `json:"timestamp"`
			Message     struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scan.Bytes(), &r) != nil || r.Type != "user" || r.Message.Role != "user" || r.SessionID != nativeID || r.IsSidechain || r.Timestamp.Before(since) {
			continue
		}
		var content string
		if json.Unmarshal(r.Message.Content, &content) != nil {
			continue
		}
		fields := strings.Fields(content)
		found = strings.Join(fields, " ") == "<command-name>/exit</command-name> <command-message>exit</command-message> <command-args></command-args>"
		if found {
			break
		}
	}
	return found && scan.Err() == nil
}

func (p *Provider) finishNativeExit(ctx context.Context, current agent, generation string, since time.Time, clientErr error) (core.Result, bool) {
	if since.IsZero() || clientErr != nil || ctx.Err() != nil || !p.nativeExitSince(current.SessionID, since) {
		return core.Result{}, false
	}
	a := core.Action{Kind: "terminate", NativeID: current.SessionID, ExecutionID: generation}
	if executionExited(a.NativeID, generation) {
		return core.Result{Status: "accepted", Message: stopConfirmed}, true
	}
	r, err := p.stopExecution(ctx, a, current)
	if err != nil {
		return core.Result{Status: "unknown", Message: stopUnconfirmed}, true
	}
	return r, true
}

func (p *Provider) runTargetCLI(ctx context.Context, short string, stdin, stdout *os.File) error {
	aa, err := p.agents(ctx)
	if err != nil {
		return err
	}
	var current *agent
	for _, a := range aa {
		if a.ID == short || a.SessionID == short {
			v := a
			current = &v
			break
		}
	}
	if current == nil || current.Kind != "background" {
		return fmt.Errorf("native background execution unavailable; inspect native sessions before retrying")
	}
	generation := executionID(*current)
	if generation == "" {
		return fmt.Errorf("native execution exited before attach")
	}
	var intent exitInput
	c := p.command(ctx, "attach", current.ID)
	_, err = runCLIProxy(c, stdin, stdout, true, &intent)
	if r, exit := p.finishNativeExit(ctx, *current, generation, intent.submittedAt(), err); exit {
		if r.Status != "accepted" && r.Status != "submitted" {
			return fmt.Errorf("%s", r.Message)
		}
		fmt.Fprintln(stdout, r.Message)
	} else if err == nil {
		if executionExited(current.SessionID, generation) {
			fmt.Fprintln(stdout, stopConfirmed)
		} else {
			fmt.Fprintln(stdout, "Detached from the native terminal; the background execution continues.")
		}
	}
	return err
}
