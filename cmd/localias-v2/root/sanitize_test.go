package root

import (
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

func TestTermBufferInPlaceRedraw(t *testing.T) {
	t.Parallel()
	// Regression (nuxt/webpackbar): the progress block spans two lines and
	// is redrawn via CR + line-erase + cursor-up sequences with embedded
	// newlines, interleaved with a plain LF WARN block. A real terminal
	// shows ONE progress block updating in place and ONE warn; the buffer
	// must reproduce exactly that instead of one line per update.
	vt := newTermBuffer()
	vt.Write([]byte("\r\x1b[2K\u25cf Client building (63%) 450/509 modules\n vue-loader \u203a TwoLineNavButton.vue\x1b[1A\x1b[1A"))
	vt.Write([]byte("\r\x1b[2K\u25cf Client building (64%) 464/511 modules\n babel-loader \u203a history/index.js\x1b[1A\x1b[1A"))
	vt.Write([]byte("\n WARN Browserslist: caniuse-lite is outdated.\n  npx update-browserslist-db@latest\n"))
	vt.Write([]byte("\r\x1b[2K\u25cf Client building (65%) 489/545 modules\n node_modules/axios/lib/utils.js\x1b[1A\x1b[1A"))
	out := vt.String()
	lines := strings.Split(out, "\n")
	count := func(sub string) int {
		c := 0
		for _, l := range lines {
			if strings.Contains(l, sub) {
				c++
			}
		}
		return c
	}
	check.Equal(t, 0, count("63%"))
	check.Equal(t, 1, count("64%")) // warn interleave scrolls; the 64% block stays, redraws continue below
	check.Equal(t, 1, count("65%"))
	check.Equal(t, 1, count("Browserslist"))

	// Colors survive: SGR sequences are kept inline.
	vt2 := newTermBuffer()
	vt2.Write([]byte("\x1b[36mServer building\x1b[0m\n"))
	check.Equal(t, "\x1b[36mServer building\x1b[0m\n", vt2.String())
}

func TestTermBufferDetectsPortAcrossCursorMerges(t *testing.T) {
	t.Parallel()
	// A progress redraw that ends with cursor-up (no CR) leaves the
	// cursor mid-buffer; the next plain line merges into an earlier row.
	// Port detection must scan enough tail lines to still find it.
	detect := newTermBuffer()
	detect.Write([]byte("start\n\r\x1b[2K\x1b[36mClient 63\x1b[0m\n mod\x1b[1A\x1b[1AListening on: http://127.0.0.1:18952/\n"))
	found := 0
	for _, tl := range detect.TailLines(8) {
		if p := listenPortFromLine(tl); p > 0 {
			found = p
		}
	}
	check.Equal(t, 18952, found)
}
