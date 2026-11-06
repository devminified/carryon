package main

import "testing"

// install and uninstall's RunE calls internal/service, which registers a
// real OS-level service (launchd/systemd/Windows Service). That's
// deliberately not exercised here — automated CI shouldn't mutate the
// runner's real service manager. internal/install's own test suite covers
// the settings.json merge/backup/restore logic without touching it;
// service install/start/stop/uninstall is verified manually (see CO-106's
// PR description).

func TestInstallCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"install"})
	if err != nil {
		t.Fatalf("find install: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("install has no RunE")
	}
}

func TestUninstallCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"uninstall"})
	if err != nil {
		t.Fatalf("find uninstall: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("uninstall has no RunE")
	}
}
