//go:build linux

package service

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
)

const (
	unitName = "tolikopa-agent.service"
	unitPath = "/etc/systemd/system/tolikopa-agent.service"
)

// IsService is always false on Linux (systemd runs the `run` subcommand in the
// foreground; there is no SCM equivalent to auto-detect).
func IsService() (bool, error) { return false, nil }

// RunService runs the loop in the foreground; systemd (Type=simple) supervises
// the process and stops it with SIGTERM.
func RunService(cfg *config.Config, log *slog.Logger, version string) error {
	return RunForeground(cfg, log, version)
}

// Install writes and enables the systemd unit. The Ubuntu bootstrap script also
// handles this path; keeping it here makes the agent self-installable.
func Install() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
`, Description, exePath)

	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	return run("systemctl", "enable", "--now", unitName)
}

// Uninstall disables and removes the systemd unit.
func Uninstall() error {
	_ = run("systemctl", "disable", "--now", unitName)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return run("systemctl", "daemon-reload")
}

// StartService starts the systemd unit.
func StartService() error { return run("systemctl", "start", unitName) }

// StopService stops the systemd unit.
func StopService() error { return run("systemctl", "stop", unitName) }

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}
