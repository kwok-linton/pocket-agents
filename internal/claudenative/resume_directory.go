package claudenative

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
)

// Transcript cwd follows shell navigation. Its final value need not be the
// directory that owns the native session. Accept either the recorded current
// directory or the first main-session directory, resolving migration aliases.
// Never infer a workspace by taking an arbitrary ancestor or scanning for any
// historic match. Takeover separately checks the live owner's exact directory.
func validateResumeDirectory(path, requested, latest string) error {
	if resolved, err := validDirectory(latest); err == nil && resolved == requested {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scan.Scan() {
		var row record
		if json.Unmarshal(scan.Bytes(), &row) != nil || row.IsSidechain || row.Cwd == "" {
			continue
		}
		if resolved, err := validDirectory(row.Cwd); err == nil && resolved == requested {
			return nil
		}
		break
	}
	if err := scan.Err(); err != nil {
		return err
	}
	return errors.New("resume must use the existing native working directory")
}
