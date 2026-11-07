package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLsCommandHasEveryFlag(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"ls"})
	if err != nil {
		t.Fatalf("find ls: %v", err)
	}
	for _, name := range []string{"project", "dev", "status", "since", "limit", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("ls command has no --%s flag", name)
		}
	}
}

func TestLsAcceptsAnOptionalSessionIDArgument(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"ls"})
	if err != nil {
		t.Fatalf("find ls: %v", err)
	}
	if err := cmd.Args(cmd, []string{"one-id"}); err != nil {
		t.Errorf("ls with one positional arg should be accepted, got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"one-id", "two-id"}); err == nil {
		t.Error("ls with two positional args should be rejected")
	}
}

func TestLsRejectsAnInvalidStatusBeforeTouchingConfig(t *testing.T) {
	// Same fast-fail shape as admin_test.go's invalid-remote case: a bad
	// --status should error clearly before config.Load()/auth even run,
	// so it fails the same way whether or not the machine is configured.
	root := newRootCmd()
	root.SetArgs([]string{"ls", "--status", "paused"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("ls --status paused = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "live or ended") {
		t.Errorf("error = %v, want it to mention the valid status values", err)
	}
}

func TestLsReportsNotConfigured(t *testing.T) {
	withFakeHome(t) // no config.toml written

	root := newRootCmd()
	root.SetArgs([]string{"ls"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("ls with no config.toml = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %v, want it to mention carryon isn't configured", err)
	}
}

func TestLsRejectsAnInvalidSinceBeforeCallingTheServer(t *testing.T) {
	home := withFakeHome(t)
	writeFakeConfig(t, home)

	root := newRootCmd()
	root.SetArgs([]string{"ls", "--since", "not-a-duration"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("ls --since not-a-duration = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "--since") {
		t.Errorf("error = %v, want it to name --since", err)
	}
}
