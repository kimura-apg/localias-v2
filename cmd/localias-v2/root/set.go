package root

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/kimura-apg/localias-v2/cmd/localias-v2/shared"
	"github.com/kimura-apg/localias-v2/pkg/config"
)

var setFlags struct { //nolint:gochecknoglobals
	Port  *int
	Alias *string
}

var setCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:     "set",
	Short:   "add or edit an alias",
	Aliases: []string{"add", "upsert", "update", "edit"},
	Example: shared.Example(`
# Add secure aliases (automatically upgrade http:// requests to https://)
## alias https://secure-explicit.test to 127.0.0.1:9001
localias set https://secure-explicit.test 9001
## alias https://secure-implicit.test to 127.0.0.1:9002
localias set secure-implicit.test 9002

# Add insecure aliases (only support http:// requests)
## alias http://not-secure.test to 127.0.0.1:9003
localias set http://not-secure.test 9003

# Add multiple aliases for the same local port
localias set door1.test 9000
localias set door2.test 9000

# Update an existing alias
localias set example.test 9001
localias set example.test 9002

# Alternative forms
localias set example.test 9001
localias set -a example.test -p 9001
localias set --alias example.test --port 9001

# Wildcard alias: route any subdomain of pelog.localhost to the same port
# (quote it so your shell doesn't glob-expand the "*")
localias set '*.pelog.localhost' 8787

# Path-split routing: send some paths of a host to a different port.
# Longest path wins; the entry without a path is the fallback.
localias set pelog.localhost 3437
localias set 'pelog.localhost/api/*' 8787
localias set pelog.localhost/graphql 8787
# Regular-expression paths use a "~" prefix:
localias set 'shorui.localhost/~^/t/[^/]+/(graphql|api/)' 8787
	`),
	RunE: setImpl,
}

func setImpl(_ *cobra.Command, args []string) error {
	alias := *setFlags.Alias
	port := *setFlags.Port

	if port == 0 && alias == "" {
		if len(args) != 2 {
			return fmt.Errorf("invalid arguments: expected [alias] [port]")
		}
		alias = args[0]
		x, err := strconv.ParseInt(args[1], 0, 0)
		if err != nil {
			return fmt.Errorf("valid to parse port: %w", err)
		}
		port = int(x)
	}

	if err := config.ValidateAlias(alias); err != nil {
		return err
	}

	cfg := shared.Config()
	updated := cfg.Set(config.Entry{
		Alias: alias,
		Port:  port,
	})
	if err := cfg.Save(); err != nil {
		return err
	}

	shared.PrintUpdate(config.Entry{
		Alias: alias,
		Port:  port,
	}, updated)
	return nil
}

func init() {
	setFlags.Alias = setCmd.Flags().StringP("alias", "a", "", "domain alias e.g. example.test")
	setFlags.Port = setCmd.Flags().IntP("port", "p", 0, "local port e.g. 9000")
	setCmd.MarkFlagsRequiredTogether("alias", "port")
	Command.AddCommand(setCmd)
}
