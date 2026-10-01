package root

import "regexp"

// TUI log sanitization: dev-server progress bars (webpack/nuxt) redraw a
// single line using carriage returns and ANSI cursor moves instead of
// printing new lines. Appending every redraw verbatim floods the viewport
// (observed: 18 interleaved "building (N%)" snapshots). sanitizeLogLine
// turns a raw output chunk into a display line plus a redraw hint so the
// TUI can update the previous line in place.

var (
	// Cursor-movement / erase sequences (CUU/CUD/CUF/CUB/EL/ED), NOT SGR
	// color codes — colors are kept for display.
	cursorCodes = regexp.MustCompile(`\x1b\[[0-9;]*[ABCDEFGJK]`)
)

// sanitizeLogLine strips cursor control codes (keeping color escapes) and
// reports whether the chunk was a redraw (contained a bare CR or cursor
// codes), meaning the TUI should replace the last line instead of
// appending.
func sanitizeLogLine(raw string) (clean string, redraw bool) {
	redraw = false
	s := raw
	if i := lastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
		redraw = true
	}
	if cursorCodes.MatchString(s) {
		redraw = true
	}
	s = cursorCodes.ReplaceAllString(s, "")
	return s, redraw
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
