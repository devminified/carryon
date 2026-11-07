package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withFakeHome points os.UserHomeDir() (and so Dir()/path()) at a temp
// directory for the duration of the test, on both Unix (HOME) and Windows
// (USERPROFILE).
func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestLoadReturnsErrNotConfiguredWhenMissing(t *testing.T) {
	withFakeHome(t)
	_, err := Load()
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load() error = %v, want ErrNotConfigured", err)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	withFakeHome(t)

	want := &Config{APIURL: "http://127.0.0.1:54421", AnonKey: "sb_publishable_test"}
	if err := Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.APIURL != want.APIURL || got.AnonKey != want.AnonKey {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestSaveThenLoadRoundTripsRepos(t *testing.T) {
	withFakeHome(t)

	want := &Config{
		APIURL:  "http://127.0.0.1:54421",
		AnonKey: "sb_publishable_test",
		Repos: map[string]string{
			"github.com/devminified/acme-api": "/Users/ali/code/acme-api",
		},
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Repos["github.com/devminified/acme-api"] != "/Users/ali/code/acme-api" {
		t.Errorf("Load().Repos = %+v, want the saved mapping", got.Repos)
	}
}

func TestLoadRejectsMissingFields(t *testing.T) {
	withFakeHome(t)

	if err := Save(&Config{AnonKey: "key-only"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := Load(); err == nil {
		t.Error("Load() with empty api_url = nil error, want an error")
	}
}

func TestSaveWritesPrivateFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply on Windows")
	}
	dir := withFakeHome(t)

	if err := Save(&Config{APIURL: "http://x", AnonKey: "y"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, ".carryon", "config.toml"))
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.toml mode = %o, want 0600", perm)
	}
}
