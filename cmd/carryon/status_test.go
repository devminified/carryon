package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devminified/carryon/internal/status"
)

func writeFakeConfig(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".carryon")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	toml := "api_url = \"http://127.0.0.1:54421\"\nanon_key = \"anon-key\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o600); err != nil {
		t.Fatalf("writing config.toml: %v", err)
	}
}

func TestStatusCommandHasJSONFlag(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"status"})
	if err != nil {
		t.Fatalf("find status: %v", err)
	}
	if cmd.Flags().Lookup("json") == nil {
		t.Error(`status command has no --json flag`)
	}
}

func TestStatusReportsNotConfigured(t *testing.T) {
	withFakeHome(t) // no config.toml written

	root := newRootCmd()
	root.SetArgs([]string{"status"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("status with no config.toml = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %v, want it to mention carryon isn't configured", err)
	}
}

func TestStatusCommandPrintsLoggedOutAndAgentDown(t *testing.T) {
	home := withFakeHome(t)
	writeFakeConfig(t, home)

	root := newRootCmd()
	root.SetArgs([]string{"status"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err != nil {
		t.Fatalf("status Execute() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Not logged in") {
		t.Errorf("output missing \"Not logged in\":\n%s", got)
	}
	if !strings.Contains(got, "Agent: not running") {
		t.Errorf("output missing \"Agent: not running\":\n%s", got)
	}
	if !strings.Contains(got, "http://127.0.0.1:54421") {
		t.Errorf("output missing the configured backend URL:\n%s", got)
	}
}

func TestStatusJSONOutputIsValidAndMatchesTheConfiguredBackend(t *testing.T) {
	home := withFakeHome(t)
	writeFakeConfig(t, home)

	root := newRootCmd()
	root.SetArgs([]string{"status", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err != nil {
		t.Fatalf("status --json Execute() error = %v", err)
	}

	var report status.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("output isn't valid JSON: %v\noutput:\n%s", err, out.String())
	}
	if report.LoggedIn {
		t.Error("LoggedIn = true, want false")
	}
	if report.BackendURL != "http://127.0.0.1:54421" {
		t.Errorf("BackendURL = %q, want the configured backend", report.BackendURL)
	}
	if report.Agent.Running {
		t.Error("Agent.Running = true, want false")
	}
}
