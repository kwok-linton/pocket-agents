package claudenative

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// terminalScreen is a bounded, text-only rendering of a native PTY frame. ANSI
// remains available separately for a full terminal emulator. No escape text is
// interpreted as HTML or executed in the browser.
func terminalScreen(raw []byte) string {
	screen, _ := terminalView(raw)
	return screen
}

// Preserve faint prompt text so suggested replies are not mistaken for input.
func terminalView(raw []byte) (string, string) {
	return terminalViewSize(raw, 32, 100)
}

func terminalViewSize(raw []byte, rows, cols int) (string, string) {
	if rows <= 0 || rows > 200 {
		rows = 32
	}
	if cols <= 0 || cols > 500 {
		cols = 100
	}
	// A wide glyph occupies two terminal columns, but only one Unicode rune.
	// Do not turn its second column into a visible space in the mobile view.
	const continuation rune = -1
	cells := make([][]rune, rows)
	faint := make([][]bool, rows)
	clear := func() {
		for r := range cells {
			cells[r] = make([]rune, cols)
			faint[r] = make([]bool, cols)
		}
	}
	clear()
	eraseCell := func(r, c int) {
		if cells[r][c] == continuation && c > 0 {
			cells[r][c-1], faint[r][c-1] = 0, false
		}
		if c+1 < cols && cells[r][c+1] == continuation {
			cells[r][c+1], faint[r][c+1] = 0, false
		}
		cells[r][c], faint[r][c] = 0, false
	}
	dim := false
	row, col, sr, sc := 0, 0, 0, 0
	clamp := func() {
		if row < 0 {
			row = 0
		}
		if row >= rows {
			row = rows - 1
		}
		if col < 0 {
			col = 0
		}
		if col >= cols {
			col = cols - 1
		}
	}
	for i := 0; i < len(raw); {
		if raw[i] == 27 {
			i++
			if i >= len(raw) {
				break
			}
			switch raw[i] {
			case '[':
				i++
				start := i
				for i < len(raw) && (raw[i] < 0x40 || raw[i] > 0x7e) {
					i++
				}
				if i >= len(raw) {
					break
				}
				code := raw[i]
				params := string(raw[start:i])
				i++
				if strings.HasPrefix(params, "?") || strings.HasPrefix(params, ">") {
					continue
				}
				parts := strings.Split(params, ";")
				get := func(n, def int) int {
					if n >= len(parts) || parts[n] == "" {
						return def
					}
					v, e := strconv.Atoi(parts[n])
					if e != nil || v == 0 {
						return def
					}
					return v
				}
				n := get(0, 1)
				switch code {
				case 'm':
					for index := 0; index < len(parts); index++ {
						part := parts[index]
						if (part == "38" || part == "48" || part == "58") && index+1 < len(parts) {
							if parts[index+1] == "2" {
								index += 4
							} else if parts[index+1] == "5" {
								index += 2
							}
							continue
						}
						switch part {
						case "", "0", "22":
							dim = false
						case "2":
							dim = true
						}
					}
				case 'H', 'f':
					row = get(0, 1) - 1
					col = get(1, 1) - 1
				case 'A':
					row -= n
				case 'B':
					row += n
				case 'C':
					col += n
				case 'D':
					col -= n
				case 'E':
					row += n
					col = 0
				case 'F':
					row -= n
					col = 0
				case 'G':
					col = n - 1
				case 'd':
					row = n - 1
				case 'J':
					if params == "2" || params == "3" {
						clear()
					} else {
						for r := row; r < rows; r++ {
							c := 0
							if r == row {
								c = col
							}
							for ; c < cols; c++ {
								eraseCell(r, c)
							}
						}
					}
				case 'K':
					first, last := col, cols
					if params == "1" {
						first = 0
						last = col + 1
					}
					if params == "2" {
						first = 0
					}
					for c := first; c < last; c++ {
						eraseCell(row, c)
					}
				case 's':
					sr, sc = row, col
				case 'u':
					row, col = sr, sc
				}
				clamp()
			case ']':
				i++
				for i < len(raw) && raw[i] != 7 {
					if raw[i] == 27 && i+1 < len(raw) && raw[i+1] == '\\' {
						i++
						break
					}
					i++
				}
				if i < len(raw) {
					i++
				}
			case '7':
				sr, sc = row, col
				i++
			case '8':
				row, col = sr, sc
				i++
			case '(', ')':
				i += 2
			default:
				i++
			}
			continue
		}
		b := raw[i]
		if b == '\r' {
			col = 0
			i++
			continue
		}
		if b == '\n' {
			row++
			if row >= rows {
				copy(cells[:], cells[1:])
				copy(faint[:], faint[1:])
				cells[rows-1] = make([]rune, cols)
				faint[rows-1] = make([]bool, cols)
				row = rows - 1
			}
			i++
			continue
		}
		if b == '\b' {
			col--
			clamp()
			i++
			continue
		}
		if b < 32 || b == 127 {
			i++
			continue
		}
		r, n := utf8.DecodeRune(raw[i:])
		i += n
		if r == utf8.RuneError && n == 1 {
			continue
		}
		wide := r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xff01 && r <= 0xff60 || r >= 0x1f300)
		eraseCell(row, col)
		if wide && col+1 < cols {
			eraseCell(row, col+1)
		}
		cells[row][col] = r
		faint[row][col] = dim
		col++
		if wide {
			if col < cols {
				cells[row][col] = continuation
				faint[row][col] = dim
			}
			col++
		}
		clamp()
	}
	lines := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			v := cells[r][c]
			if v == continuation {
				continue
			}
			if v == 0 {
				v = ' '
			}
			b.WriteRune(v)
		}
		lines[r] = strings.TrimRight(b.String(), " ")
	}
	suggestion := ""
	for r := rows - 1; r >= 0; r-- {
		// Only the input prompt, not dim explanatory text elsewhere in the screen.
		line := strings.TrimSpace(lines[r])
		if !strings.HasPrefix(line, "❯") {
			continue
		}
		var b strings.Builder
		allDim, found := true, false
		for c := 0; c < cols; c++ {
			ch := cells[r][c]
			if !found {
				if ch == '❯' {
					found = true
				}
				continue
			}
			if ch == continuation || ch == 0 || ch == ' ' || ch == '\u00a0' {
				continue
			}
			if !faint[r][c] {
				allDim = false
			}
			b.WriteRune(ch)
		}
		if allDim && b.Len() > 0 {
			suggestion = strings.TrimSpace(strings.TrimPrefix(line, "❯"))
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), suggestion
}
