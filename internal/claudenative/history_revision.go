package claudenative

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Stat native files instead of parsing their JSONL on every unchanged poll.
// Include change-time/inode (through Sys) but exclude access-time, which reads
// themselves can update. Phone delivery overlays are small local JSON files.
func (p *Provider) HistoryRevision(ctx context.Context, id string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := p.findPath(id)
	if err != nil {
		return "", err
	}
	paths := []string{path}
	if p.profile.DataDir != "" {
		files, err := filepath.Glob(filepath.Join(p.deliveryDir(), "*.json"))
		if err != nil {
			return "", err
		}
		paths = append(paths, files...)
	}
	hash := sha256.New()
	for _, name := range paths {
		st, err := os.Stat(name)
		if err != nil {
			return "", err
		}
		// Platform stat field names differ between Darwin and Linux.
		b, _ := json.Marshal(st.Sys())
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(b, &fields)
		delete(fields, "Atimespec")
		delete(fields, "Atim")
		delete(fields, "Atime")
		delete(fields, "Atimensec")
		b, _ = json.Marshal([]any{name, st.Size(), st.ModTime().UnixNano(), fields})
		hash.Write(b)
	}
	return hex.EncodeToString(hash.Sum(nil)[:12]), nil
}
