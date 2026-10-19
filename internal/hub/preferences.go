package hub

import (
	"database/sql"
	"errors"
	"net/http"
	"path"
	"strings"
)

const preferencesKey = "preferences"

type workspacePreference struct {
	NodeID string `json:"nodeId"`
	Path   string `json:"path"`
}

type preferences struct {
	WorkspaceScope string                `json:"workspaceScope"`
	Workspaces     []workspacePreference `json:"workspaces"`
}

func (h *Hub) loadPreferences() (preferences, error) {
	p := preferences{WorkspaceScope: "all", Workspaces: []workspacePreference{}}
	if err := h.S.Get(preferencesKey, &p); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	if p.WorkspaceScope != "all" && p.WorkspaceScope != "focused" {
		return p, errors.New("invalid stored workspace scope")
	}
	if p.Workspaces == nil {
		p.Workspaces = []workspacePreference{}
	}
	return p, nil
}

func (h *Hub) getPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := h.loadPreferences()
	if err != nil {
		fail(w, http.StatusInternalServerError, "unable to read workspace preferences")
		return
	}
	reply(w, p)
}

func (h *Hub) putPreferences(w http.ResponseWriter, r *http.Request) {
	var p preferences
	if !decode(w, r, &p) {
		return
	}
	if p.WorkspaceScope != "all" && p.WorkspaceScope != "focused" {
		fail(w, http.StatusBadRequest, "workspaceScope must be all or focused")
		return
	}
	if len(p.Workspaces) > 100 {
		fail(w, http.StatusBadRequest, "at most 100 workspaces may be selected")
		return
	}
	cleaned := make([]workspacePreference, 0, len(p.Workspaces))
	seen := map[workspacePreference]bool{}
	h.mu.RLock()
	for _, workspace := range p.Workspaces {
		if _, ok := h.devices[workspace.NodeID]; !ok {
			h.mu.RUnlock()
			fail(w, http.StatusBadRequest, "workspace nodeId must identify a registered device")
			return
		}
		if len(workspace.Path) > 4096 || !path.IsAbs(workspace.Path) || strings.ContainsRune(workspace.Path, '\x00') {
			h.mu.RUnlock()
			fail(w, http.StatusBadRequest, "workspace path must be an absolute Unix directory path of at most 4096 bytes without NUL")
			return
		}
		// Scope is a display preference, not filesystem authorization. A Hub
		// must not resolve or stat paths that belong to another execution node.
		workspace.Path = path.Clean(workspace.Path)
		if !seen[workspace] {
			seen[workspace] = true
			cleaned = append(cleaned, workspace)
		}
	}
	p.Workspaces = cleaned
	err := h.S.Put(preferencesKey, p)
	h.mu.RUnlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "unable to save workspace preferences")
		return
	}
	reply(w, p)
}
