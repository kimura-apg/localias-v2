package root

import "strings"

// termBuffer is a tiny terminal emulator (scrollback + cursor subset) used
// to render the wrapped dev server's output inside the TUI. Dev servers
// like nuxt/webpackbar redraw multi-line progress blocks using carriage
// returns, line-erase and cursor-up sequences spanning embedded newlines —
// no per-line CR/LF classification can reconstruct that, so we interpret
// the byte stream the way a real terminal would: in-place edits mutate
// existing lines instead of appending new ones.
//
// Supported: printable runes, \n, \r, \b, CSI SGR (kept for color),
// CUU/CUD/CUF/CUB (A/B/C/D), EL (K), ED (J), and CUP (H) as home.
type termBuffer struct {
	lines [][]rune
	row   int
	col   int
	cap   int

	// CSI parser state (sequences may arrive split across writes).
	pending []rune
	inEsc   bool
	inCSI   bool
}

const defaultTermCap = 5000 //nolint:gochecknoglobals

func newTermBuffer() *termBuffer {
	return &termBuffer{lines: [][]rune{{}}, cap: defaultTermCap}
}

// Write feeds raw bytes into the emulator.
func (t *termBuffer) Write(p []byte) {
	for _, r := range string(p) {
		t.writeRune(r)
	}
	t.trim()
}

func (t *termBuffer) writeRune(r rune) {
	switch {
	case t.inCSI:
		t.pending = append(t.pending, r)
		if r >= 0x40 && r <= 0x7E {
			t.inCSI = false
			t.inEsc = false
			t.applyCSI(t.pending)
			t.pending = t.pending[:0]
		}
		return
	case t.inEsc:
		if r == '[' {
			t.inCSI = true
			return
		}
		t.inEsc = false // not CSI; drop (e.g. OSC unsupported)
		return
	}

	switch r {
	case 0x1b:
		t.inEsc = true
	case '\n':
		t.row++
		t.ensureRow()
	case '\r':
		t.col = 0
	case '\b':
		if t.col > 0 {
			t.col--
		}
	default:
		t.putRune(r)
	}
}

// applyCSI handles a complete CSI sequence (params + final byte).
func (t *termBuffer) applyCSI(seq []rune) {
	final := seq[len(seq)-1]
	params := strings.TrimSuffix(string(seq), string(final))
	n := 1
	if params != "" {
		if v := atoiDefault(params, 1); v > 0 {
			n = v
		}
	}
	switch final {
	case 'm':
		// Keep color escapes inline (zero width); re-emit the CSI prefix
		// (ESC and "[" were consumed by the state machine).
		t.putRune(0x1b)
		t.putRune('[')
		for _, pr := range seq {
			t.putRune(pr)
		}
	case 'A':
		t.row -= n
		if t.row < 0 {
			t.row = 0
		}
	case 'B':
		t.row += n
		t.ensureRow()
	case 'C':
		t.col += n
	case 'D':
		t.col -= n
		if t.col < 0 {
			t.col = 0
		}
	case 'K':
		switch n {
		case 0:
			t.ensureRow()
			if t.col <= len(t.lines[t.row]) {
				t.lines[t.row] = t.lines[t.row][:t.col]
			}
		default: // 1 and 2: clear the whole line, cursor home
			t.ensureRow()
			t.lines[t.row] = nil
			t.col = 0
		}
	case 'J':
		// 0: clear to end of screen; 2: clear screen (keep cursor line).
		t.ensureRow()
		if n >= 2 {
			t.lines = t.lines[:t.row+1]
			t.lines[t.row] = nil
			t.col = 0
		} else {
			t.lines = t.lines[:t.row+1]
		}
	case 'H':
		t.row, t.col = 0, 0
	}
}

func (t *termBuffer) putRune(r rune) {
	t.ensureRow()
	line := t.lines[t.row]
	for len(line) < t.col {
		line = append(line, ' ')
	}
	if t.col < len(line) {
		line[t.col] = r
	} else {
		line = append(line, r)
	}
	t.lines[t.row] = line
	t.col++
}

func (t *termBuffer) ensureRow() {
	for t.row >= len(t.lines) {
		t.lines = append(t.lines, []rune{})
	}
	if t.row < 0 {
		t.row = 0
	}
}

func (t *termBuffer) trim() {
	if len(t.lines) > t.cap {
		drop := len(t.lines) - t.cap
		t.lines = t.lines[drop:]
		t.row -= drop
		if t.row < 0 {
			t.row = 0
		}
	}
}

// String renders the buffer as plain lines (SGR escapes preserved for
// color; movement/erase sequences are consumed, never stored).
func (t *termBuffer) String() string {
	var b strings.Builder
	for i, line := range t.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(line))
	}
	return b.String()
}

// TailLines returns up to k of the most recent lines, ANSI-stripped.
func (t *termBuffer) TailLines(k int) []string {
	var out []string
	for i := len(t.lines) - 1; i >= 0 && len(out) < k; i-- {
		clean, _ := sanitizeLogLine(string(t.lines[i]))
		out = append(out, clean)
	}
	return out
}

func atoiDefault(s string, def int) int {
	v := 0
	ok := false
	for _, c := range s {
		if c < '0' || c > '9' {
			return def // compound/absent params: use default
		}
		v = v*10 + int(c-'0')
		ok = true
	}
	if !ok {
		return def
	}
	return v
}
