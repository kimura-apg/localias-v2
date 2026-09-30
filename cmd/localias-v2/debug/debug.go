package debug

import (
	"github.com/spf13/cobra"

	"github.com/kimura-apg/localias-v2/cmd/localias-v2/debug/hostctl"
	"github.com/kimura-apg/localias-v2/cmd/localias-v2/shared"
)

var Command = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "debug",
	Short: "various helpers for debugging localias",
	Example: shared.Example(`
# show the path to the current configuration file
localias debug config
# print the contents of the current configuration file
localias debug config --print
# show the path to the root certificate
localias debug cert
# print the contents of the root certificate
localias debug cert --print
	`),
	Hidden: true,
}

func init() {
	Command.AddCommand(hostctl.Command)
}
