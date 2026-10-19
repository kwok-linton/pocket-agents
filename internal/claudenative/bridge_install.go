package claudenative

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"pocketagents/internal/config"
	"pocketagents/internal/core"
)

// ConfigureBridgeHooks preserves all unrelated settings and hooks. The only
// hooks installed run our local relay; no permission/inbound policy is changed.
func ConfigureBridgeHooks(profile core.Profile, executable, dataDir string, enable bool) (string, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(dataDir) || profile.DataDir == "" {
		return "", errors.New("absolute executable/data paths required")
	}
	path := filepath.Join(profile.Home, "settings.json")
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if st, e := os.Lstat(path); e == nil && st.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("settings.json is a symlink; install hook manually in its managed source")
	}
	settings := map[string]json.RawMessage{}
	if len(before) > 0 {
		if err = json.Unmarshal(before, &settings); err != nil {
			return "", err
		}
	}
	if settings == nil {
		return "", errors.New("settings must be a JSON object")
	}
	hooks := map[string][]map[string]json.RawMessage{}
	if raw, ok := settings["hooks"]; ok {
		if err = json.Unmarshal(raw, &hooks); err != nil {
			return "", err
		}
	}
	if hooks == nil {
		hooks = map[string][]map[string]json.RawMessage{}
	}
	args := []string{"claude-hook", "--data", dataDir, "--profile", profile.Name}
	for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
		kept := []map[string]json.RawMessage{}
		for _, group := range hooks[event] {
			var commands []map[string]json.RawMessage
			if err = json.Unmarshal(group["hooks"], &commands); err != nil {
				return "", err
			}
			other := []map[string]json.RawMessage{}
			for _, command := range commands {
				var program string
				var values []string
				_ = json.Unmarshal(command["command"], &program)
				_ = json.Unmarshal(command["args"], &values)
				ours := program == executable && len(values) == len(args)
				if ours {
					for i := range args {
						if values[i] != args[i] {
							ours = false
							break
						}
					}
				}
				if !ours {
					other = append(other, command)
				}
			}
			if len(other) > 0 {
				group["hooks"], _ = json.Marshal(other)
				kept = append(kept, group)
			}
		}
		if enable {
			command, _ := json.Marshal([]any{map[string]any{"type": "command", "command": executable, "args": args, "async": true}})
			kept = append(kept, map[string]json.RawMessage{"hooks": command})
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	settings["hooks"], _ = json.Marshal(hooks)
	after, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	after = append(after, '\n')
	if err = os.MkdirAll(profile.DataDir, 0700); err != nil {
		return "", err
	}
	// Disable first when uninstalling so existing children revoke their sockets.
	marker := filepath.Join(profile.DataDir, "bridge-enabled.json")
	if !enable {
		if err = config.Atomic(marker, []byte(`{"enabled":false}`)); err != nil {
			return "", err
		}
	}
	if !bytes.Equal(before, after) {
		if len(before) > 0 {
			backup := filepath.Join(profile.DataDir, "settings-before-hooks-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
			if err = config.Atomic(backup, before); err != nil {
				return "", err
			}
		}
		if err = os.MkdirAll(profile.Home, 0700); err != nil {
			return "", err
		}
		if err = config.Atomic(path, after); err != nil {
			return "", err
		}
	}
	if enable {
		if err = config.Atomic(marker, []byte(`{"enabled":true}`)); err != nil {
			return "", err
		}
	}
	return path, nil
}
func (p *Provider) bridgeDisabled() bool {
	b, err := os.ReadFile(filepath.Join(p.profile.DataDir, "bridge-enabled.json"))
	if os.IsNotExist(err) {
		return false
	}
	var v struct {
		Enabled bool `json:"enabled"`
	}
	return err != nil || json.Unmarshal(b, &v) != nil || !v.Enabled
}
