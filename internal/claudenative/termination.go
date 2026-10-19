package claudenative

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pocketagents/internal/core"
)

const stopUnconfirmed = "Native stop is unconfirmed; inspect the session before retrying."
const stopConfirmed = "The original native execution has exited; conversation history is retained."

// Absence from the agents list alone is insufficient: the worker may still be
// alive. Verify the exact process generation, never stop a replacement worker.
func executionExited(nativeID, execution string) bool {
	parts := strings.Split(execution, ":")
	if len(parts) != 4 || parts[0] != "claude" || parts[1] != nativeID {
		return false
	}
	pid, err := strconv.Atoi(parts[2])
	if err != nil || pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	now := executionID(agent{SessionID: nativeID, PID: pid})
	return now != "" && now != execution
}

func (p *Provider) stopExecution(ctx context.Context, a core.Action, current agent) (core.Result, error) {
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, ExecutionID: a.ExecutionID}
	if a.ExecutionID == "" || executionID(current) != a.ExecutionID {
		return r, errors.New("execution changed or exited; refresh before sending")
	}
	stopctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := p.command(stopctx, "stop", current.ID)
	cmd.WaitDelay = 2 * time.Second
	// A nonzero acknowledgement can still follow a successful stop. Never
	// classify it as rejected (safe to retry), or resend during reconciliation.
	if err := cmd.Start(); err != nil {
		return r, err
	}
	if err := cmd.Wait(); err != nil {
		r.Status, r.Message = "unknown", stopUnconfirmed
		return r, nil
	}
	// The existing discovery/result loop observes completion. Do not hold the
	// controller lock polling for process exit after native stop acknowledges.
	r.Status, r.Message = "submitted", "Native stop requested; state will be reconciled."
	return r, nil
}

func (a *attachment) disconnect() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dead = true
	if a.input != nil {
		_ = a.input.Close()
	}
	if a.file != nil {
		_ = a.file.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
}
