package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devminified/carryon/internal/config"
)

func withFakeHomeForConfigCmd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestConfigSetCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"config", "set"})
	if err != nil {
		t.Fatalf("find config set: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("config set has no RunE")
	}
}

func TestConfigSetWritesTheField(t *testing.T) {
	withFakeHomeForConfigCmd(t)

	root := newRootCmd()
	root.SetArgs([]string{"config", "set", "api_url", "https://carryon.internal.example.com"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("config set api_url: %v", err)
	}

	cfg, err := config.LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if cfg.APIURL != "https://carryon.internal.example.com" {
		t.Errorf("APIURL = %q, want the self-hosted URL", cfg.APIURL)
	}
}

// The real CO-506 bootstrap case: api_url and anon_key are set in two
// separate commands on a brand-new machine with no config.toml yet —
// the first command must not require the second field to already be
// present.
func TestConfigSetTwoFieldsSeparatelyOnAFreshMachine(t *testing.T) {
	withFakeHomeForConfigCmd(t)

	root := newRootCmd()
	root.SetArgs([]string{"config", "set", "api_url", "https://carryon.internal.example.com"})
	root.SetOut(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("first config set: %v", err)
	}

	root2 := newRootCmd()
	root2.SetArgs([]string{"config", "set", "anon_key", "sb_publishable_selfhost"})
	root2.SetOut(&bytes.Buffer{})
	if err := root2.Execute(); err != nil {
		t.Fatalf("second config set (api_url still unset at load time otherwise): %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() after both fields set: %v", err)
	}
	if cfg.APIURL != "https://carryon.internal.example.com" || cfg.AnonKey != "sb_publishable_selfhost" {
		t.Errorf("Load() = %+v, want both fields preserved across the two set calls", cfg)
	}
}

func TestConfigSetRejectsUnknownKey(t *testing.T) {
	withFakeHomeForConfigCmd(t)

	root := newRootCmd()
	root.SetArgs([]string{"config", "set", "machine_id", "whatever"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("config set machine_id = nil error, want an error (not a settable field)")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("error = %v, want it to mention the unknown key", err)
	}
}

func TestConfigGetReturnsWhatWasSet(t *testing.T) {
	withFakeHomeForConfigCmd(t)

	setRoot := newRootCmd()
	setRoot.SetArgs([]string{"config", "set", "api_url", "https://carryon.internal.example.com"})
	setRoot.SetOut(&bytes.Buffer{})
	if err := setRoot.Execute(); err != nil {
		t.Fatalf("config set: %v", err)
	}

	getRoot := newRootCmd()
	getRoot.SetArgs([]string{"config", "get", "api_url"})
	var out bytes.Buffer
	getRoot.SetOut(&out)
	if err := getRoot.Execute(); err != nil {
		t.Fatalf("config get: %v", err)
	}
	if strings.TrimSpace(out.String()) != "https://carryon.internal.example.com" {
		t.Errorf("config get api_url = %q, want the self-hosted URL", out.String())
	}
}
