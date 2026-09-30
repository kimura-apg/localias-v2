package config

import (
	"fmt"
	"os"

	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/fatih/color"

	"github.com/peterldowns/localias/pkg/hostctl"
)

func Apply(hctl hostctl.Controller, cfg *Config) error {
	if err := hctl.Clear(); err != nil {
		return err
	}
	for _, entry := range cfg.Entries {
		up, err := httpcaddyfile.ParseAddress(entry.Alias)
		if err != nil {
			return err
		}
		// /etc/hosts has no concept of a wildcard: a literal line like
		// "127.0.0.1 *.example.localhost" never matches any real request
		// and would only give a false sense that the alias is wired up.
		// Wildcard aliases work anyway under ".localhost" because the OS
		// and browsers already resolve any "*.localhost" name to 127.0.0.1
		// on their own (RFC 6761), so skip writing (and warn if the alias
		// is on a TLD that has no such guarantee, since it needs a real
		// wildcard DNS resolver that Localias does not provide).
		if entry.IsWildcard() {
			warn := color.New(color.FgYellow, color.Italic)
			fmt.Fprintln(os.Stderr, warn.Sprintf(
				"skipping /etc/hosts for wildcard alias %s (relies on the OS resolving \"*.localhost\" on its own; other TLDs need your own wildcard DNS setup)",
				entry.Alias,
			))
			continue
		}
		if err := hctl.SetLocal(up.Host); err != nil {
			return err
		}
	}
	_, err := hctl.Apply()
	return err
}
