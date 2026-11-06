package main

import (
	"testing"
	"time"
)

// newAgentCmd's RunE calls kservice.Service.Run(), which blocks on a real
// OS signal (SIGTERM/SIGINT) — not cmd.Context() — so it's deliberately
// never invoked directly here (that would hang a test until it's killed).
// agentProgram's Start/Stop are tested directly instead; CO-106's own
// install/uninstall tests take the same approach with the real OS service
// manager (see install_test.go).

func TestAgentCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"agent"})
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("agent has no RunE")
	}
}

func TestAgentProgramStopWaitsForRunToReturn(t *testing.T) {
	withFakeHome(t) // agent.New resolves ~/.carryon via internal/config

	p := &agentProgram{}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- p.Stop(nil) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Stop() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return")
	}
}
