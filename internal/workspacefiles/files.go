// Package workspacefiles transfers bounded files within a session workspace.
// os.Root keeps resolution inside that workspace even across symlink races.
package workspacefiles

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

const MaxBytes = 8 << 20

type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Directory bool   `json:"directory"`
}

type Request struct {
	Cwd     string `json:"cwd"`
	Path    string `json:"path"`
	Name    string `json:"name,omitempty"`
	ID      string `json:"id,omitempty"`
	Content []byte `json:"content,omitempty"`
}

func relative(path string) bool {
	return path == "." || (filepath.IsLocal(path) && !strings.Contains(path, "\\"))
}

func List(cwd, path string) ([]Entry, error) {
	if path == "" {
		path = "."
	}
	if !relative(path) {
		return nil, errors.New("workspace-relative path required")
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(1001)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 1000 {
		return nil, errors.New("directory exceeds 1000 entries; choose a smaller directory")
	}
	result := []Entry{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			continue
		}
		result = append(result, Entry{Name: e.Name(), Path: filepath.Join(path, e.Name()), Size: info.Size(), Directory: info.IsDir()})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Directory != result[j].Directory {
			return result[i].Directory
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func Read(cwd, path string) ([]byte, error) {
	if !relative(path) {
		return nil, errors.New("workspace-relative path required")
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Stat before open avoids opening a named pipe/device. Opened descriptor is
	// checked again before reading so file replacement cannot bypass the limit.
	st, err := root.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > MaxBytes {
		return nil, errors.New("only regular files up to 8 MiB can be downloaded")
	}
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBytes {
		return nil, errors.New("file exceeds 8 MiB")
	}
	return b, nil
}

func Write(cwd, id, name string, content []byte) (Entry, error) {
	var out Entry
	if len(content) > MaxBytes {
		return out, errors.New("attachment exceeds 8 MiB")
	}
	if len(id) < 16 || len(id) > 80 || strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) >= 0 {
		return out, errors.New("invalid attachment id")
	}
	if name == "" || len(name) > 180 || filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return out, errors.New("invalid attachment filename")
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return out, err
	}
	defer root.Close()
	// A unique directory avoids overwriting user files or previous uploads.
	if err = root.Mkdir(".pocket-attachments", 0700); err != nil && !os.IsExist(err) {
		return out, err
	}
	dir := filepath.Join(".pocket-attachments", id)
	if err = root.Mkdir(dir, 0700); err != nil {
		return out, err
	}
	path := filepath.Join(dir, name)
	f, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return out, err
	}
	_, writeErr := f.Write(content)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = root.Remove(path)
		return out, err
	}
	return Entry{Name: name, Path: path, Size: int64(len(content))}, nil
}
