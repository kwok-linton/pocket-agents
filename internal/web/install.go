package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pocketagents/internal/config"
)

// InstallUI publishes a local build without replacing or restarting the executable.
// Only the OS user with access to DataDir can install executable web content.
func InstallUI(data, source string) (string, error) {
	root, err := os.OpenRoot(source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	files := map[string][]byte{}
	total := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("UI build must not contain symlinks")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<20 || len(files) >= 256 {
			return errors.New("invalid or oversized UI build")
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
		f.Close()
		if err != nil {
			return err
		}
		total += len(b)
		if len(b) > 16<<20 || total > 32<<20 {
			return errors.New("UI build exceeds size limit")
		}
		files[name] = b
		return nil
	})
	if err != nil {
		return "", err
	}
	var manifest struct {
		Protocol int `json:"protocol"`
	}
	if json.Unmarshal(files["pocket-ui.json"], &manifest) != nil || manifest.Protocol != 1 {
		return "", errors.New("unsupported UI protocol; rebuild with a compatible frontend")
	}
	for _, required := range []string{"index.html", "sw.js", "manifest.webmanifest"} {
		if len(files[required]) == 0 {
			return "", fmt.Errorf("UI build missing %s", required)
		}
	}
	for _, match := range regexp.MustCompile(`(?:src|href)="(/assets/[^"?]+)"`).FindAllSubmatch(files["index.html"], -1) {
		if len(files[strings.TrimPrefix(string(match[1]), "/")]) == 0 {
			return "", errors.New("UI index references a missing asset")
		}
	}
	// JSON sorts map keys, giving each complete release a stable content identity.
	content, _ := json.Marshal(files)
	sum := sha256.Sum256(content)
	version := hex.EncodeToString(sum[:])
	base := filepath.Join(data, "web-ui")
	if err = os.MkdirAll(filepath.Join(base, "releases"), 0700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(base, ".staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	for name, content := range files {
		target := filepath.Join(staging, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return "", err
		}
		if err = os.WriteFile(target, content, 0600); err != nil {
			return "", err
		}
		// Hashed assets from previous releases stay available to already-open clients.
		if strings.HasPrefix(name, "assets/") {
			cache := filepath.Join(base, filepath.FromSlash(name))
			if old, e := os.ReadFile(cache); e == nil && string(old) != string(content) {
				// Codec versions/settings may change a sidecar's bytes while
				// its hashed original stays identical. Keep the old cache copy.
				if strings.HasSuffix(name, ".br") || strings.HasSuffix(name, ".gz") {
					continue
				}
				return "", errors.New("immutable asset name reused with different content")
			}
			if err = os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
				return "", err
			}
			if err = config.Atomic(cache, content); err != nil {
				return "", err
			}
		}
	}
	target := filepath.Join(base, "releases", version)
	if _, err = os.Stat(target); os.IsNotExist(err) {
		if err = os.Rename(staging, target); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	// Atomic pointer switch: readers see either the complete old or new release.
	if err = config.Atomic(filepath.Join(base, "current"), []byte(version)); err != nil {
		return "", err
	}
	return version, nil
}
