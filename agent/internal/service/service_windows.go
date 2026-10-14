//go:build windows

package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
)

// winHandler implements svc.Handler for the Tolikopa service.
type winHandler struct {
	runner  *Runner
	tracker *collector.SessionTracker
	log     *slog.Logger
}

// Execute reports RUNNING quickly, runs the collector/sender in a goroutine, and
// maintains session state from SESSIONCHANGE notifications.
func (h *winHandler) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptSessionChange

	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := h.runner.Run(ctx); err != nil {
			h.log.Error("runner exited with error", "err", err.Error())
		}
	}()

	// Slow init happens inside runner goroutines; report RUNNING immediately.
	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(8 * time.Second): // stay within SCM timeout
				}
				return false, 0
			case svc.SessionChange:
				// Pass 0: the tracker resolves the current console session live
				// rather than dereferencing the async WTSSESSION_NOTIFICATION.
				h.tracker.HandleSessionChange(c.EventType, 0)
				changes <- c.CurrentStatus
			default:
				h.log.Debug("unexpected service control", "cmd", uint32(c.Cmd))
			}
		case <-done:
			// Runner stopped on its own; report stopped.
			return false, 0
		}
	}
}

// IsService reports whether the process is running under the Windows SCM.
func IsService() (bool, error) { return svc.IsWindowsService() }

// RunService runs under the SCM as the Tolikopa service.
func RunService(cfg *config.Config, log *slog.Logger, version string) error {
	tracker := collector.NewSessionTracker()
	runner := NewRunner(cfg, log, tracker, version)
	return svc.Run(Name, &winHandler{runner: runner, tracker: tracker, log: log})
}

// Install registers the service: delayed-auto-start, LocalSystem, restart-on-failure.
func Install() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	if s, err := m.OpenService(Name); err == nil {
		s.Close()
		return fmt.Errorf("service %s already installed", Name)
	}

	s, err := m.CreateService(Name, exePath, mgr.Config{
		DisplayName:      DisplayName,
		Description:      Description,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
		ServiceStartName: "", // empty => LocalSystem
	})
	if err != nil {
		return err
	}
	defer s.Close()

	// Recovery: restart with increasing backoff; reset the failure count daily.
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, uint32((24 * time.Hour).Seconds())); err != nil {
		return fmt.Errorf("set recovery actions: %w", err)
	}
	return nil
}

// Uninstall stops (best-effort) and deletes the service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %s not installed", Name)
	}
	defer s.Close()

	if status, err := s.Control(svc.Stop); err == nil {
		_ = status
		waitState(s, svc.Stopped, 10*time.Second)
	}
	return s.Delete()
}

// StartService starts the installed service.
func StartService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %s not installed", Name)
	}
	defer s.Close()
	return s.Start()
}

// StopService stops the running service.
func StopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %s not installed", Name)
	}
	defer s.Close()
	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	waitState(s, svc.Stopped, 15*time.Second)
	return nil
}

func waitState(s *mgr.Service, want svc.State, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil || st.State == want {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}
