package server

import (
	"testing"

	"github.com/peterldowns/localias/pkg/config"
	"github.com/peterldowns/testy/check"
)

func TestEnsureSuffix(t *testing.T) {
	t.Parallel()
	result := ensureSuffix("hostname.local", ".local")
	check.Equal(t, "hostname.local", result)

	result = ensureSuffix("hostname", ".local")
	check.Equal(t, "hostname.local", result)

	result = ensureSuffix("hostname.local.foo.", ".local")
	check.Equal(t, "hostname.local.foo..local", result)
}

func TestShouldServeMDNS(t *testing.T) {
	t.Parallel()
	check.Equal(t, true, shouldServeMDNS(config.Entry{Alias: "foo.local", Port: 9000}))
	check.Equal(t, false, shouldServeMDNS(config.Entry{Alias: "foo.test", Port: 9000}))
	// Defense-in-depth: even if a wildcard+.local entry somehow reaches
	// here (ValidateAlias rejects it at `set` time), it must not be
	// registered, since mDNS can't serve wildcard records.
	check.Equal(t, false, shouldServeMDNS(config.Entry{Alias: "*.foo.local", Port: 9000}))
}
