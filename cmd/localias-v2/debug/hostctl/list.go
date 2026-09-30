package hostctl

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kimura-apg/localias-v2/cmd/localias-v2/shared"
)

func listImpl(_ *cobra.Command, _ []string) error {
	hctl := shared.Controller()
	entries, err := hctl.List()
	if err != nil {
		return err
	}
	for path, lines := range entries {
		fmt.Println(path)
		for _, line := range lines {
			fmt.Println("\t", line)
		}
	}
	return nil
}

var listCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:     "list",
	Aliases: []string{"print", "show"},
	Short:   "show any entries that localias has made to the host file(s)",
	RunE:    listImpl,
}

func init() {
	Command.AddCommand(listCmd)
}
