package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kimura-apg/localias-v2/cmd/localias-v2/shared"
	"github.com/kimura-apg/localias-v2/pkg/config"
)

const daemonLabel = "dev.localias"

var daemonCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "daemon",
	Short: "manage the resident (launchd) proxy service",
	Long: shared.Example(`
Enable or disable the resident proxy service via a macOS LaunchAgent.
When enabled, the proxy starts at login and is automatically restarted
by launchd (KeepAlive) if it ever exits or crashes.

Enabling also stops the one-shot daemon (if any) so the resident
service owns the ports.
	`),
	RunE: daemonStatusImpl,
}

var daemonOnCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "on",
	Short: "install and start the resident proxy service",
	RunE:  daemonOnImpl,
}

var daemonOffCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "off",
	Short: "stop and uninstall the resident proxy service",
	RunE:  daemonOffImpl,
}

var daemonStatusCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "status",
	Short: "show whether the resident service is enabled",
	RunE:  daemonStatusImpl,
}

func daemonPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", daemonLabel+".plist"), nil
}

func launchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func launchctlQuiet(args ...string) bool {
	return exec.Command("launchctl", args...).Run() == nil
}

// daemonLoaded reports whether launchd currently has the service in its
// session (regardless of whether the process is alive).
func daemonLoaded() bool {
	return launchctlQuiet("print", fmt.Sprintf("gui/%d/%s", os.Getuid(), daemonLabel))
}

func daemonOnImpl(_ *cobra.Command, _ []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("daemon on/off is only implemented for macOS (launchd)")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve localias executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("failed to resolve localias executable: %w", err)
	}
	// Resolve the config file now (cwd/XDG-dependent) and pin it in the
	// plist so the service always uses the same config regardless of the
	// environment launchd gives it.
	cfgPath, err := config.Path(shared.Flags.Configfile)
	if err != nil {
		return err
	}
	if cfgPath, err = filepath.Abs(cfgPath); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	plist, err := daemonPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	// The service runs `localias run` in the foreground; launchd keeps it
	// alive. PATH must include the executable's dir and system dirs.
	envPath := strings.Join([]string{
		filepath.Dir(exe),
		home + "/.local/bin",
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/usr/bin:/bin:/usr/sbin:/sbin",
	}, ":")
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>-c</string>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>%s</string>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>/tmp/localias-daemon.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/localias-daemon.log</string>
</dict>
</plist>
`, daemonLabel, exe, cfgPath, home, envPath)
	if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", plist)
	// Reload from scratch: unload any previous incarnation first.
	launchctlQuiet("bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), daemonLabel))
	if err := launchctl("bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist); err != nil {
		return fmt.Errorf("failed to load service: %w", err)
	}
	fmt.Println("resident service enabled (starts at login, auto-restarts on crash)")
	return daemonStatusImpl(nil, nil)
}

func daemonOffImpl(_ *cobra.Command, _ []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("daemon on/off is only implemented for macOS (launchd)")
	}
	plist, err := daemonPlistPath()
	if err != nil {
		return err
	}
	if daemonLoaded() {
		if err := launchctl("bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), daemonLabel)); err != nil {
			return fmt.Errorf("failed to unload service: %w", err)
		}
		fmt.Println("resident service stopped")
	} else {
		fmt.Println("resident service was not loaded")
	}
	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("removed %s\n", plist)
	return nil
}

func daemonStatusImpl(_ *cobra.Command, _ []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("daemon status is only implemented for macOS (launchd)")
	}
	plist, err := daemonPlistPath()
	if err != nil {
		return err
	}
	_, statErr := os.Stat(plist)
	switch {
	case daemonLoaded():
		fmt.Printf("resident service: on (loaded in launchd)\n")
	case statErr == nil:
		fmt.Printf("resident service: installed but not loaded (run: localias daemon on)\n")
	default:
		fmt.Printf("resident service: off\n")
	}
	return nil
}

func init() {
	daemonCmd.AddCommand(daemonOnCmd)
	daemonCmd.AddCommand(daemonOffCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	Command.AddCommand(daemonCmd)
}
