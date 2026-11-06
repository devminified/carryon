package main

import (
	"context"
	"testing"
	"time"
)

func TestAgentCommandExitsCleanlyWhenContextCancelled(t *testing.T) {
	// Simulates the service manager stopping the agent (SIGTERM/service
	// Stop) — see agent.go's doc comment for why this placeholder must not
	// exit immediately on its own.
	root := newRootCmd()
	root.SetArgs([]string{"agent"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("agent RunE returned error: %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent command did not exit after its context was cancelled")
	}
}
