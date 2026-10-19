package hub

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"pocketagents/internal/core"
	"strings"
	"testing"
	"time"
)

func TestConditionalReplyRevalidatesAndCompresses(t *testing.T) {
	list := []map[string]string{}
	for i := 0; i < 200; i++ {
		list = append(list, map[string]string{"title": strings.Repeat("会话", 20)})
	}
	first := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.Header.Set("Accept-Encoding", "gzip, deflate, br")
	replyConditional(first, r, list)
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" || first.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("large reply not tagged and compressed", first.Code, first.Header())
	}
	z, e := gzip.NewReader(first.Body)
	if e != nil {
		t.Fatal(e)
	}
	var decoded []map[string]string
	if body, _ := io.ReadAll(z); json.Unmarshal(body, &decoded) != nil || len(decoded) != 200 {
		t.Fatal("compressed body does not decode to the original list")
	}
	again := httptest.NewRecorder()
	r.Header.Set("If-None-Match", tag)
	replyConditional(again, r, list)
	if again.Code != 304 || again.Body.Len() != 0 {
		t.Fatal("unchanged list was resent", again.Code, again.Body.Len())
	}
	list[0]["title"] = "changed"
	changed := httptest.NewRecorder()
	replyConditional(changed, r, list)
	if changed.Code != 200 || changed.Header().Get("ETag") == tag {
		t.Fatal("changed list matched the stale ETag")
	}
	small := httptest.NewRecorder()
	replyConditional(small, httptest.NewRequest("GET", "/api/devices", nil), []string{"a"})
	if small.Header().Get("Content-Encoding") != "" || strings.TrimSpace(small.Body.String()) != `["a"]` {
		t.Fatal("small or non-gzip client reply altered", small.Header(), small.Body.String())
	}
}

func TestUnchangedSessionListKeepsItsETag(t *testing.T) {
	h := testHub(t)
	same := time.Now().UTC()
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("s%02d", i)
		h.sessions[id] = core.Session{ID: id, NativeID: id, Provider: "claude", UpdatedAt: same}
	}
	tag := ""
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		h.listSessions(w, httptest.NewRequest("GET", "/api/sessions", nil))
		if tag != "" && w.Header().Get("ETag") != tag {
			t.Fatal("identical sessions produced a different ETag; 304 can never fire")
		}
		tag = w.Header().Get("ETag")
	}
}
