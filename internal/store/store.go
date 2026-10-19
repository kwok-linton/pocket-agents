package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

func Open(path string) (*Store, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS kv(key TEXT PRIMARY KEY, value BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS actions(id TEXT PRIMARY KEY, digest TEXT NOT NULL, result BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, token TEXT NOT NULL, name TEXT NOT NULL, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, node TEXT NOT NULL, data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS sessions_node ON sessions(node);
CREATE TABLE IF NOT EXISTS metadata(id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, session TEXT NOT NULL, node TEXT NOT NULL, at TEXT NOT NULL, data BLOB);
CREATE TABLE IF NOT EXISTS push(endpoint TEXT PRIMARY KEY, data BLOB NOT NULL);
UPDATE actions SET result=json_set(result,'$.status','unknown','$.message','process restarted; reconcile native history before explicitly resending') WHERE json_extract(result,'$.status')='submitted';`)
	if e != nil {
		db.Close()
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Put(key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO kv VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, b)
	return e
}
func (s *Store) Get(key string, v any) error {
	var b []byte
	if e := s.DB.QueryRow("SELECT value FROM kv WHERE key=?", key).Scan(&b); e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func (s *Store) Claim(scope string, a core.Action) (core.Result, bool, error) {
	b, e := json.Marshal(a)
	if e != nil {
		return core.Result{}, false, e
	}
	digest := config.Hash(string(b))
	r := core.Result{ActionID: a.ID, Status: "submitted"}
	rb, _ := json.Marshal(r)
	x, e := s.DB.Exec("INSERT OR IGNORE INTO actions VALUES(?,?,?)", scope+a.ID, digest, rb)
	if e != nil {
		return r, false, e
	}
	n, _ := x.RowsAffected()
	if n == 1 {
		return r, true, nil
	}
	var old string
	if e = s.DB.QueryRow("SELECT digest,result FROM actions WHERE id=?", scope+a.ID).Scan(&old, &rb); e != nil {
		return r, false, e
	}
	if old != digest {
		return r, false, errors.New("action ID was already used with a different payload")
	}
	e = json.Unmarshal(rb, &r)
	return r, false, e
}
func (s *Store) Result(scope, id string) (core.Result, error) {
	var b []byte
	var r core.Result
	e := s.DB.QueryRow("SELECT result FROM actions WHERE id=?", scope+id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &r)
	}
	return r, e
}
func (s *Store) Finish(scope string, r core.Result) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("UPDATE actions SET result=? WHERE id=?", b, scope+r.ActionID)
	return e
}
func (s *Store) AddEvent(kind, session, node string, v any) (core.Event, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return core.Event{}, e
	}
	ev := core.Event{Kind: kind, SessionID: session, NodeID: node, At: time.Now().UTC(), Data: b}
	r, e := s.DB.Exec("INSERT INTO events(kind,session,node,at,data) VALUES(?,?,?,?,?)", kind, session, node, ev.At.Format(time.RFC3339Nano), b)
	if e != nil {
		return ev, e
	}
	ev.ID, e = r.LastInsertId()
	return ev, e
}
func (s *Store) Events(after int64, notifications bool) ([]core.Event, error) {
	q := "SELECT id,kind,session,node,at,data FROM events WHERE id>?"
	if notifications {
		q += " AND kind IN ('completed','waiting_approval','waiting_input','error','interrupted')"
	}
	q += " ORDER BY id LIMIT 500"
	return s.events(q, after)
}

// Inbox returns the newest attention events, oldest first. Cursor reads
// (Events) page forward from the beginning, which would freeze an inbox on the
// first 500 events ever recorded.
func (s *Store) Inbox(limit int) ([]core.Event, error) {
	return s.events("SELECT * FROM (SELECT id,kind,session,node,at,data FROM events WHERE kind IN ('completed','waiting_approval','waiting_input','error','interrupted') ORDER BY id DESC LIMIT ?) ORDER BY id", limit)
}
func (s *Store) events(q string, arg any) ([]core.Event, error) {
	rows, e := s.DB.Query(q, arg)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []core.Event{}
	for rows.Next() {
		var v core.Event
		var at string
		if e = rows.Scan(&v.ID, &v.Kind, &v.SessionID, &v.NodeID, &at, &v.Data); e != nil {
			return nil, e
		}
		v.At, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Metadata(id string) core.Metadata {
	var b []byte
	var m core.Metadata
	if s.DB.QueryRow("SELECT data FROM metadata WHERE id=?", id).Scan(&b) == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// AllMetadata reads every row at once; the session list needs all of them and
// one query per session costs over a thousand round trips per refresh.
func (s *Store) AllMetadata() (map[string]core.Metadata, error) {
	rows, e := s.DB.Query("SELECT id,data FROM metadata")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]core.Metadata{}
	for rows.Next() {
		var id string
		var b []byte
		var m core.Metadata
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(b, &m)
		out[id] = m
	}
	return out, rows.Err()
}
func (s *Store) SetMetadata(id string, m core.Metadata) error {
	b, _ := json.Marshal(m)
	_, e := s.DB.Exec("INSERT INTO metadata VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", id, b)
	return e
}
