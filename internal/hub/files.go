package hub

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"pocketagents/internal/core"
	"pocketagents/internal/workspacefiles"
)

func (h *Hub) sessionFileRPC(w http.ResponseWriter, r *http.Request, kind string, v workspacefiles.Request) (core.Wire, bool) {
	s, ok := h.getSession(r.PathValue("id"))
	if !ok {
		fail(w, 404, "session not found")
		return core.Wire{}, false
	}
	v.Cwd = s.Cwd
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := h.RPC(ctx, s.NodeID, kind, v)
	if err != nil {
		fail(w, 502, err.Error())
		return result, false
	}
	return result, true
}
func (h *Hub) filesList(w http.ResponseWriter, r *http.Request) {
	result, ok := h.sessionFileRPC(w, r, "files_list", workspacefiles.Request{Path: r.URL.Query().Get("path")})
	if ok {
		reply(w, result.Data)
	}
}
func (h *Hub) fileWrite(w http.ResponseWriter, r *http.Request) {
	// Raw bodies avoid base64 overhead between phone and Hub. The internal
	// authenticated Node transport has a separately bounded JSON frame.
	r.Body = http.MaxBytesReader(w, r.Body, workspacefiles.MaxBytes)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 413, "attachment exceeds 8 MiB or upload interrupted")
		return
	}
	result, ok := h.sessionFileRPC(w, r, "files_write", workspacefiles.Request{ID: r.URL.Query().Get("uploadId"), Name: r.URL.Query().Get("name"), Content: b})
	if ok {
		reply(w, result.Data)
	}
}
func (h *Hub) fileRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	result, ok := h.sessionFileRPC(w, r, "files_read", workspacefiles.Request{Path: path})
	if !ok {
		return
	}
	var out struct {
		Content []byte `json:"content"`
	}
	if json.Unmarshal(result.Data, &out) != nil || len(out.Content) > workspacefiles.MaxBytes {
		fail(w, 502, "invalid file response")
		return
	}
	// Never render arbitrary workspace HTML/SVG as same-origin executable content.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(path)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out.Content)
}
