package root

import (
	"bufio"
	"strings"
	"testing"

	"github.com/peterldowns/testy/check"
)

func TestSanitizeLogLine(t *testing.T) {
	t.Parallel()
	// Plain lines pass through untouched (colors kept).
	clean, redraw := sanitizeLogLine("\x1b[32m✔ Client: Compiled successfully\x1b[0m")
	check.Equal(t, "\x1b[32m✔ Client: Compiled successfully\x1b[0m", clean)
	check.Equal(t, false, redraw)

	// Carriage-return progress redraws collapse to the last segment and
	// are marked as redraws (regression: webpack/nuxt progress flooded
	// the TUI with one line per update).
	clean, redraw = sanitizeLogLine("building (10%)\r  ● Client building (67%) 1173/1228 modules")
	check.Equal(t, "  ● Client building (67%) 1173/1228 modules", clean)
	check.Equal(t, true, redraw)

	// Cursor-movement/erase codes are stripped and flag a redraw, but
	// SGR color codes survive.
	clean, redraw = sanitizeLogLine("\x1b[1A\x1b[2K\x1b[36m● Server building (68%)\x1b[0m")
	check.Equal(t, "\x1b[36m● Server building (68%)\x1b[0m", clean)
	check.Equal(t, true, redraw)

	// A lone CR produces an empty redraw (skipped by the TUI).
	clean, redraw = sanitizeLogLine("\r")
	check.Equal(t, "", clean)
	check.Equal(t, true, redraw)
}

func TestSplitOnCROrLF(t *testing.T) {
	t.Parallel()
	// LF, CR and CRLF all terminate tokens; CR-terminated progress
	// updates must not wait for an LF that never comes.
	sc := bufio.NewScanner(strings.NewReader("a\nb\rc\r\nd"))
	sc.Split(splitOnCROrLF)
	var tokens []string
	for sc.Scan() {
		tokens = append(tokens, sc.Text())
	}
	check.Equal(t, []string{"a", "b", "c", "d"}, tokens)
}
