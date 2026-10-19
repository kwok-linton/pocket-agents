package claudenative

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"pocketagents/internal/core"
	"strings"
	"time"
)

// Delete only the exact saved conversation. Never purge a project or its files.
func (p *Provider) deleteHistory(ctx context.Context, a core.Action) (core.Result, error) {
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, Status: "rejected"}
	path, err := p.findPath(a.NativeID)
	if err != nil {
		if strings.Contains(err.Error(), "No native conversation history") {
			return p.deleteTranscriptlessBackground(ctx, a)
		}
		return r, err
	}
	real, err := filepath.EvalSymlinks(path)
	root, rootErr := filepath.EvalSymlinks(filepath.Join(p.profile.Home, "projects"))
	expected := filepath.Join(root, filepath.Base(filepath.Dir(path)), filepath.Base(path))
	if err != nil || rootErr != nil || real != expected {
		return r, errors.New("refusing a symlinked native transcript")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return r, errors.New("native transcript is not a regular file")
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
	for _, owner := range agents {
		if owner.SessionID != a.NativeID || executionID(owner) == "" {
			continue
		}
		if a.Decision != "stop_and_delete" {
			return r, errors.New("会话仍在运行。请先结束原生实例，再删除记录；项目文件不会删除。")
		}
		if owner.Kind != "background" || a.ExecutionID == "" || executionID(owner) != a.ExecutionID {
			return r, errors.New("native instance changed or is unmanaged; refresh before deleting")
		}
		stopctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		cmd := p.command(stopctx, "stop", owner.ID)
		cmd.WaitDelay = 2 * time.Second
		_, stopErr := cmd.CombinedOutput()
		cancel()
		if stopErr != nil {
			return r, fmt.Errorf("could not stop native session; history preserved: %w", stopErr)
		}
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for {
			fresh, err := p.agents(ctx)
			if err != nil {
				return r, err
			}
			alive := false
			for _, v := range fresh {
				if v.SessionID == a.NativeID && executionID(v) != "" {
					if executionID(v) != a.ExecutionID {
						return r, errors.New("native instance changed; history preserved")
					}
					alive = true
				}
			}
			if !alive {
				break
			}
			select {
			case <-ctx.Done():
				return r, ctx.Err()
			case <-deadline.C:
				return r, errors.New("native session has not exited; history preserved")
			case <-time.After(100 * time.Millisecond):
			}
		}
		// Shutdown may append the final transcript entry. Recheck its identity,
		// then establish a fresh snapshot for the final unchanged-file guard.
		freshInfo, err := os.Lstat(path)
		if err != nil || !os.SameFile(info, freshInfo) {
			return r, errors.New("native transcript replaced; history preserved")
		}
		info = freshInfo
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) || info.Size() != current.Size() || !info.ModTime().Equal(current.ModTime()) {
		return r, errors.New("native transcript changed; refresh before deleting")
	}
	if err = os.Remove(path); err != nil {
		return r, err
	}
	delete(p.cache, path)
	r.Status = "accepted"
	r.Message = "已删除原生会话主记录；项目文件及附件保留。"
	return r, nil
}

// deleteTranscriptlessBackground removes a native supervisor record that never
// reached transcript creation. It is deliberately limited to one exact
// background owner and never touches the workspace.
func (p *Provider) deleteTranscriptlessBackground(ctx context.Context, a core.Action) (core.Result, error) {
	r := core.Result{ActionID: a.ID, NativeID: a.NativeID, Status: "rejected"}
	agents, err := p.agents(ctx)
	if err != nil {
		return r, err
	}
	var matches []agent
	for _, owner := range agents {
		if owner.SessionID == a.NativeID {
			matches = append(matches, owner)
		}
	}
	if len(matches) == 0 {
		return r, errors.New("No native conversation history or background record was found.")
	}
	if len(matches) != 1 {
		return r, errors.New("Multiple native background records match this session; refusing deletion.")
	}
	owner := matches[0]
	if owner.Kind != "background" {
		return r, errors.New("Transcriptless native session is not a managed background instance.")
	}
	if owner.Cwd == "" || owner.Cwd != a.Cwd {
		return r, errors.New("native background working directory mismatch")
	}
	if a.Decision != "stop_and_delete" {
		return r, errors.New("This native background session has no transcript. Confirm ending and removing its supervisor record.")
	}
	id := executionID(owner)
	if id != "" {
		if a.ExecutionID == "" || a.ExecutionID != id {
			return r, errors.New("native instance changed; refresh before deleting")
		}
		stopctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		cmd := p.command(stopctx, "stop", owner.ID)
		cmd.WaitDelay = 2 * time.Second
		b, stopErr := cmd.CombinedOutput()
		cancel()
		if stopErr != nil {
			return r, fmt.Errorf("could not stop transcriptless native session; record preserved: %w: %s", stopErr, b)
		}
	}
	rmctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := p.command(rmctx, "rm", owner.ID)
	cmd.WaitDelay = 2 * time.Second
	b, err := cmd.CombinedOutput()
	if err != nil {
		return r, fmt.Errorf("could not remove transcriptless native session: %w: %s", err, b)
	}
	r.Status = "accepted"
	r.Message = "Removed the transcriptless native background session; project files and attachments were kept."
	return r, nil
}
