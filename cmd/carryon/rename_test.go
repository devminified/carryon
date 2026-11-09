package main

import (
	"bytes"
	"testing"
)

func TestRenameCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"rename"})
	if err != nil {
		t.Fatalf("find rename: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("rename has no RunE")
	}
}

func TestRenameRequiresExactlyTwoArgs(t *testing.T) {
	for _, args := range [][]string{
		{"rename"},
		{"rename", "only-one-arg"},
		{"rename", "s1", "title", "extra"},
	} {
		root := newRootCmd()
		root.SetArgs(args)
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)

		if err := root.Execute(); err == nil {
			t.Errorf("rename %v = nil error, want an error", args[1:])
		}
	}
}
