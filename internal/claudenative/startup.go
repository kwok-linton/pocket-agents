package claudenative

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"pocketagents/internal/core"
	"sync"
	"time"

	"github.com/creack/pty"
)

// A startup is deliberately NOT a Session: native --bg assigns its own ID only
// after the real trust dialog succeeds. No fabricated native ID enters indexes.
type startup struct {
	ID, Generation string
	Action         core.Action
	Cmd            *exec.Cmd
	File           *os.File
	mu             sync.Mutex
	Output         []byte
	Done           bool
	Err            error
	Started        time.Time
	Expired        bool
}

func (p *Provider) beginStartup(a core.Action, args []string) (core.Result, error) {
	if p.startups == nil {
		p.startups = map[string]*startup{}
	}
	count := 0
	for id, s := range p.startups {
		s.mu.Lock()
		if !s.Done {
			count++
		} else if time.Since(s.Started) > 10*time.Minute || len(p.startups) >= 64 {
			delete(p.startups, id)
		}
		s.mu.Unlock()
	}
	if count >= 8 {
		return core.Result{Status: "rejected"}, errors.New("too many pending native startup dialogs")
	}
	var seed [16]byte
	if _, e := rand.Read(seed[:]); e != nil {
		return core.Result{}, e
	}
	id := hex.EncodeToString(seed[:])
	cmd := p.command(context.Background(), args...)
	cmd.Dir = a.Cwd
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	f, e := pty.StartWithSize(cmd, &pty.Winsize{Rows: 32, Cols: 100})
	if e != nil {
		return core.Result{}, e
	}
	s := &startup{ID: id, Generation: "startup:" + id, Action: a, Cmd: cmd, File: f, Started: time.Now()}
	p.startups[id] = s
	finished := make(chan struct{})
	go func() {
		timer := time.NewTimer(10 * time.Minute)
		defer timer.Stop()
		select {
		case <-finished:
		case <-timer.C:
			s.mu.Lock()
			if !s.Done {
				s.Expired = true
				_ = s.Cmd.Process.Kill()
			}
			s.mu.Unlock()
		}
	}()
	go func() {
		defer close(finished)
		buf := make([]byte, 8192)
		for {
			n, e := f.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.Output = append(s.Output, buf[:n]...)
				if len(s.Output) > 256*1024 {
					s.Output = append([]byte(nil), s.Output[len(s.Output)-256*1024:]...)
				}
				s.mu.Unlock()
			}
			if e != nil {
				break
			}
		}
		e := cmd.Wait()
		s.mu.Lock()
		s.Done = true
		s.Err = e
		s.mu.Unlock()
		_ = f.Close()
	}()
	return p.startupResult(context.Background(), s)
}
func (p *Provider) startupResult(ctx context.Context, s *startup) (core.Result, error) {
	s.mu.Lock()
	out := append([]byte(nil), s.Output...)
	done, err, expired := s.Done, s.Err, s.Expired
	s.mu.Unlock()
	r := core.Result{ActionID: s.Action.ID, Status: "submitted", Message: "Native workspace startup needs attention. Review the actual Claude dialog below; no session exists until Claude assigns its ID."}
	if expired {
		r.Status = "unknown"
		r.Message = "Native startup dialog expired. Inspect native sessions before explicitly retrying."
		return r, nil
	}
	if done {
		// The interactive launcher emits SGR styling between the words and ID.
		// Parse its rendered text, exactly as shown to the user.
		match := backgroundIDPattern.FindStringSubmatch(terminalScreen(out))
		if len(match) == 2 {
			agents, e := p.agents(ctx)
			if e != nil {
				r.Status = "unknown"
				r.Message = e.Error()
				return r, nil
			}
			for _, v := range agents {
				if v.ID == match[1] {
					r.NativeID = v.SessionID
					r.ExecutionID = executionID(v)
					r.Status = "accepted"
					r.Message = "Native workspace dialog completed and native session created."
					return r, nil
				}
			}
			r.Status = "unknown"
			r.Message = "Native background ID returned but not reconciled; inspect native sessions before retrying."
			return r, nil
		}
		if err != nil {
			r.Status = "rejected"
			r.Message = "Native startup ended without creating a session: " + terminalScreen(out) + " (" + err.Error() + ")"
		} else {
			r.Status = "unknown"
			r.Message = "Native launcher exited without a recognized session acknowledgement. Inspect native sessions before retrying: " + terminalScreen(out)
		}
		return r, nil
	}
	r.Data, _ = json.Marshal(map[string]string{"startupId": s.ID, "executionId": s.Generation, "screen": terminalScreen(out)})
	return r, nil
}
func (p *Provider) startupAction(ctx context.Context, a core.Action) (core.Result, error) {
	s := p.startups[a.RequestID]
	if s == nil {
		return core.Result{Status: "unknown", Message: "Startup controller no longer exists (Node may have restarted); inspect native sessions before retrying."}, nil
	}
	if a.Kind == "startup_input" {
		if a.ExecutionID != s.Generation {
			return core.Result{Status: "rejected"}, errors.New("startup generation mismatch")
		}
		s.mu.Lock()
		done := s.Done
		s.mu.Unlock()
		if done {
			return core.Result{Status: "rejected"}, errors.New("native startup dialog already ended")
		}
		if len(a.Text) > 4096 {
			return core.Result{Status: "rejected"}, errors.New("startup input too large")
		}
		if _, e := s.File.Write([]byte(a.Text)); e != nil {
			return core.Result{Status: "unknown"}, e
		}
	}
	return p.startupResult(ctx, s)
}
