package claudenative

import (
	"sync"
	"time"
)

// This is only a correlation hint, never sufficient to authorize stop. The
// native transcript must independently confirm /exit after this submission,
// and this same client must exit successfully. Unrecognized terminal editing
// fails closed to detach, preserving the worker. It also prevents an observer
// client from treating another terminal's /exit as its own stop request.
type exitInput struct {
	mu       sync.Mutex
	line     string
	escape   bool
	sequence string
	at       time.Time
}

func (e *exitInput) observe(input []byte, writtenAt time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, b := range input {
		if e.escape {
			e.sequence += string(b)
			if len(e.sequence) == 1 && b == '[' {
				continue
			}
			if e.sequence[0] != '[' || b >= 0x40 && b <= 0x7e {
				if e.sequence != "[200~" && e.sequence != "[201~" {
					e.line = "?"
				}
				e.escape = false
				e.sequence = ""
			}
			continue
		}
		switch b {
		case '\x1b':
			e.sequence = ""
			e.at = time.Time{}
			e.escape = true
		case '\r', '\n':
			if e.line == "/exit" {
				e.at = writtenAt
			} else if b != '\n' {
				e.at = time.Time{}
			}
			e.line = ""
		case '\b', 127:
			if len(e.line) > 0 {
				e.line = e.line[:len(e.line)-1]
			}
			e.at = time.Time{}
		case 21:
			e.line = ""
			e.at = time.Time{} // Ctrl+U clears the current line.
		default:
			e.at = time.Time{}
			if b < 32 || b > 126 || len(e.line) > 64 {
				e.line = "?"
			} else {
				e.line += string(b)
			}
		}
	}
}
func (e *exitInput) submittedAt() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.at }
