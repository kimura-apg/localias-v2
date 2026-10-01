package shared

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/fatih/color"

	"github.com/kimura-apg/localias-v2/pkg/daemon"
)

// Quiet suppresses the informational prints (e.g. "[daemon reloaded]")
// while another renderer owns the terminal — the `dev` TUI sets it, since
// plain prints corrupt the alt-screen layout.
var Quiet bool //nolint:gochecknoglobals

// ReloadIfRunning restarts the localias daemon so config changes made by
// mutating commands (`set`, `rm`, `clear`, `import`, `dev`) take effect
// immediately. If no daemon is running, one is started — an alias nobody
// serves is indistinguishable from a typo. If the (re)start fails (e.g.
// editing /etc/hosts needs sudo and there is no TTY), it warns instead of
// failing the command — the config on disk is already updated and the next
// successful start will apply it.
func ReloadIfRunning() string {
	running, err := daemon.Status()

	if err != nil {
		return ""
	}
	// Restart via a subprocess. Calling daemon.Start in-process is
	// dangerous for arbitrary commands: go-daemon re-execs THIS binary
	// with the SAME arguments, so e.g. `localias dev -- yarn dev` would
	// fork a daemon that starts re-running the wrapped dev server.
	// `localias start` in a child process keeps the re-exec harmless.
	args := []string{"start"}
	if Flags.Configfile != nil && *Flags.Configfile != "" {
		args = append(args, "--configfile", *Flags.Configfile)
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		warn := color.New(color.FgYellow, color.Italic)
		fmt.Fprintln(os.Stderr, warn.Sprintf(
			"warning: config saved but the daemon could not be (re)started (%v); run `localias start` to apply",
			err,
		))
		return ""
	}

	if running != nil {
		if !Quiet {
			fmt.Println("[daemon reloaded]")
		}
		return "daemon reloaded"
	}
	if !Quiet {
		fmt.Println("[daemon started]")
	}
	return "daemon started"
}
