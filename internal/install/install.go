package install

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/service"
)

// Install merges Carryon's hooks into ~/.claude/settings.json, creates
// ~/.carryon (mode 0700, via config.Dir), and installs and starts the
// background capture agent service (CO-107's eventual daemon). Safe to run
// more than once.
func Install() error {
	settingsPath, err := claudeSettingsPath()
	if err != nil {
		return err
	}
	statePath, err := installStatePath()
	if err != nil {
		return err
	}

	if err := InstallSettings(settingsPath, statePath); err != nil {
		return err
	}

	if err := service.Install(); err != nil {
		return err
	}
	return nil
}

// Uninstall reverses Install's settings.json merge and removes the
// background service.
//
// It deliberately does NOT remove ~/.carryon itself: that directory holds
// config.toml (the configured backend), stored login credentials, and any
// queued-but-not-yet-uploaded session data (internal/hook's queue.jsonl) —
// removing the hooks and the service shouldn't destroy that.
func Uninstall() error {
	settingsPath, err := claudeSettingsPath()
	if err != nil {
		return err
	}
	statePath, err := installStatePath()
	if err != nil {
		return err
	}

	if err := UninstallSettings(settingsPath, statePath); err != nil {
		return err
	}

	return service.Uninstall()
}

func claudeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func installStatePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "install-state.json"), nil
}
