package config

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"github.com/peterldowns/testy/assert"
	"github.com/peterldowns/testy/check"
)

// fullFeatureEntries exercises every alias feature of the fork at once:
// scheme prefixes, wildcard hosts, path-split routes (glob + regex), shared
// hosts and plain host entries.
func fullFeatureEntries() []Entry {
	return []Entry{
		{Alias: "http://pelog.localhost", Port: 3437},
		{Alias: "http://pelog.localhost/graphql", Port: 8787},
		{Alias: "http://pelog.localhost/api/*", Port: 8787},
		{Alias: "http://*.pelog.localhost", Port: 3437},
		{Alias: "http://*.pelog.localhost/api/*", Port: 8787},
		{Alias: "http://admin.pelog.localhost", Port: 3438},
		{Alias: "http://admin.pelog.localhost/admin/graphql", Port: 8788},
		{Alias: "http://shorui.localhost", Port: 3000},
		{Alias: "http://shorui.localhost/api/auth/*", Port: 8818},
		{Alias: "http://shorui.localhost/api/*", Port: 8817},
		{Alias: "http://shorui.localhost/~^/t/[^/]+/(graphql|api/|mcp)", Port: 8817},
	}
}

// Regression: the fork's Caddyfile generation must survive the Caddyfile →
// JSON adapter for every feature combination. A caddy upgrade that breaks
// adaptation (or generation) fails here instead of at daemon start.
func TestCaddyJSONAdaptsAllFeatures(t *testing.T) {
	t.Parallel()
	cfg := Config{Entries: fullFeatureEntries()}
	js, warnings, err := cfg.CaddyJSON()
	assert.NoError(t, err)
	check.True(t, len(js) > 0)
	for _, w := range warnings {
		if strings.Contains(w.Message, "unrecognized") {
			t.Errorf("adaptation warning: %s", w.Message)
		}
	}
}

// freePort reserves an ephemeral port so the load test never collides.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer ln.Close() //nolint:errcheck
	_, port, err := net.SplitHostPort(ln.Addr().String())
	assert.NoError(t, err)
	return port
}

// Regression for the class of bug where caddy.Load panicked with
// "interface conversion: interface is nil, not caddy.StorageConverter":
// the generated config (with its storage file_system global) must load on
// whatever toolchain runs the tests. Uses unprivileged ports so it works
// in CI without root.
func TestGeneratedConfigLoadsInCaddy(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	cfg := Config{Entries: []Entry{
		{Alias: "http://load-check.localhost:" + port, Port: 1},
		{Alias: "http://load-check.localhost:" + port + "/api/*", Port: 2},
	}}
	caddyfile := cfg.Caddyfile()
	// The real config binds :80; redirect to the ephemeral port for the test.
	caddyfile = strings.Replace(caddyfile, "{\n\tadmin off", "{\n\thttp_port "+port+"\n\tadmin off", 1)
	adapter := caddyconfig.GetAdapter("caddyfile")
	js, _, err := adapter.Adapt([]byte(caddyfile), map[string]any{"filename": "Caddyfile"})
	assert.NoError(t, err)
	assert.NoError(t, caddy.Load(js, false))
	// Leave a clean global state for other tests.
	assert.NoError(t, caddy.Stop())
}

// Regression: aliases with special characters (wildcards, paths, regexes)
// must round-trip through Save/Open unchanged.
func TestConfigRoundtripsSpecialAliases(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "localias.yaml")
	entries := fullFeatureEntries()
	cfg := &Config{Path: path, Entries: entries}
	assert.NoError(t, cfg.Save())
	cfg2, err := Open(path)
	assert.NoError(t, err)
	check.Equal(t, entries, cfg2.Entries)
}
