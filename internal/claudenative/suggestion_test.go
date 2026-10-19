package claudenative

import "testing"

func TestNativeSuggestedReplyIsNotTypedInput(t *testing.T) {
	raw := []byte("\x1b[30;1H❯\u00a0\x1b[2mUse Claude to review\x1b[22m")
	if terminalSuggestedReply(raw) != "Use Claude to review" {
		t.Fatal("faint prompt not recognized")
	}
	// The same text becomes real input when the native CLI redraws it normally.
	typed := append(append([]byte{}, raw...), []byte("\x1b[30;1H\x1b[2K❯ Use Claude to review")...)
	if terminalSuggestedReply(typed) != "" {
		t.Fatal("typed text mistaken for suggestion")
	}
	cleared := append(append([]byte{}, raw...), []byte("\x1b[2J")...)
	if terminalSuggestedReply(cleared) != "" {
		t.Fatal("cleared suggestion retained")
	}
	if terminalSuggestedReply([]byte("\x1b[2mSome faint output\x1b[22m\r\n❯ hello")) != "" {
		t.Fatal("ordinary input mistaken for suggestion")
	}
}

func TestRGBColorDoesNotMeanFaint(t *testing.T) {
	if terminalSuggestedReply([]byte("❯ \x1b[38;2;2;22;0mtyped text")) != "" {
		t.Fatal("RGB color interpreted as faint")
	}
	if terminalSuggestedReply([]byte("❯ \x1b[2m\x1b[38;2;255;255;255msuggestion")) != "suggestion" {
		t.Fatal("RGB color reset faint style")
	}
}

func terminalSuggestedReply(raw []byte) string { _, suggestion := terminalView(raw); return suggestion }
