package root

import (
	"bufio"
)

// readChunk reads one output chunk of the wrapped process, terminating on
// '\n' (a finished log line) or on a bare '\r'/CRLF (an in-place progress
// redraw, e.g. webpackbar's stage bars). The redraw flag tells the TUI to
// update the previous line instead of appending a new one — that is what
// keeps progress bars in one place like a real terminal.
func readChunk(r *bufio.Reader) (line string, redraw bool, err error) {
	var buf []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			if len(buf) > 0 {
				return string(buf), false, nil
			}
			return "", false, err
		}
		switch b {
		case '\n':
			return string(buf), false, nil
		case '\r':
			// Swallow a following LF so CRLF counts as a redraw, not as
			// an extra empty line.
			if nb, err := r.Peek(1); err == nil && nb[0] == '\n' {
				_, _ = r.ReadByte() //nolint:errcheck
			}
			return string(buf), true, nil
		default:
			buf = append(buf, b)
		}
	}
}
