package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"pocketagents/internal/core"
	"time"
)

// RecordTurn commits the observed turn and its notification together. A crash
// before last-sessions is saved cannot duplicate an event; a failed write must
// not move the caller's in-memory observation forward.
func (s *Store) RecordTurn(next, previous core.Session, existed, notify bool) error {
	// Loss of provider visibility is not a new native turn. Keep the last
	// authoritative state so recovery cannot replay an old completion.
	if next.TurnState == "unknown" {
		return nil
	}
	type turn struct {
		State string
		ID    string
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	key := "observed-turn:" + next.ID
	old := turn{previous.TurnState, previous.TurnID}
	var raw []byte
	err = tx.QueryRow("SELECT value FROM kv WHERE key=?", key).Scan(&raw)
	if err == nil {
		if err = json.Unmarshal(raw, &old); err != nil {
			return err
		}
		existed = true
	} else if err != sql.ErrNoRows {
		return err
	}
	current := turn{next.TurnState, next.TurnID}
	// Persist completion identity independently of transient working/waiting
	// observations. One native turn may finish only once across restarts.
	completionKey := ""
	duplicateCompletion := false
	if next.TurnState == "completed" && next.TurnID != "" {
		completionKey = fmt.Sprintf("completed-turn:%x", sha256.Sum256([]byte(next.ID+"\x00"+next.TurnID)))
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM kv WHERE key=?", completionKey).Scan(&count); err != nil {
			return err
		}
		duplicateCompletion = count > 0
	}
	if existed && old != current && notify && !duplicateCompletion && !(next.TurnState == "completed" && next.ExecutionState == "exited") {
		data, err := json.Marshal(core.NotificationFor(next))
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO events(kind,session,node,at,data) VALUES(?,?,?,?,?)", next.TurnState, next.ID, next.NodeID, time.Now().UTC().Format(time.RFC3339Nano), data); err != nil {
			return err
		}
	}
	if completionKey != "" {
		if _, err = tx.Exec("INSERT OR IGNORE INTO kv VALUES(?,?)", completionKey, []byte("true")); err != nil {
			return err
		}
	}
	raw, _ = json.Marshal(current)
	if _, err = tx.Exec("INSERT INTO kv VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, raw); err != nil {
		return err
	}
	return tx.Commit()
}
