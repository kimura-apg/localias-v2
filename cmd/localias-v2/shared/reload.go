package shared

import (
	"fmt"
	"os"

	"github.com/fatih/color"

	"github.com/kimura-apg/localias-v2/pkg/daemon"
)

// ReloadIfRunning restarts the localias daemon so config changes made by
// mutating commands (`set`, `rm`, `clear`, `import`, `dev`) take effect
// immediately. If no daemon is running, one is started — an alias nobody
// serves is indistinguishable from a typo. If the (re)start fails (e.g.
// editing /etc/hosts needs sudo and there is no TTY), it warns instead of
// failing the command — the config on disk is already updated and the next
// successful start will apply it.
func ReloadIfRunning() {
	running, err := daemon.Status()
	if err != nil {
		return
	}
	if err := daemon.Start(Config()); err != nil {
		warn := color.New(color.FgYellow, color.Italic)
		fmt.Fprintln(os.Stderr, warn.Sprintf(
			"warning: config saved but the daemon could not be (re)started (%v); run `localias start` to apply",
			err,
		))
		return
	}
	if running != nil {
		fmt.Println("[daemon reloaded]")
	} else {
		fmt.Println("[daemon started]")
	}
}
