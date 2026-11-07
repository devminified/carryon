package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCommandHasOutputFlag(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"export"})
	if err != nil {
		t.Fatalf("find export: %v", err)
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Error("export command has no --output flag")
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("export command has no -o shorthand")
	}
}

func TestExportRequiresExactlyOneArg(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"export"})
	if err != nil {
		t.Fatalf("find export: %v", err)
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("export with no args should be rejected")
	}
	if err := cmd.Args(cmd, []string{"one-id", "two-id"}); err == nil {
		t.Error("export with two positional args should be rejected")
	}
}

func TestExportRequiresOutputFlag(t *testing.T) {
	withFakeHome(t)
	writeFakeConfig(t, os.Getenv("HOME"))

	root := newRootCmd()
	root.SetArgs([]string{"export", "11111111-1111-1111-1111-111111111111"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("export with no --output = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "--output") {
		t.Errorf("error = %v, want it to mention --output", err)
	}
}

func TestExportReportsNotConfigured(t *testing.T) {
	withFakeHome(t) // no config.toml written

	root := newRootCmd()
	root.SetArgs([]string{"export", "11111111-1111-1111-1111-111111111111", "-o", filepath.Join(t.TempDir(), "out.jsonl")})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("export with no config = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %v, want it to mention carryon isn't configured", err)
	}
}

func TestCopyFileCopiesContentExactly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	want := []byte(`{"sessionId":"s1"}` + "\n")
	if err := os.WriteFile(src, want, 0o600); err != nil {
		t.Fatalf("writing src: %v", err)
	}

	dst := filepath.Join(dir, "out", "dst.jsonl")
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("copied content = %q, want %q", got, want)
	}
}

func TestCopyFileOverwritesAnExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatalf("writing src: %v", err)
	}
	dst := filepath.Join(dir, "dst.jsonl")
	if err := os.WriteFile(dst, []byte("old-content-that-should-be-replaced"), 0o600); err != nil {
		t.Fatalf("writing dst: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new" {
		t.Errorf("dst content = %q, err = %v, want %q", got, err, "new")
	}
}
