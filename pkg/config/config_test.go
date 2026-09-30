package config

import (
	"strings"
	"testing"

	"github.com/peterldowns/testy/assert"
)

var exampleEntries = []Entry{ //nolint:gochecknoglobals
	{Alias: "bare", Port: 9003},
	{Alias: "bare.test", Port: 9002},
	{Alias: "invalid://failure", Port: 9004},
	{Alias: "valid.duplicate", Port: 9000},
	{Alias: "http://insecure.test", Port: 9001},
	{Alias: "https://secure.test", Port: 9000},
}

func TestReadConfig(t *testing.T) {
	t.Parallel()
	cfg, err := Open("./example.roundtrip.yaml")
	assert.NoError(t, err)
	assert.Equal(t, "./example.roundtrip.yaml", cfg.Path)
	assert.Equal(t, exampleEntries, cfg.Entries)
}

func TestWriteConfig(t *testing.T) { //nolint:paralleltest // weird race on the file
	cfg := &Config{
		Path:    "./example.roundtrip.yaml",
		Entries: exampleEntries,
	}
	err := cfg.Save()
	assert.NoError(t, err)
}

func TestConfigRoundtripsPreservingOrder(t *testing.T) { //nolint:paralleltest // weird race on the file
	cfg, err := Open("./example.roundtrip.yaml")
	assert.NoError(t, err)

	err = cfg.Save()
	assert.NoError(t, err)

	cfg2, err := Open(cfg.Path)
	assert.NoError(t, err)
	assert.Equal(t, cfg.Path, cfg2.Path)
	assert.Equal(t, cfg.Entries, cfg2.Entries)
}

func TestUpsertUpdatesExistingEntry(t *testing.T) { //nolint:paralleltest // weird race on the file
	cfg := &Config{
		Path: "./example.upsert.yaml",
	}
	cfg.Set(Entry{
		Alias: "dev.test",
		Port:  8000,
	})
	cfg.Set(Entry{
		Alias: "dev.test",
		Port:  9000,
	})
	expected := []Entry{
		{Alias: "dev.test", Port: 9000},
	}
	assert.Equal(t, expected, cfg.Entries)

	assert.NoError(t, cfg.Save())
	cfg2, err := Open(cfg.Path)
	assert.NoError(t, err)
	assert.Equal(t, expected, cfg2.Entries)
}

func TestDefaultPath(t *testing.T) {
	t.Parallel()
	path, err := Path(nil)
	assert.NoError(t, err)
	assert.NotEqual(t, "", path)
}

func TestImport(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Entries: []Entry{
			{Alias: "a", Port: 1},
			{Alias: "b", Port: 2},
		},
	}
	other := &Config{
		Entries: []Entry{
			{Alias: "b", Port: 3}, // will update the existing entry
			{Alias: "c", Port: 4}, // will be a new addition
		},
	}
	added, updated := cfg.Import(other)
	assert.Equal(t, []Entry{{Alias: "c", Port: 4}}, added)
	assert.Equal(t, []Entry{{Alias: "b", Port: 3}}, updated)
	expected := []Entry{
		{Alias: "a", Port: 1},
		{Alias: "b", Port: 3},
		{Alias: "c", Port: 4},
	}
	assert.Equal(t, expected, cfg.Entries)
}

func TestWildcardEntryHostAndCaddyfile(t *testing.T) {
	t.Parallel()
	entry := Entry{Alias: "*.pelog.localhost", Port: 8787}
	assert.Equal(t, "*.pelog.localhost", entry.Host())
	assert.True(t, entry.IsWildcard())

	caddyfile := entry.Caddyfile()
	assert.True(t, strings.Contains(caddyfile, "*.pelog.localhost {"))
	assert.True(t, strings.Contains(caddyfile, "reverse_proxy localhost:8787"))
	assert.True(t, strings.Contains(caddyfile, "on_demand"))
}

func TestNonWildcardEntryIsNotWildcard(t *testing.T) {
	t.Parallel()
	entry := Entry{Alias: "pelog.localhost", Port: 8787}
	assert.False(t, entry.IsWildcard())
}

func TestValidateAliasAcceptsLeftmostWildcard(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateAlias("*.pelog.localhost"))
	assert.NoError(t, ValidateAlias("https://*.pelog.localhost"))
	assert.NoError(t, ValidateAlias("plain.test"))
}

func TestValidateAliasRejectsInvalidWildcardPlacement(t *testing.T) {
	t.Parallel()
	assert.Error(t, ValidateAlias("foo.*.test"))     // not leftmost
	assert.Error(t, ValidateAlias("foo*.test"))       // partial label
	assert.Error(t, ValidateAlias("*.*.test"))        // more than one wildcard
	assert.Error(t, ValidateAlias("*.pelog.local"))   // mDNS can't serve wildcards
}

func TestPathSplitAccessors(t *testing.T) {
	t.Parallel()
	entry := Entry{Alias: "http://pelog.localhost:3437/api/*", Port: 8787}
	assert.Equal(t, "http://pelog.localhost:3437", entry.Address())
	assert.Equal(t, "/api/*", entry.PathSpec())
	assert.Equal(t, "pelog.localhost", entry.Host())

	plain := Entry{Alias: "pelog.localhost", Port: 3437}
	assert.Equal(t, "pelog.localhost", plain.Address())
	assert.Equal(t, "", plain.PathSpec())

	regex := Entry{Alias: "shorui.localhost/~^/t/[^/]+/(graphql|api/|mcp)", Port: 8000}
	assert.Equal(t, "shorui.localhost", regex.Address())
	assert.Equal(t, "~^/t/[^/]+/(graphql|api/|mcp)", regex.PathSpec())
}

func TestValidateAliasPathSpecs(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateAlias("pelog.localhost/api/*"))
	assert.NoError(t, ValidateAlias("http://pelog.localhost/graphql"))
	assert.NoError(t, ValidateAlias("shorui.localhost/~^/t/[^/]+/(graphql|api/)"))
	assert.Error(t, ValidateAlias("shorui.localhost/~^/t/[unclosed"))
}

func TestCaddyfileGroupsPathRoutesByHost(t *testing.T) {
	t.Parallel()
	cfg := Config{Entries: []Entry{
		{Alias: "pelog.localhost", Port: 3437},
		{Alias: "pelog.localhost/api/*", Port: 8787},
		{Alias: "pelog.localhost/graphql", Port: 8787},
	}}
	out := cfg.Caddyfile()
	// exactly one site block for the shared host
	assert.Equal(t, 1, strings.Count(out, "pelog.localhost {"))
	assert.True(t, strings.Contains(out, "handle /api/* {"))
	assert.True(t, strings.Contains(out, "handle /graphql {"))
	assert.True(t, strings.Contains(out, "handle {"))
	// the fallback handle (no matcher) must come after the path handles
	apiIdx := strings.Index(out, "handle /api/* {")
	fallbackIdx := strings.Index(out, "\thandle {")
	assert.True(t, fallbackIdx > apiIdx)
}

func TestCaddyfileOrdersLongerPathsFirst(t *testing.T) {
	t.Parallel()
	cfg := Config{Entries: []Entry{
		{Alias: "shorui.localhost", Port: 9100},                 // web fallback
		{Alias: "shorui.localhost/api/*", Port: 9200},           // api
		{Alias: "shorui.localhost/api/auth/*", Port: 9300},      // id (must win)
		{Alias: "shorui.localhost/~^/t/[^/]+/api/", Port: 9200}, // tenant regex
	}}
	out := cfg.Caddyfile()
	idIdx := strings.Index(out, "handle /api/auth/* {")
	apiIdx := strings.Index(out, "handle /api/* {")
	assert.True(t, idIdx < apiIdx)
	assert.True(t, strings.Contains(out, "@path0 path_regexp ^/t/[^/]+/api/"))
	assert.True(t, strings.Contains(out, "handle @path0 {"))
}
