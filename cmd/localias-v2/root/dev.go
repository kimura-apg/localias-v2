package root

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/kimura-apg/localias-v2/cmd/localias-v2/shared"
	"github.com/kimura-apg/localias-v2/pkg/config"
)

// timeAfter indirection kept tiny; exists so tests could stub it if needed.
var timeAfter = time.After //nolint:gochecknoglobals
var devFlags struct { //nolint:gochecknoglobals
	Timeout *int
}

var devCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "dev <alias> -- <command> [args...]",
	Short: "run a dev server and alias whatever port it listens on",
	Long: strings.TrimSpace(`
Starts <command> as a child process, watches its output until it prints a
listening address (e.g. "localhost:3000", "0.0.0.0:5173", "port 8000"),
registers <alias> -> that port, and keeps running until the child exits.
When the child exits (or you Ctrl-C), the alias is removed again.

The alias defaults to http:// (dev servers are plain HTTP); pass an explicit
scheme (e.g. https://app.test) to override. The running daemon is reloaded
automatically when the alias is added and removed.
`),
	Example: shared.Example(`
# serve a vite/bun dev server at http://sawada.localhost
localias dev sawada.localhost -- bun run dev

# https alias
localias dev https://app.test -- python3 -m http.server
	`),
	Args: cobra.MinimumNArgs(2),
	RunE: devImpl,
}

// Patterns printed by common dev servers when they start listening:
// vite/bun/next print "localhost:5173" or "0.0.0.0:3000",
// python http.server prints "Serving HTTP on 0.0.0.0 port 8000".
var listenPatterns = []*regexp.Regexp{ //nolint:gochecknoglobals
	regexp.MustCompile(`(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1?\]|0:0:0:0:0:0:0:1):(\d{2,5})`),
	regexp.MustCompile(`port (\d{2,5})`),
}

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

	// Run the child with its stdout/stderr teed through a port scanner.
	pr, pw := io.Pipe()
	child := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	child.Stdout = io.MultiWriter(os.Stdout, pw)
	child.Stderr = io.MultiWriter(os.Stderr, pw)
	child.Stdin = os.Stdin
	if err := child.Start(); err != nil {
		return fmt.Errorf("failed to start %v: %w", cmdArgs, err)
	}

	portCh := make(chan int, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			if port := FirstListenPort(scanner.Text()); port > 0 {
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
		fmt.Printf("[dev] %s -> 127.0.0.1:%d\n", alias, port)
		shared.ReloadIfRunning()
	case err := <-waitCh:
		if err != nil {
			return fmt.Errorf("command exited before listening: %w", err)
		}
		return fmt.Errorf("command exited before printing a listening port")
	case <-timeAfter(time.Duration(timeout) * time.Second):
		return fmt.Errorf("timed out after %ds waiting for %v to print a listening port", timeout, cmdArgs)
	}

	// Forward Ctrl-C to the child and wait for it.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range sigCh {
			_ = child.Process.Signal(os.Interrupt)
		}
	}()
	err := <-waitCh
	close(sigCh)

	// Remove the alias again, best-effort.
	if registered {
		cfg := shared.Config()
		cfg.Remove(alias)
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[dev] warning: failed to remove alias: %v\n", err)
		} else {
			fmt.Printf("[dev] removed %s\n", alias)
		}
		shared.ReloadIfRunning()
	}
	pw.Close()
	wg.Wait()
	if err != nil {
		return err
	}
	return nil
}

func init() {
	devFlags.Timeout = devCmd.Flags().Int("timeout", 60, "seconds to wait for the command to print a listening port")
	Command.AddCommand(devCmd)
}
