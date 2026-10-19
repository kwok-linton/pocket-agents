package core

// NotificationContext travels with an event, independently of the session
// snapshot. It contains identity and scope only, never transcript or approvals.
type NotificationContext struct {
	Title    string `json:"title"`
	Provider string `json:"provider"`
	Profile  string `json:"profile"`
	NativeID string `json:"nativeId"`
	Cwd      string `json:"cwd"`
	ParentID string `json:"parentId,omitempty"`
}

func NotificationFor(s Session) NotificationContext {
	return NotificationContext{Title: s.Title, Provider: s.Provider, Profile: s.Profile, NativeID: s.NativeID, Cwd: s.Cwd, ParentID: s.ParentID}
}
