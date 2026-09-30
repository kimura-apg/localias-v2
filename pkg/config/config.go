package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adrg/xdg"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/go-yaml/yaml"
)

type Config struct {
	Path    string
	Entries []Entry
}

type Entry struct {
	Alias string
	Port  int
}

// Set will add or update the existing list of entries.  If there is
// already a entry with the same upstream/alias, its downstream/port will be
// updated. If there is not already a entry with the same upstream/alias,
// the new entry will be added to the list.
//
// Returns `true` if an existing entry was updated, `false` if the entry
// was added.
func (c *Config) Set(d Entry) bool {
	for i, existing := range c.Entries {
		if existing.Alias == d.Alias {
			c.Entries[i] = d
			return true
		}
	}
	c.Entries = append(c.Entries, d)
	return false
}

// Import merges the entries from another config into the current one.
// It returns the entries that were added and updated.
func (c *Config) Import(other *Config) (added []Entry, updated []Entry) {
	for _, entry := range other.Entries {
		if c.Set(entry) {
			updated = append(updated, entry)
		} else {
			added = append(added, entry)
		}
	}
	return added, updated
}

// Remove removes all entries from the config that match
// any of the specified aliases.
func (c *Config) Remove(aliases ...string) []Entry {
	var removed []Entry
	previous := c.Entries
	c.Clear()
	for _, d := range previous {
		shouldRemove := false
		for _, alias := range aliases {
			if d.Alias == alias {
				shouldRemove = true
				break
			}
		}
		if shouldRemove {
			removed = append(removed, d)
		} else {
			c.Set(d)
		}
	}
	return removed
}

// Clear removes all entries from the config.
func (c *Config) Clear() []Entry {
	removed := c.Entries
	c.Entries = []Entry{}
	return removed
}

// Save writes the config to disk.
func (c *Config) Save() error {
	entries := yaml.MapSlice{}
	for _, d := range c.Entries {
		entries = append(entries, yaml.MapItem{
			Key:   d.Alias,
			Value: d.Port,
		})
	}
	bytes := []byte(strings.TrimSpace(`
# localias config file syntax
#
# 	alias: port
#
# for example,
#
#   bareTLD: 9003 # serves over https and http
#   implicitly_secure.test: 9002 # serves over https and http
#   https://explicit_secure.test: 9000 # serves over https and http
#   http://explicit_insecure.test: 9001 # serves over http only
#
	`) + "\n")
	if len(entries) != 0 {
		entryBytes, err := yaml.Marshal(entries)
		if err != nil {
			return err
		}
		bytes = append(bytes, entryBytes...)
	}
	return os.WriteFile(c.Path, bytes, 0o644)
}

func (Config) CaddyStatePath() string {
	path, err := xdg.StateFile("localias/caddy")
	if err != nil {
		panic(err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	return path
}

func (c Config) Caddyfile() string {
	path := c.CaddyStatePath()
	global := fmt.Sprintf(strings.TrimSpace(`
{
	admin off
	persist_config off
	local_certs
	ocsp_stapling off
	storage file_system "%s"

	pki {
		ca local {
			name "Localias"
			root_cn "Localias Root"
			intermediate_cn "Localias Intermediate"
		}
	}

}
`), path)
	blocks := []string{global}
	for _, group := range groupByAddress(c.Entries) {
		blocks = append(blocks, siteBlock(group))
	}
	// extra newline prevents "caddy fmt" warning in logs
	return strings.Join(blocks, "\n") + "\n"
}

func (c Config) CaddyJSON() ([]byte, []caddyconfig.Warning, error) {
	caddyfile := c.Caddyfile()
	cfgAdapter := caddyconfig.GetAdapter("caddyfile")
	if cfgAdapter == nil {
		return nil, nil, fmt.Errorf("failed to load caddyfile adapater")
	}
	cfgJSON, warnings, err := cfgAdapter.Adapt([]byte(caddyfile), map[string]any{
		"filename": "Caddyfile",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse configuration: %w", err)
	}
	return cfgJSON, warnings, nil
}

func (entry Entry) String() string {
	return fmt.Sprintf("%s: %d", entry.Alias, entry.Port)
}

// splitAlias splits an alias into its address part (scheme://host[:port])
// and optional path spec (everything from the first "/" after the host).
// The path spec is either a Caddy-style glob path ("/api/*", "/graphql")
// or, when it starts with "~", a regular expression applied to the request
// path ("~^/t/[^/]+/(graphql|api/)").
func splitAlias(alias string) (address, pathSpec string) {
	rest := alias
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		spec := rest[i:]
		// A regular-expression path is written "/~/pattern"; normalize it
		// to "~/pattern" so PathSpec() always starts with either "/" (glob)
		// or "~" (regex).
		if strings.HasPrefix(spec, "/~") {
			spec = spec[1:]
		}
		return alias[:len(alias)-len(rest)+i], spec
	}
	return alias, ""
}

// Address returns the alias without any path spec, e.g.
// "http://pelog.localhost:3437/api/*" -> "http://pelog.localhost:3437".
func (entry Entry) Address() string {
	address, _ := splitAlias(entry.Alias)
	return address
}

// PathSpec returns the path-matching part of the alias ("" if the entry is
// a whole-host/fallback route), e.g. "pelog.localhost/api/*" -> "/api/*",
// "pelog.localhost/~^/x" -> "~^/x".
func (entry Entry) PathSpec() string {
	_, pathSpec := splitAlias(entry.Alias)
	return pathSpec
}

// Host returns just the hostname of the alias, without scheme, port, or
// path spec.
func (entry Entry) Host() string {
	a, _ := httpcaddyfile.ParseAddress(entry.Address())
	return a.Host
}

// IsWildcard reports whether the entry's host has a wildcard label, e.g.
// "*.example.localhost". Caddy only supports a wildcard replacing exactly
// one whole label (see ValidateAlias), so a simple substring check is
// sufficient here.
func (entry Entry) IsWildcard() bool {
	return strings.Contains(entry.Host(), "*")
}

// ValidateAlias checks that an alias is syntactically valid for use with
// Caddy, and if it contains a wildcard label enforces the constraints that
// make the wildcard usable end-to-end by the rest of Localias:
//
//   - Caddy itself only supports a wildcard that replaces exactly one whole
//     label, and only as the leftmost label of the host
//     (https://caddyserver.com/docs/caddyfile/concepts).
//   - A wildcard alias cannot be served over mDNS (".local"), because mDNS
//     records are literal hostnames, not patterns.
func ValidateAlias(alias string) error {
	address, pathSpec := splitAlias(alias)
	a, err := httpcaddyfile.ParseAddress(address)
	if err != nil {
		return fmt.Errorf("invalid alias %q: %w", alias, err)
	}
	labels := strings.Split(a.Host, ".")
	wildcards := 0
	for i, label := range labels {
		if !strings.Contains(label, "*") {
			continue
		}
		if label != "*" {
			return fmt.Errorf("invalid alias %q: a wildcard must occupy an entire label (e.g. \"*.example.localhost\"), got label %q", alias, label)
		}
		if i != 0 {
			return fmt.Errorf("invalid alias %q: a wildcard must be the leftmost label", alias)
		}
		wildcards++
	}
	if wildcards > 1 {
		return fmt.Errorf("invalid alias %q: only one wildcard label is supported", alias)
	}
	if wildcards == 1 && strings.HasSuffix(a.Host, ".local") {
		return fmt.Errorf("invalid alias %q: wildcard aliases are not supported under .local (mDNS cannot serve wildcard records)", alias)
	}
	if strings.HasPrefix(pathSpec, "~") {
		if _, err := regexp.Compile(strings.TrimPrefix(pathSpec, "~")); err != nil {
			return fmt.Errorf("invalid alias %q: invalid path regular expression: %w", alias, err)
		}
	} else if pathSpec != "" && !strings.HasPrefix(pathSpec, "/") {
		return fmt.Errorf("invalid alias %q: path spec must start with \"/\" or \"~/\" for a regular expression", alias)
	}
	return nil
}

// groupByAddress groups entries that share the same address (scheme, host,
// and port) so they can be rendered as one Caddy site block whose request
// paths are split across multiple upstreams. Group order and entry order
// within a group follow the config file order.
func groupByAddress(entries []Entry) [][]Entry {
	var order []string
	groups := make(map[string][]Entry)
	for _, entry := range entries {
		key := entry.Address()
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], entry)
	}
	var result [][]Entry
	for _, key := range order {
		result = append(result, groups[key])
	}
	return result
}

// siteBlock renders one Caddy site block for a group of entries that share
// the same address. The entry without a path spec (if present) is the
// fallback and is emitted last; path routes are emitted before it, sorted
// by descending path-spec length so that more specific routes (e.g.
// "/api/auth/*") win over shorter prefixes on the same path tree (e.g.
// "/api/*"), mirroring Traefik's rule-priority behavior.
func siteBlock(group []Entry) string {
	address := group[0].Address()
	var fallback *Entry
	var routes []Entry
	for i, entry := range group {
		if entry.PathSpec() == "" {
			fallback = &group[i]
		} else {
			routes = append(routes, entry)
		}
	}
	sort.SliceStable(routes, func(i, j int) bool {
		pi, pj := routes[i].PathSpec(), routes[j].PathSpec()
		if len(pi) != len(pj) {
			return len(pi) > len(pj)
		}
		return pi < pj
	})

	var body strings.Builder
	matcher := 0
	for _, route := range routes {
		spec := route.PathSpec()
		if strings.HasPrefix(spec, "~") {
			// Named matcher for a regular-expression path route.
			fmt.Fprintf(&body, "\t@path%d path_regexp %s\n", matcher, strings.TrimPrefix(spec, "~"))
			fmt.Fprintf(&body, "\thandle @path%d {\n\t\treverse_proxy localhost:%d\n\t}\n", matcher, route.Port)
			matcher++
			continue
		}
		fmt.Fprintf(&body, "\thandle %s {\n\t\treverse_proxy localhost:%d\n\t}\n", spec, route.Port)
	}
	if fallback != nil {
		fmt.Fprintf(&body, "\thandle {\n\t\treverse_proxy localhost:%d\n\t}\n", fallback.Port)
	}

	tls := "# tls disabled"
	a, _ := httpcaddyfile.ParseAddress(address)
	// If no scheme is given, default to https.
	if a.Scheme == "" {
		a.Scheme = "https"
	}
	if a.Scheme == "https" {
		tls = strings.TrimSpace(`
	tls {
		on_demand
		issuer internal {
			ca local
		}
	}
`)
	}
	return fmt.Sprintf(strings.TrimSpace(`
%s {
%s	%s
}
	`), address, body.String(), tls)
}

// Caddyfile renders this single entry as a site block. Entries with a path
// spec only make sense as part of a group; rendering one on its own still
// works (it routes matching paths and 404s the rest).
func (entry Entry) Caddyfile() string {
	return siteBlock([]Entry{entry})
}
