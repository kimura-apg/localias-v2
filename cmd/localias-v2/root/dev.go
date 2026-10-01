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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

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

// parseDevArgs validates and normalizes the arguments of `localias dev`:
// alias (defaults to http://) before the first "--", command after it.
// Extracted from devImpl so the argument contract is unit-testable.
func parseDevArgs(args []string) (alias string, cmdArgs []string, err error) {
	if len(args) < 2 {
		return "", nil, fmt.Errorf("invalid arguments: expected <alias> -- <command>")
	}
	alias = args[0]
	cmdArgs = args[1:]
	for i, a := range args {
		if a == "--" {
			if i == 0 {
				return "", nil, fmt.Errorf("invalid arguments: expected <alias> before \"--\" (usage: localias dev <alias> -- <command>)")
			}
			cmdArgs = args[i+1:]
			break
		}
	}
	if len(cmdArgs) == 0 {
		return "", nil, fmt.Errorf("invalid arguments: expected <alias> -- <command>")
	}
	// A missing alias usually surfaces as the command's name in the alias
	// slot (e.g. "dev -- yarn dev" -> alias "yarn"): aliases always contain
	// a dot (host.tld), so catch that with a targeted hint.
	if !strings.Contains(alias, ".") {
		return "", nil, fmt.Errorf(
			"invalid alias %q: did you forget the alias before \"--\"? usage: localias dev <alias> -- <command>",
			alias,
		)
	}
	if !strings.Contains(alias, "://") {
		alias = "http://" + alias
	}
	if err := config.ValidateAlias(alias); err != nil {
		return "", nil, err
	}
	return alias, cmdArgs, nil
}

// listenPortFromLine decides whether an output line of the wrapped command
// advertises the port we should register: ANSI escapes are stripped, and
// lines mentioning "already in use" are ignored — the port they mention is
// NOT the one this process ended up listening on (nuxt e.g. falls back to a
// random one right after such a warning).
func listenPortFromLine(raw string) int {
	line := ansiPattern.ReplaceAllString(raw, "")
	if strings.Contains(line, "already in use") {
		return 0
	}
	return FirstListenPort(line)
}

func devImpl(_ *cobra.Command, args []string) error {
	alias, cmdArgs, err := parseDevArgs(args)
	if err != nil {
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
	interactive := isTerminal(os.Stdout)
	if interactive {
		defer func() { shared.Quiet = false }()
	}
	// Lines feed either the TUI (via program.Send) or plain stdout.
	var program *tea.Program
	quitReq := make(chan struct{})
	if interactive {
		shared.Quiet = true
		program = tea.NewProgram(newDevModel(alias, cmdArgs, &quitReq),
			tea.WithAltScreen())
		go func() { _, _ = program.Run() }() //nolint:errcheck
	}

	go func() {
		// Feed raw bytes to the TUI's terminal emulator (or pass them
		// straight through in non-interactive mode) and mirror them into
		// a detection buffer whose recent lines are scanned for the
		// listening port.
		detect := newTermBuffer()
		reader := bufio.NewReader(ptyFile)
		buf := make([]byte, 8192)
		for {
			n, rerr := reader.Read(buf)
			if n > 0 {
				raw := make([]byte, n)
				copy(raw, buf[:n])
				if program != nil {
					program.Send(ptyBytesMsg(raw))
				} else {
					_, _ = os.Stdout.Write(raw)
				}
				detect.Write(raw)
				for _, tl := range detect.TailLines(8) {
					if port := listenPortFromLine(tl); port > 0 {
						select {
						case portCh <- port:
						default:
						}
					}
				}
			}
			if rerr != nil {
				break // EOF
			}
		}
	}()

	waitCh := make(chan error, 1)
	go func() {
		err := child.Wait()
		if program != nil {
			program.Send(childExitMsg{err: err})
		}
		waitCh <- err
	}()

	timeout := *devFlags.Timeout
	var registered bool
	if !interactive {
		fmt.Printf("[dev] waiting for %v to print a listening port (timeout %ds)...\n", cmdArgs, timeout)
	}
	select {
	case port := <-portCh:
		cfg := shared.Config()
		cfg.Set(config.Entry{Alias: alias, Port: port})
		if err := cfg.Save(); err != nil {
			return err
		}
		registered = true
		detail := probeAlias(alias)
		if program != nil {
			program.Send(statusMsg{port: port, detail: detail})
		} else {
			fmt.Printf("[dev] alias registered: %s -> 127.0.0.1:%d\n", alias, port)
		}
		if note := shared.ReloadIfRunning(); note != "" && program != nil {
			program.Send(noteMsg(note))
		}
		if !interactive {
			fmt.Printf("[dev] ready: %s (%s)\n", alias, detail)
		}
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

	// Wait until the user quits the TUI (or the child exits on its own /
	// a signal arrives). In non-interactive mode this returns as soon as
	// the child exits.
	shutdown := func() {
		// Deliver Ctrl-C on the child's pty so its whole process tree
		// gets SIGINT, exactly like a real terminal, with SIGKILL
		// escalation on the group after a grace period.
		_, _ = ptyFile.Write([]byte{0x03})
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			<-waitCh
		}
	}
	if interactive {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			select {
			case <-sigCh:
				shutdown()
				program.Send(childExitMsg{})
			case <-quitReq: // user pressed q / ctrl+c in the TUI
				shutdown()
			case <-waitCh: // child died on its own
			}
			close(done)
		}()
		<-done
		signal.Stop(sigCh)
		if program != nil {
			program.Quit()
		_programWait:
			for {
				select {
				case <-waitCh:
					break _programWait
				case <-time.After(2 * time.Second):
					break _programWait
				}
			}
		}
	} else {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			for range sigCh {
				shutdown()
			}
		}()
		<-waitCh
		signal.Stop(sigCh)
	}

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
	return nil
}

// isTerminal reports whether the given file is attached to a terminal.
func isTerminal(f *os.File) bool {
	_, err := f.Stat()
	if err != nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// probeAlias does a best-effort GET against the alias through the local
// proxy and describes the outcome for status displays.
func probeAlias(alias string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, alias, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("not reachable yet: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	return fmt.Sprintf("ready: upstream answered %s", resp.Status)
}

func init() {
	devFlags.Timeout = devCmd.Flags().Int("timeout", 60, "seconds to wait for the command to print a listening port")
	Command.AddCommand(devCmd)
}
