package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAdminProjectAddCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"admin", "project", "add"})
	if err != nil {
		t.Fatalf("find admin project add: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("admin project add has no RunE")
	}
}

func TestAdminDeleteCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"admin", "delete"})
	if err != nil {
		t.Fatalf("find admin delete: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("admin delete has no RunE")
	}
	if cmd.Flags().Lookup("reason") == nil {
		t.Fatal("admin delete has no --reason flag")
	}
}

func TestAdminDeleteRequiresExactlyOneArg(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"admin", "delete"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err == nil {
		t.Fatal("admin delete with no session id = nil error, want an error")
	}
}

func TestAdminProjectAddRejectsInvalidRemoteBeforeTouchingNetwork(t *testing.T) {
	// NormalizeRemote runs before config.Load()/auth, so an invalid remote
	// should fail fast and clearly, with no "not configured" noise, even
	// on a machine with no ~/.carryon/config.toml at all.
	root := newRootCmd()
	root.SetArgs([]string{"admin", "project", "add", "acme-api", "not-a-remote-at-all"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("admin project add with an invalid remote = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "invalid remote") {
		t.Errorf("error = %v, want it to mention the remote is invalid", err)
	}
}
