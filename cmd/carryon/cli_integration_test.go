package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBinary compiles the real carryon binary once per test run, so these
// tests exercise the actual CLI surface (exit codes, --help text) rather
// than calling cobra internals directly — the shape CO-101's testing points
// describe.
func buildBinary(t *testing.T) string {
	t.Helper()
	name := "carryon"
	if runtime.GOOS == "windows" {
		// os/exec needs the literal extension on Windows; it doesn't
		// resolve one the way cmd.exe's PATH lookup does.
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestHelpListsEverySubcommand(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("carryon --help: %v\n%s", err, out)
	}
	for _, name := range allSubcommands {
		if !strings.Contains(string(out), name) {
			t.Errorf("--help output is missing subcommand %q:\n%s", name, out)
		}
	}
}

func TestStubSubcommandsExitTwoAndSayNotImplemented(t *testing.T) {
	bin := buildBinary(t)
	for _, name := range stubSubcommands {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command(bin, name).CombinedOutput()
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("carryon %s: expected an ExitError, got %v (output: %s)", name, err, out)
			}
			if code := exitErr.ExitCode(); code != 2 {
				t.Errorf("carryon %s exit code = %d, want 2", name, code)
			}
			if !strings.Contains(string(out), "not implemented") {
				t.Errorf("carryon %s output = %q, want it to contain %q", name, out, "not implemented")
			}
		})
	}
}

func TestVersionCommandExitsZero(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("carryon version: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "carryon") {
		t.Errorf("carryon version output = %q, want it to mention carryon", out)
	}
}
