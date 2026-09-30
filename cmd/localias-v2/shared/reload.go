package shared

import (
	"fmt"
	"os"

	"github.com/fatih/color"

	"github.com/kimura-apg/localias-v2/pkg/daemon"
)

// ReloadIfRunning restarts the localias daemon so config changes made by
// mutating commands (`set`, `rm`, `clear`, `import`, `dev`) take effect
// immediately. If no daemon is running, this is a no-op; if the restart
// fails (e.g. editing /etc/hosts needs sudo and there is no TTY), it warns
// instead of failing the command — the config on disk is already updated
// and the next successful start will apply it.
func ReloadIfRunning() {
	proc, err := daemon.Status()
	if err != nil || proc == nil {
		return
	}
	if err := daemon.Start(Config()); err != nil {
		warn := color.New(color.FgYellow, color.Italic)
		fmt.Fprintln(os.Stderr, warn.Sprintf(
			"warning: config saved but the running daemon could not be reloaded (%v); run `localias start` to apply",
			err,
		))
		return
	}
	fmt.Println("[daemon reloaded]")
}
