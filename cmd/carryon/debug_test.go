package main

import (
	"bytes"
	"testing"
)

func TestDebugCommandIsHidden(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"debug"})
	if err != nil {
		t.Fatalf("find debug: %v", err)
	}
	if !cmd.Hidden {
		t.Error("debug command should be Hidden")
	}
}

func TestDebugEncodePathPrintsTheRealEncoding(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"debug", "encode-path", "/Users/cto/Documents/GitHub/carryon"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := "-Users-cto-Documents-GitHub-carryon\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

func TestDebugEncodePathRequiresExactlyOneArg(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"debug", "encode-path"})
	if err != nil {
		t.Fatalf("find debug encode-path: %v", err)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("encode-path with no args should be rejected")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("encode-path with two args should be rejected")
	}
}
