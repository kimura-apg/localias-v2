package root

import (
	"strings"
	"testing"

	"github.com/peterldowns/testy/assert"
	"github.com/peterldowns/testy/check"
)

func TestParseDevArgs(t *testing.T) {
	t.Parallel()
	// Canonical form: alias -- command...
	alias, cmdArgs, err := parseDevArgs([]string{"sawada.localhost", "--", "yarn", "dev"})
	assert.NoError(t, err)
	assert.Equal(t, "http://sawada.localhost", alias)
	assert.Equal(t, []string{"yarn", "dev"}, cmdArgs)

	// Explicit scheme is preserved.
	alias, _, err = parseDevArgs([]string{"https://app.test", "--", "python3", "-m", "http.server"})
	assert.NoError(t, err)
	assert.Equal(t, "https://app.test", alias)

	// "--" before the alias is an error.
	_, _, err = parseDevArgs([]string{"--", "yarn", "dev"})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "expected <alias> before"))

	// Missing alias surfaces as a dot-less alias (cobra may strip "--"):
	// must produce the targeted hint, never reach exec.
	_, _, err = parseDevArgs([]string{"yarn", "dev"})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "did you forget the alias"))

	// Alias with no command is an error.
	_, _, err = parseDevArgs([]string{"app.test"})
	assert.Error(t, err)

	// Invalid alias (wildcard not leftmost) still goes through ValidateAlias.
	_, _, err = parseDevArgs([]string{"foo.*.test", "--", "true"})
	assert.Error(t, err)
}

func TestListenPortFromLineIgnoresAlreadyInUse(t *testing.T) {
	t.Parallel()
	// nuxt's fallback warning must never be registered (regression: the
	// dead port 8924 was registered instead of the real 54870).
	check.Equal(t, 0, listenPortFromLine("WARN  Address 0.0.0.0:8924 is already in use."))
	check.Equal(t, 54870, listenPortFromLine("ℹ Listening on: http://192.168.1.8:54870/"))
}

func TestListenPortFromLineStripsANSI(t *testing.T) {
	t.Parallel()
	// Colors from the pty must not break matching.
	check.Equal(t, 3000, listenPortFromLine("\x1b[32m➜\x1b[0m  Local:   http://localhost:3000/"))
	check.Equal(t, 8924, listenPortFromLine("\x1b[2mℹ\x1b[0m Listening on: \x1b[4mhttp://192.168.1.8:8924/\x1b[24m"))
}
