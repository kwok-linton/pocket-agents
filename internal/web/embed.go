package web

import (
	"bytes"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"pocketagents/internal/httpencoding"
	"strings"
)

//go:embed dist
var assets embed.FS

func HandlerAt(data string) http.Handler {
	embedded, _ := fs.Sub(assets, "dist")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		root := embedded
		if data != "" {
			base := filepath.Join(data, "web-ui")
			if current, err := os.ReadFile(filepath.Join(base, "current")); err == nil && len(current) == 64 {
				if _, err = hex.DecodeString(string(current)); err == nil {
					candidate := os.DirFS(filepath.Join(base, "releases", string(current)))
					if _, err = fs.Stat(candidate, "index.html"); err == nil {
						root = uiFS{current: candidate, cache: os.DirFS(base), embedded: embedded}
					}
				}
			}
		}
		files := http.FileServer(http.FS(root))
		clean := path.Clean(r.URL.Path)
		name := strings.TrimPrefix(clean, "/")
		if name == "" {
			name = "index.html"
		}
		if _, e := fs.Stat(root, name); e != nil {
			name = "index.html"
		}
		if name == "sw.js" || name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.Header().Add("Vary", "Accept-Encoding")
			var available []string
			for _, codec := range []string{"br", "gzip"} {
				suffix := "." + codec
				if codec == "gzip" {
					suffix = ".gz"
				}
				if _, err := fs.Stat(root, name+suffix); err == nil {
					available = append(available, codec)
				}
			}
			if codec := httpencoding.Best(r.Header.Get("Accept-Encoding"), available...); codec != "" {
				suffix := "." + codec
				if codec == "gzip" {
					suffix = ".gz"
				}
				if content, err := fs.ReadFile(root, name+suffix); err == nil {
					w.Header().Set("Content-Encoding", codec)
					w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
					info, _ := fs.Stat(root, name)
					http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(content))
					return
				}
			}
		}
		// FileServer redirects /index.html to ./ even when it is being fetched
		// for precaching. Replaying that redirected response for a navigation
		// fails in Safari. Serve every shell URL directly, for all encodings.
		if name == "index.html" {
			r2 := r.Clone(r.Context())
			u := *r.URL
			r2.URL = &u
			r2.URL.Path = "/"
			files.ServeHTTP(w, r2)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// Only immutable build assets fall back across versions, never index or SW.
type uiFS struct{ current, cache, embedded fs.FS }

func (u uiFS) Open(name string) (fs.File, error) {
	f, err := u.current.Open(name)
	if err == nil || !strings.HasPrefix(name, "assets/") {
		return f, err
	}
	if f, err = u.cache.Open(name); err == nil {
		return f, nil
	}
	return u.embedded.Open(name)
}
