package root

import (
	"testing"

	"github.com/peterldowns/testy/check"
)

func TestFirstListenPort(t *testing.T) {
	t.Parallel()
	check.Equal(t, 3000, FirstListenPort("  ➜  Local:   http://localhost:3000/"))
	check.Equal(t, 5173, FirstListenPort("ready in 456 ms — http://127.0.0.1:5173/"))
	check.Equal(t, 8080, FirstListenPort("Server running at http://0.0.0.0:8080"))
	check.Equal(t, 8000, FirstListenPort("Serving HTTP on 0.0.0.0 port 8000 (http://0.0.0.0:8000/) ..."))
	check.Equal(t, 3437, FirstListenPort("listening on [::1]:3437"))
	check.Equal(t, 0, FirstListenPort("no ports here"))
	check.Equal(t, 0, FirstListenPort("version 1.2.3 of something"))
}
