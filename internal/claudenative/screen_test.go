package claudenative

import (
	"strings"
	"testing"
)

func TestTerminalWideCharactersDoNotInsertSpaces(t *testing.T) {
	for _, text := range []string{"中文选项", "检查配置 / logs", "❯ 1. [✔] 检查日志", "完成 🚀 OK"} {
		if got := terminalScreen([]byte(text)); got != text {
			t.Fatalf("wide text changed: got %q, want %q", got, text)
		}
	}
	// Cursor coordinates still count terminal columns, even though the plain
	// text sent to the phone omits the wide glyph's continuation cell.
	if got := terminalScreen([]byte("中文ABC\x1b[1;5HXYZ")); got != "中文XYZ" {
		t.Fatalf("cursor columns changed: %q", got)
	}
	if got := terminalScreen([]byte("中文ABC\x1b[1;3H\x1b[K件")); got != "中件" {
		t.Fatalf("erase/redraw changed: %q", got)
	}
	if got := terminalScreen([]byte("中Q\x1b[1;1HA")); got != "A Q" {
		t.Fatalf("replacing a wide glyph collapsed columns: %q", got)
	}
	if got := terminalScreen([]byte("中Q\x1b[1;2HAB")); got != "AB" {
		t.Fatalf("overwriting a continuation retained half a glyph: %q", got)
	}
}

func TestWideSuggestedReplyKeepsOriginalText(t *testing.T) {
	screen, suggestion := terminalView([]byte("❯ \x1b[2m检查配置 logs\x1b[22m"))
	if screen != "❯ 检查配置 logs" || suggestion != "检查配置 logs" {
		t.Fatalf("suggestion text changed: %q / %q", screen, suggestion)
	}
}

func TestTerminalViewPreservesRowsBelowPhoneDefault(t *testing.T) {
	screen, _ := terminalViewSize([]byte("\x1b[40;1H第一行\x1b[43;1H❯ 2. [✔] 第二行\x1b[45;1H底部"), 45, 93)
	lines := strings.Split(screen, "\n")
	if len(lines) != 6 || lines[0] != "第一行" || lines[3] != "❯ 2. [✔] 第二行" || lines[5] != "底部" {
		t.Fatalf("desktop rows collapsed: %q", screen)
	}
	for _, size := range [][2]int{{0, 0}, {-1, -1}, {100000, 100000}} {
		got, _ := terminalViewSize([]byte("屏幕"), size[0], size[1])
		if got != "屏幕" {
			t.Fatalf("invalid dimensions did not fall back: %q", got)
		}
	}
}
