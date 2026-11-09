package main

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

var allSubcommands = []string{
	"version", "admin", "install", "uninstall", "login", "logout", "hook", "agent", "status", "logs", "ls", "pull", "export", "rename",
}

// login, logout (CO-103), hook (CO-105), install/uninstall (CO-106),
// status/logs (CO-113), ls (CO-204), pull/export (CO-211), and rename
// (CO-405, the last ticket with a stub — stub.go and its
// notImplementedCmd helper were removed with it, golangci-lint's
// unused check having nothing left to scaffold for) are implemented;
// agent (CO-107) is a special case — it was never a stub, since
// CO-106 installs and starts it as a real OS service, so it can't
// exit immediately. No subcommand is currently a stub —
// TestExitCodeForErr below covers errNotImplemented's mapping to exit
// code 2 directly instead.
var stubSubcommands = []string{}

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

// run()'s mapping from errNotImplemented to exit code 2 has no live
// subcommand to exercise it through now that none is a stub (see
// stubSubcommands' own comment) — exitCodeForErr is split out of run()
// precisely so this mapping stays directly testable regardless.
func TestExitCodeForErr(t *testing.T) {
	if got := exitCodeForErr(errNotImplemented); got != 2 {
		t.Errorf("exitCodeForErr(errNotImplemented) = %d, want 2", got)
	}
	if got := exitCodeForErr(fmt.Errorf("wrapped: %w", errNotImplemented)); got != 2 {
		t.Errorf("exitCodeForErr(wrapped errNotImplemented) = %d, want 2", got)
	}
	if got := exitCodeForErr(errors.New("some other failure")); got != 1 {
		t.Errorf("exitCodeForErr(other) = %d, want 1", got)
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

	// An unrecognized subcommand is cobra's own error, not
	// errNotImplemented — exitCodeForErr's 2-vs-1 split is covered
	// directly by TestExitCodeForErr instead.
	os.Args = []string{"carryon", "not-a-real-subcommand"}
	if code := run(); code != 1 {
		t.Errorf("run() with an unknown subcommand = %d, want 1", code)
	}

	os.Args = []string{"carryon", "version"}
	if code := run(); code != 0 {
		t.Errorf("run() with version = %d, want 0", code)
	}
}
