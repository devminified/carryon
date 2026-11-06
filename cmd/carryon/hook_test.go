package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestHookCommandHasFinalFlag(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"hook"})
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	if cmd.Flags().Lookup("final") == nil {
		t.Error(`hook command has no --final flag`)
	}
}

func TestHookCommandAlwaysExitsZero(t *testing.T) {
	// The hook path must never fail Claude Code's session (I1, NFR-01) —
	// even malformed stdin must return nil from RunE (which main.go turns
	// into exit code 0).
	withFakeHome(t)

	root := newRootCmd()
	root.SetArgs([]string{"hook"})
	root.SetIn(strings.NewReader(`not json at all`))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err != nil {
		t.Errorf("hook with malformed stdin returned error %v, want nil", err)
	}
}

func TestHookCommandAppendsToQueue(t *testing.T) {
	home := withFakeHome(t)

	root := newRootCmd()
	root.SetArgs([]string{"hook"})
	root.SetIn(strings.NewReader(`{"session_id":"s1","cwd":"/repo","hook_event_name":"Stop"}`))

	if err := root.Execute(); err != nil {
		t.Fatalf("hook: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".carryon", "queue.jsonl")) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read queue.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"s1"`) {
		t.Errorf("queue.jsonl = %q, want it to contain session_id s1", data)
	}
}

func TestHookCommandFinalFlagSetsSessionEndEvent(t *testing.T) {
	home := withFakeHome(t)

	root := newRootCmd()
	root.SetArgs([]string{"hook", "--final"})
	root.SetIn(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop"}`))

	if err := root.Execute(); err != nil {
		t.Fatalf("hook --final: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".carryon", "queue.jsonl")) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read queue.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"SessionEnd"`) {
		t.Errorf("queue.jsonl = %q, want event SessionEnd", data)
	}
}
