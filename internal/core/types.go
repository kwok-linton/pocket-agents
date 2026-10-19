package core

import (
	"context"
	"encoding/json"
	"time"
)

type Profile struct {
	CodexWithoutApproval       bool     `json:"codexWithoutApproval,omitempty"`
	CodexCreateWithoutApproval bool     `json:"codexCreateWithoutApproval,omitempty"`
	ClaudeCreatePermissionMode string   `json:"claudeCreatePermissionMode,omitempty"`
	Name                       string   `json:"name"`
	Provider                   string   `json:"provider"`
	Home                       string   `json:"home"`
	Binary                     string   `json:"binary,omitempty"`
	Endpoint                   string   `json:"endpoint,omitempty"`
	DataDir                    string   `json:"dataDir,omitempty"`
	Env                        []string `json:"env,omitempty"`
}
type Capabilities struct {
	InputMode  string `json:"inputMode,omitempty"`
	Terminate  bool   `json:"terminate"`
	Discover   bool   `json:"discover"`
	Create     bool   `json:"create"`
	Resume     bool   `json:"resume"`
	Attach     bool   `json:"attach"`
	Send       bool   `json:"send"`
	Approve    bool   `json:"approve"`
	Interrupt  bool   `json:"interrupt"`
	Terminal   bool   `json:"terminal"`
	Persistent bool   `json:"persistent"`
	Reason     string `json:"reason,omitempty"`
}
type Session struct {
	ID             string       `json:"id"`
	NodeID         string       `json:"nodeId"`
	DeviceName     string       `json:"deviceName,omitempty"`
	Provider       string       `json:"provider"`
	Profile        string       `json:"profile"`
	NativeID       string       `json:"nativeId"`
	Cwd            string       `json:"cwd"`
	Title          string       `json:"title"`
	UpdatedAt      time.Time    `json:"updatedAt"`
	ExecutionID    string       `json:"executionId,omitempty"`
	ExecutionState string       `json:"executionState"`
	TurnState      string       `json:"turnState"`
	TurnID         string       `json:"turnId,omitempty"`
	Capabilities   Capabilities `json:"capabilities"`
	Pending        []Approval   `json:"pending,omitempty"`
	Error          string       `json:"error,omitempty"`
	ParentID       string       `json:"parentId,omitempty"`
	Metadata       Metadata     `json:"metadata"`
}
type Metadata struct {
	Alias    string    `json:"alias,omitempty"`
	Favorite bool      `json:"favorite"`
	Done     bool      `json:"done"`
	Hidden   bool      `json:"hidden"`
	ReadAt   time.Time `json:"readAt"`
}
type Message struct {
	ID   string    `json:"id"`
	Role string    `json:"role"`
	Text string    `json:"text"`
	Kind string    `json:"kind,omitempty"`
	At   time.Time `json:"at"`
}
type History struct {
	Messages []Message `json:"messages"`
	Cursor   string    `json:"cursor,omitempty"`
	HasMore  bool      `json:"hasMore"`
	Boundary string    `json:"boundary,omitempty"`
}
type Approval struct {
	ID          string          `json:"id"`
	NativeID    string          `json:"nativeId"`
	ExecutionID string          `json:"executionId"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Detail      string          `json:"detail"`
	Options     []string        `json:"options,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}
type FileInput struct {
	Path string `json:"path"`
	MIME string `json:"mime"`
}
type Action struct {
	Model       string          `json:"model,omitempty"`
	Effort      string          `json:"effort,omitempty"`
	Files       []string        `json:"files,omitempty"`
	InputFiles  []FileInput     `json:"-"`
	ID          string          `json:"id"`
	NodeID      string          `json:"nodeId"`
	Provider    string          `json:"provider"`
	Profile     string          `json:"profile"`
	NativeID    string          `json:"nativeId,omitempty"`
	ExecutionID string          `json:"executionId,omitempty"`
	Kind        string          `json:"kind"`
	Cwd         string          `json:"cwd,omitempty"`
	Text        string          `json:"text,omitempty"`
	TerminalID  string          `json:"terminalId,omitempty"`
	RequestID   string          `json:"requestId,omitempty"`
	Decision    string          `json:"decision,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}
type Result struct {
	ActionID    string          `json:"actionId"`
	Status      string          `json:"status"`
	NativeID    string          `json:"nativeId,omitempty"`
	ExecutionID string          `json:"executionId,omitempty"`
	Message     string          `json:"message,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}
type Diagnostic struct {
	CreatePermissionMode string       `json:"createPermissionMode,omitempty"`
	Provider             string       `json:"provider"`
	Profile              string       `json:"profile"`
	Version              string       `json:"version"`
	Available            bool         `json:"available"`
	LoggedIn             string       `json:"loggedIn"`
	Capabilities         Capabilities `json:"capabilities"`
	Details              string       `json:"details"`
}
type Provider interface {
	Discover(context.Context) ([]Session, error)
	History(context.Context, string, string, int) (History, error)
	Execute(context.Context, Action) (Result, error)
	Doctor(context.Context) Diagnostic
	Close() error
}
type Device struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	OS          string       `json:"os"`
	Online      bool         `json:"online"`
	LastSeen    time.Time    `json:"lastSeen"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Roots       []string     `json:"roots"`
	TailnetID   string       `json:"tailnetId,omitempty"`
}
type Snapshot struct {
	Device   Device    `json:"device"`
	Sessions []Session `json:"sessions"`
}
type Event struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	SessionID string          `json:"sessionId,omitempty"`
	NodeID    string          `json:"nodeId,omitempty"`
	At        time.Time       `json:"at"`
	Data      json.RawMessage `json:"data,omitempty"`
}
type Wire struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}
