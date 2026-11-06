package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogsCommandHasFollowFlag(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"logs"})
	if err != nil {
		t.Fatalf("find logs: %v", err)
	}
	if cmd.Flags().Lookup("follow") == nil {
		t.Error(`logs command has no --follow flag`)
	}
}

func TestLogsPrintsNoLogsYetMessageWhenFileIsMissing(t *testing.T) {
	withFakeHome(t)

	root := newRootCmd()
	root.SetArgs([]string{"logs"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err != nil {
		t.Fatalf("logs Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "No agent logs yet") {
		t.Errorf("output = %q, want the \"no logs yet\" message", out.String())
	}
}

func TestLogsPrintsExistingContentWithoutFollowing(t *testing.T) {
	home := withFakeHome(t)
	logsDir := filepath.Join(home, ".carryon", "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{"level":"INFO","msg":"session s1 confirmed"}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "agent.log"), []byte(content), 0o600); err != nil {
		t.Fatalf("writing agent.log: %v", err)
	}

	root := newRootCmd()
	root.SetArgs([]string{"logs"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err != nil {
		t.Fatalf("logs Execute() error = %v", err)
	}
	if out.String() != content {
		t.Errorf("output = %q, want %q", out.String(), content)
	}
}
