package main

import (
	"errors"
	"os"
	"testing"
)

var allSubcommands = []string{
	"version", "admin", "install", "uninstall", "login", "logout", "hook", "agent", "status", "logs", "ls", "pull", "export", "rename",
}

// login, logout (CO-103), hook (CO-105), install/uninstall (CO-106),
// status/logs (CO-113), ls (CO-204), and pull/export (CO-211) are
// implemented; everything else still scaffolds the CLI surface for a
// later ticket. agent (CO-107) is a special case — not a
// notImplementedCmd stub (see agent.go) because CO-106 installs and
// starts it as a real OS service, so it can't exit immediately.
var stubSubcommands = []string{
	"rename",
}

func TestRootCommandListsAllSubcommands(t *testing.T) {
	root := newRootCmd()
	for _, name := range allSubcommands {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name {
			t.Errorf("expected subcommand %q to be registered, got err=%v", name, err)
		}
	}
}

func TestStubCommandsReturnNotImplemented(t *testing.T) {
	root := newRootCmd()
	for _, name := range stubSubcommands {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if cmd.RunE == nil {
			t.Fatalf("%q has no RunE", name)
		}
		if err := cmd.RunE(cmd, nil); !errors.Is(err, errNotImplemented) {
			t.Errorf("%q RunE = %v, want errNotImplemented", name, err)
		}
	}
}

func TestVersionCommandRuns(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"version"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("version RunE returned error: %v", err)
	}
}

func TestRunExitCode(t *testing.T) {
	// run() reads os.Args directly (so main() needs no plumbing); swap it
	// for the duration of this test rather than changing run()'s signature.
	orig := os.Args
	defer func() { os.Args = orig }()

	os.Args = []string{"carryon", "rename"}
	if code := run(); code != 2 {
		t.Errorf("run() with a stub subcommand = %d, want 2", code)
	}

	os.Args = []string{"carryon", "version"}
	if code := run(); code != 0 {
		t.Errorf("run() with version = %d, want 0", code)
	}
}
