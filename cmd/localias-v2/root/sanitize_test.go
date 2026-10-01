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

func TestReadChunk(t *testing.T) {
	t.Parallel()
	// LF-terminated chunks are plain lines (append); CR/CRLF-terminated
	// chunks are in-place progress redraws — the distinction keeps
	// progress bars animating in one place instead of one line per stage.
	r := bufio.NewReader(strings.NewReader("start\nbar 70%\rbar 88%\r\nfinal 98%\rdone\nplain\n"))
	type want struct {
		line   string
		redraw bool
	}
	var got []want
	for {
		line, redraw, err := readChunk(r)
		if err != nil {
			break
		}
		got = append(got, want{line, redraw})
	}
	expected := []want{
		{"start", false},
		{"bar 70%", true},
		{"bar 88%", true},
		{"final 98%", true},
		{"done", false},
		{"plain", false},
	}
	if len(got) != len(expected) {
		t.Fatalf("chunk count: want %d, got %d (%v)", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("chunk %d: want %+v, got %+v", i, expected[i], got[i])
		}
	}
}
