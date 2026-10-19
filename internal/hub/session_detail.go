package hub

import (
	"net/http"
	"pocketagents/internal/core"
	"time"
)

func (h *Hub) sessionDetail(w http.ResponseWriter, r *http.Request) {
	s, ok := h.getSession(r.PathValue("id"))
	if !ok {
		fail(w, 404, "native session no longer indexed")
		return
	}
	s.Metadata = h.S.Metadata(s.ID)
	h.mu.RLock()
	d := h.devices[s.NodeID]
	h.mu.RUnlock()
	d.Online = d.Online && time.Since(d.LastSeen) < 30*time.Second
	replyConditional(w, r, struct {
		Session core.Session `json:"session"`
		Device  core.Device  `json:"device"`
	}{s, d})
}
