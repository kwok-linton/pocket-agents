package claudenative

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"pocketagents/internal/core"
)

const interactiveInputReason = "This terminal session receives Pocket messages as peer messages, not your approval. Enable phone control to resume the same conversation with native user input, or run /bg in its original terminal."

func takeoverIdle(a agent, presence inboxRegistry) bool {
	return a.Kind == "interactive" && a.Status == "idle" && a.WaitingFor == "" &&
		(a.State == "" || a.State == "done" || a.State == "failed") &&
		presence.Status == "idle" && presence.WaitingFor == ""
}

// takeover is an explicit lifecycle operation, never a side effect of send.
// Resume the saved native ID only after the original writer has exited. It
// does not replay peer messages or send a draft/approval to the resumed agent.
func (p *Provider) takeover(ctx context.Context, a core.Action, current agent) (core.Result, error) {
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, ExecutionID: a.ExecutionID, Status: "rejected"}
	if a.Decision != "restart" || a.Text != "" || len(a.Files) != 0 || len(a.InputFiles) != 0 || a.Model != "" || a.Effort != "" {
		return r, errors.New("Phone control requires an explicit restart confirmation without a message.")
	}
	if current.PID <= 1 || current.PID == os.Getpid() || executionID(current) != a.ExecutionID {
		return r, errors.New("Native execution changed; refresh before enabling phone control.")
	}
	presence, err := p.inbox(current)
	if err != nil {
		return r, err
	}
	if !takeoverIdle(current, presence) {
		return r, errors.New("Wait for this session to finish its turn before enabling phone control.")
	}
	// Validate everything needed to resume before stopping the original writer.
	cwd, err := validDirectory(a.Cwd)
	if err != nil {
		return r, err
	}
	path, err := p.findPath(a.NativeID)
	if err != nil {
		return r, err
	}
	meta, err := readMetadata(path, p.profile.Name)
	if err != nil {
		return r, err
	}
	nativeCwd, err := validDirectory(meta.Cwd)
	if err != nil || cwd != nativeCwd {
		return r, errors.New("resume must use the existing native working directory")
	}
	ownerCwd, err := validDirectory(current.Cwd)
	if err != nil || cwd != ownerCwd {
		return r, errors.New("Native workspace changed; refresh before enabling phone control.")
	}
	// The registry is updated by the running process. Recheck immediately
	// before signalling; never kill a newly busy/replaced execution.
	presence, err = p.inbox(current)
	if err != nil || !takeoverIdle(current, presence) || executionID(current) != a.ExecutionID {
		return r, errors.New("Native execution changed; refresh before enabling phone control.")
	}
	if err = syscall.Kill(current.PID, syscall.SIGTERM); err != nil {
		return r, err
	}
	r.Status = "unknown"
	r.Message = "Phone-control restart was requested. Check session state before retrying; no message was sent."
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if executionExited(a.NativeID, a.ExecutionID) {
			break
		}
		select {
		case <-ctx.Done():
			return r, nil
		case <-deadline.C:
			return r, nil // no SIGKILL and no second writer
		case <-tick.C:
		}
	}
	// start checks the live-owner list again and preserves the native session
	// ID. Resume uses native saved settings, never the create-only bypass mode.
	next, err := p.start(ctx, core.Action{ID: a.ID, Kind: "resume", NativeID: a.NativeID, Cwd: cwd})
	if err != nil {
		r.Message = "Phone-control startup was not confirmed. Conversation history is preserved; check session state before using Resume. " + err.Error()
		return r, nil
	}
	return next, nil
}
