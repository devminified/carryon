package main

import (
	"testing"
	"time"

	"github.com/devminified/carryon/internal/redact"
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

// redactConfig must never cause an upload to go out with a weaker rule
// set than the built-in defaults just because the agent isn't logged
// in/configured yet (I5) — it falls back to redact.DefaultConfig()
// rather than erroring or returning an empty Config.
func TestRedactConfigFallsBackToDefaultWhenNotConfigured(t *testing.T) {
	withFakeHome(t) // no config.toml, so lazyAPI.resolve() fails every time

	l := &lazyAPI{}
	got := l.redactConfig()
	want := redact.DefaultConfig()

	if len(got.Rules) != len(want.Rules) {
		t.Fatalf("len(Rules) = %d, want %d", len(got.Rules), len(want.Rules))
	}
	if len(got.Denylist) != len(want.Denylist) {
		t.Fatalf("len(Denylist) = %d, want %d", len(got.Denylist), len(want.Denylist))
	}
}

// Once a successful fetch has happened, a later failure must keep
// serving the last known-good merged config instead of reverting to
// plain defaults — same "don't blank out a good value on a transient
// error" rule internal/upload's cachedClaudeVersion already follows.
func TestRedactConfigKeepsLastKnownGoodAfterResolveStartsFailing(t *testing.T) {
	withFakeHome(t)

	l := &lazyAPI{
		cachedRedactConfig:  redact.ApplyServerConfig(redact.DefaultConfig(), []string{"generic-api-key"}, nil),
		redactConfigAt:      time.Now().Add(-redactConfigCacheTTL - time.Minute), // force past TTL
		redactConfigFetched: true,
	}
	got := l.redactConfig()
	if len(got.Rules) != len(redact.DefaultConfig().Rules)-1 {
		t.Fatalf("len(Rules) = %d, want %d (should still be serving the cached merged config)", len(got.Rules), len(redact.DefaultConfig().Rules)-1)
	}
}
