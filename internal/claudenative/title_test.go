package claudenative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"pocketagents/internal/core"
)

func writeTitleFixture(t *testing.T, rows ...map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, row := range rows {
		if err := json.NewEncoder(f).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func userTitleRow(content any) map[string]any {
	return map[string]any{"type": "user", "cwd": "/project", "message": map[string]any{"content": content}}
}

func TestNativeSessionTitlesTakePrecedenceOverInstanceNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []map[string]any
		want string
	}{
		{"custom wins over newer automatic title", []map[string]any{
			userTitleRow("original task"),
			{"type": "custom-title", "customTitle": "Release notes"},
			{"type": "ai-title", "aiTitle": "Automatic release summary"},
		}, "Release notes"},
		{"latest custom rename", []map[string]any{
			{"type": "custom-title", "customTitle": "Old name"},
			{"type": "custom-title", "customTitle": "User's chosen name"},
		}, "User's chosen name"},
		{"latest automatic title", []map[string]any{
			userTitleRow("original task"),
			{"type": "ai-title", "aiTitle": "Old generated title"},
			{"type": "ai-title", "aiTitle": "Investigate connection timeout"},
		}, "Investigate connection timeout"},
		{"first meaningful task", []map[string]any{
			userTitleRow("Fix the export dialog"),
			userTitleRow("Then add a regression check"),
		}, "Fix the export dialog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := readMetadata(writeTitleFixture(t, tc.rows...), "default")
			if err != nil || s.Title != tc.want {
				t.Fatalf("title=%q, want %q; err=%v", s.Title, tc.want, err)
			}
			for _, kind := range []string{"background", "interactive"} {
				applyAgent(&s, agent{Name: "project-a0", Kind: kind, Cwd: "/project"})
				if s.Title != tc.want {
					t.Fatalf("%s instance replaced native title with %q", kind, s.Title)
				}
			}
		})
	}
}

func TestTitleFallbackIgnoresNativeInjectedUserRecords(t *testing.T) {
	rows := []map[string]any{
		userTitleRow("<local-command-caveat>Generated CLI caveat</local-command-caveat>"),
		userTitleRow("<command-message>clear</command-message><command-name>/clear</command-name>"),
		userTitleRow("<local-command-stdout>Local command result</local-command-stdout>"),
		userTitleRow("<task-notification>Worker completed</task-notification>"),
		userTitleRow([]map[string]any{{"type": "tool_result", "content": "tool output"}}),
		{"type": "summary", "summary": "Compacted conversation"},
	}
	for _, flag := range []string{"isMeta", "isCompactSummary", "isSidechain"} {
		row := userTitleRow("Injected context, not a task")
		row[flag] = true
		rows = append(rows, row)
	}
	row := userTitleRow("System prompt source")
	row["promptSource"] = "system"
	rows = append(rows, row)
	rows = append(rows,
		userTitleRow([]map[string]any{
			{"type": "text", "text": "<system-reminder>Private injected context</system-reminder>\n Repair\n  checkout validation"},
			{"type": "image", "source": map[string]any{"type": "base64", "data": "fixture"}},
		}),
		userTitleRow("A later user task"),
	)
	path := writeTitleFixture(t, rows...)
	before, _ := os.ReadFile(path)
	s, err := readMetadata(path, "default")
	if err != nil || s.Title != "Repair checkout validation" {
		t.Fatalf("unexpected fallback %q: %v", s.Title, err)
	}
	// Title filtering must not rewrite or remove the native conversation records.
	h, err := readHistory(context.Background(), path, "", 100)
	if err != nil || len(h.Messages) == 0 || !strings.Contains(h.Messages[0].Text, "local-command-caveat") {
		t.Fatalf("title cleanup changed history: %+v %v", h, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("native transcript was modified")
	}
}

func TestTitleFallbackRetainsReadableUnicodeAndUsesInstanceOnlyWhenMissing(t *testing.T) {
	row := record{Type: "user"}
	row.Message.Content, _ = json.Marshal(strings.Repeat("修复", 100))
	if got := promptTitle(row); !utf8.ValidString(got) || len([]rune(got)) != 120 {
		t.Fatalf("invalid bounded Unicode title: %q", got)
	}
	for _, title := range []string{"", "Claude session"} {
		s := core.Session{Title: title}
		applyAgent(&s, agent{Name: "Native background task"})
		if s.Title != "Native background task" {
			t.Fatalf("missing title did not fall back to native instance: %q", s.Title)
		}
	}
}
