package root

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/spf13/cobra"

	"github.com/kimura-apg/localias-v2/cmd/localias-v2/shared"
	"github.com/kimura-apg/localias-v2/pkg/config"
)

// timeAfter indirection kept tiny; exists so tests could stub it if needed.
var timeAfter = time.After //nolint:gochecknoglobals
var devFlags struct {      //nolint:gochecknoglobals
	Timeout *int
}

var devCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "dev <alias> -- <command> [args...]",
	Short: "run a dev server and alias whatever port it listens on",
	Long: strings.TrimSpace(`
Starts <command> as a child process under a pseudo-terminal (so it keeps
its colors), watches its output until it prints a listening address
(e.g. "localhost:3000", "http://192.168.1.8:8924", "port 8000"),
registers <alias> -> that port, and keeps running until the child exits.
When the child exits (or you Ctrl-C), the alias is removed again.

The alias defaults to http:// (dev servers are plain HTTP); pass an explicit
scheme (e.g. https://app.test) to override. The daemon is (re)started
automatically when the alias is added and removed.
`),
	Example: shared.Example(`
# serve a vite/bun/nuxt dev server at http://sawada.localhost
localias dev sawada.localhost -- bun run dev

# https alias
localias dev https://app.test -- python3 -m http.server
	`),
	Args: cobra.MinimumNArgs(2),
	RunE: devImpl,
}

// Patterns printed by common dev servers when they start listening:
// vite/bun/next print "localhost:5173" or "0.0.0.0:3000",
// nuxt prints "Listening on: http://192.168.1.8:8924/" (LAN IP),
// python http.server prints "Serving HTTP on 0.0.0.0 port 8000".
var listenPatterns = []*regexp.Regexp{ //nolint:gochecknoglobals
	regexp.MustCompile(`(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1?\]|0:0:0:0:0:0:0:1):(\d{2,5})`),
	regexp.MustCompile(`https?://[^/\s]+:(\d{2,5})`),
	regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}:(\d{2,5})`),
	regexp.MustCompile(`port (\d{2,5})`),
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`) //nolint:gochecknoglobals

// FirstListenPort returns the first port number the given output line
// advertises a listener on, or 0 if the line doesn't match.
func FirstListenPort(line string) int {
	for _, re := range listenPatterns {
		if m := re.FindStringSubmatch(line); m != nil {
			var port int
			if _, err := fmt.Sscanf(m[1], "%d", &port); err == nil && port > 0 {
				return port
			}
		}
	}
	return 0
}

func devImpl(_ *cobra.Command, args []string) error {
	// Split on the first "--" separator: left = alias, right = command.
	// (pflag may strip the separator itself when parsing flags, so also
	// accept the flag-terminator form Args() already split.)
	alias := args[0]
	cmdArgs := args[1:]
	for i, a := range args {
		if a == "--" {
			if i == 0 {
				return fmt.Errorf("invalid arguments: expected <alias> before \"--\" (usage: localias dev <alias> -- <command>)")
			}
			cmdArgs = args[i+1:]
			break
		}
	}
	if len(cmdArgs) == 0 {
		return fmt.Errorf("invalid arguments: expected <alias> -- <command>")
	}
	// A missing alias usually surfaces as the command's name in the alias
	// slot (e.g. "dev -- yarn dev" -> alias "yarn"): aliases always contain
	// a dot (host.tld), so catch that with a targeted hint.
	if !strings.Contains(alias, ".") {
		return fmt.Errorf(
			"invalid alias %q: did you forget the alias before \"--\"? usage: localias dev <alias> -- <command>",
			alias,
		)
	}
	if !strings.Contains(alias, "://") {
		alias = "http://" + alias
	}
	if err := config.ValidateAlias(alias); err != nil {
		return err
	}

	// Run the child under a pseudo-terminal so it sees a TTY and keeps its
	// color output; we read the master side to detect the listening port.
	// (A plain pipe makes isTTY false and dev servers drop their colors.)
	child := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	ptyFile, err := pty.StartWithSize(child, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		return fmt.Errorf("failed to start %v: %w", cmdArgs, err)
	}
	defer ptyFile.Close() //nolint:errcheck

	portCh := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(ptyFile)
		for scanner.Scan() {
			// Pass the child's output through to the user's terminal
			// (raw line — colors intact); strip ANSI only for matching.
			fmt.Fprintln(os.Stdout, scanner.Text())
			line := ansiPattern.ReplaceAllString(scanner.Text(), "")
			// A port mentioned in an "already in use" warning is NOT the
			// port this process is listening on (nuxt e.g. falls back to a
			// random one right after) — never register it.
			if strings.Contains(line, "already in use") {
				continue
			}
			if port := FirstListenPort(line); port > 0 {
				select {
				case portCh <- port:
				default:
				}
			}
		}
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()

	timeout := *devFlags.Timeout
	var registered bool
	fmt.Printf("[dev] waiting for %v to print a listening port (timeout %ds)...\n", cmdArgs, timeout)
	select {
	case port := <-portCh:
		cfg := shared.Config()
		cfg.Set(config.Entry{Alias: alias, Port: port})
		if err := cfg.Save(); err != nil {
			return err
		}
		registered = true
		fmt.Printf("[dev] alias registered: %s -> 127.0.0.1:%d\n", alias, port)
		shared.ReloadIfRunning()
		logReady(alias)
	case err := <-waitCh:
		_ = child.Process.Kill()
		<-waitCh
		if err != nil {
			return fmt.Errorf("command exited before listening: %w", err)
		}
		return fmt.Errorf("command exited before printing a listening port")
	case <-timeAfter(time.Duration(timeout) * time.Second):
		// Kill the child so an orphaned dev server doesn't keep holding
		// the port a retry would want.
		_ = child.Process.Kill()
		<-waitCh
		return fmt.Errorf("timed out after %ds waiting for %v to print a listening port", timeout, cmdArgs)
	}

	// Forward Ctrl-C to the child. Signaling only child.Process would miss the
	// grandchildren (yarn -> node -> nuxt); instead write the INTR control
	// character to the pty master — the line discipline delivers SIGINT to the
	// child's whole foreground process group, exactly like a real terminal —
	// and escalate to SIGKILL on the group if it does not exit in time.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	stopDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-sigCh:
				fmt.Println("[dev] interrupt; stopping child...")
				_, _ = ptyFile.Write([]byte{0x03})
				go func() {
					select {
					case <-time.After(5 * time.Second):
						fmt.Println("[dev] child did not exit; killing process group")
						_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
					case <-stopDone:
					}
				}()
			case <-stopDone:
				return
			}
		}
	}()
	err = <-waitCh
	close(stopDone)
	close(sigCh)
	// Best-effort close of the pty master; the scanner goroutine dies with
	// the process (close does not unblock a blocked read on macOS).
	_ = ptyFile.Close()

	// Remove the alias again, best-effort.
	if registered {
		cfg := shared.Config()
		cfg.Remove(alias)
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[dev] warning: failed to remove alias: %v\n", err)
		} else {
			fmt.Printf("[dev] alias removed: %s\n", alias)
		}
		shared.ReloadIfRunning()
	}
	if err != nil {
		return err
	}
	return nil
}

// logReady probes the alias through the local proxy so the user gets a
// definitive "it works now" line (or an early warning that routing or the
// upstream is not answering yet).
func logReady(alias string) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, alias, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[dev] warning: could not reach %s yet (%v)\n", alias, err)
		return
	}
	resp.Body.Close() //nolint:errcheck
	fmt.Printf("[dev] ready: %s (upstream answered %s)\n", alias, resp.Status)
}

func init() {
	devFlags.Timeout = devCmd.Flags().Int("timeout", 60, "seconds to wait for the command to print a listening port")
	Command.AddCommand(devCmd)
}
