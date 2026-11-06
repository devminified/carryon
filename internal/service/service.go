// Package service wraps kardianos/service for CO-106's control operations
// (Install/Uninstall/Start/Stop/Status). This is launchd on macOS, a
// systemd user unit on Linux, and a Windows Service — the same package
// CO-107's `carryon agent` daemon loop will run inside of.
//
// New's returned Service is for control only: its own service.Interface
// implementation is a no-op. The OS service manager calls Start/Stop on
// whatever service.Interface `carryon agent` registers when *it* calls
// service.Run() (CO-107) — not on anything in this package.
package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devminified/carryon/internal/config"
	kservice "github.com/kardianos/service"
)

// Name identifies the installed service to the OS (the launchd label, the
// systemd unit name, the Windows Service name).
const Name = "com.devminified.carryon"

type noopInterface struct{}

func (noopInterface) Start(kservice.Service) error { return nil }
func (noopInterface) Stop(kservice.Service) error  { return nil }

// New returns a service descriptor for the Carryon capture agent, pointed
// at this binary's own path (so Install works wherever `go install` or the
// release puts it) with "agent" as its sole argument.
func New() (kservice.Service, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding carryon's own executable path: %w", err)
	}

	carryonDir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	logDir := filepath.Join(carryonDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", logDir, err)
	}

	cfg := &kservice.Config{
		Name:        Name,
		DisplayName: "Carryon Capture Agent",
		Description: "Captures and uploads your Claude Code sessions so teammates can pull and resume them.",
		Executable:  exe,
		Arguments:   []string{"agent"},
		Option: kservice.KeyValue{
			// A per-user launchd agent / systemd --user unit, not a
			// root-owned system service — matches go-conventions.md's
			// "launchd on macOS, a systemd user unit on Linux" and means
			// `carryon install` never needs sudo. Windows ignores this
			// option (kardianos always installs Windows Services via the
			// SCM, which does need an elevated prompt there).
			"UserService": true,
			// Starts on login, not just on the next reboot — CO-107's
			// "agent starts on boot and login" validation point.
			"RunAtLoad": true,
			// Without this, kardianos defaults a user service's logs to
			// the bare home directory (~/com.devminified.carryon.out.log)
			// — found via a real install on a dev machine, not a guess.
			"LogDirectory": logDir,
		},
	}

	s, err := kservice.New(noopInterface{}, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating service descriptor: %w", err)
	}
	return s, nil
}

// Install registers the service with the OS and starts it. Safe to call
// when already installed and/or already running (CO-106: idempotent).
func Install() error {
	s, err := New()
	if err != nil {
		return err
	}

	if _, statusErr := s.Status(); statusErr != nil {
		// Status errors when the service isn't registered with the OS yet
		// — this is the "not installed" case kardianos/service doesn't
		// expose as a stable sentinel error across platforms, so treat any
		// Status error as "needs installing" rather than string-matching
		// Install's own platform-specific error text.
		if err := s.Install(); err != nil {
			return fmt.Errorf("installing service: %w", err)
		}
	}

	status, err := s.Status()
	if err == nil && status == kservice.StatusRunning {
		return nil
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}
	return nil
}

// Uninstall stops and removes the service. Safe to call when not
// installed (CO-106: `carryon uninstall` reverses install exactly once,
// then becomes a no-op).
func Uninstall() error {
	s, err := New()
	if err != nil {
		return err
	}

	if _, statusErr := s.Status(); statusErr != nil {
		return nil // not installed; nothing to reverse
	}

	_ = s.Stop() // best-effort; fine if already stopped
	if err := s.Uninstall(); err != nil {
		return fmt.Errorf("uninstalling service: %w", err)
	}
	return nil
}
