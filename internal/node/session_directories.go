package node

import (
	"os"
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
				if resolved, err := directoryAlias(cwd); err == nil {
					canonical = resolved
				}
			}
			paths[cwd] = canonical
		}
		out[i].Cwd = canonical
	}
	return out
}

// A removed worktree may still have history. Resolve its existing ancestor so
// its records remain under the migrated project, without claiming it exists.
func directoryAlias(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil || !os.IsNotExist(err) {
		return resolved, err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, parentErr := directoryAlias(parent)
	if parentErr != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}
