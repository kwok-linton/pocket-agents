package node

import (
	"path/filepath"
	"pocketagents/internal/core"
)

// Resolve real aliases for indexing without rewriting provider-owned records.
// Cache per scan so hundreds of sessions in one directory need one lookup.
func canonicalSessionDirectories(sessions []core.Session) []core.Session {
	out := append([]core.Session(nil), sessions...)
	paths := make(map[string]string)
	for i := range out {
		cwd := out[i].Cwd
		canonical, ok := paths[cwd]
		if !ok {
			canonical = cwd
			if filepath.IsAbs(cwd) {
				if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
					canonical = resolved
				}
			}
			paths[cwd] = canonical
		}
		out[i].Cwd = canonical
	}
	return out
}
