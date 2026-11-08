package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/devminified/carryon/internal/update"
)

// updateCheckInterval is CO-308's "the agent checks a release manifest
// daily."
const updateCheckInterval = 24 * time.Hour

// runUpdateLoop checks for and applies a newer signed release once
// immediately on startup, then once per updateCheckInterval, until ctx
// is cancelled. It's deliberately fire-and-forget from
// agentProgram.Start's point of view (not tracked in p.done) — unlike
// an in-flight upload, there's no partial-write state here that needs
// a graceful shutdown barrier; ctx cancellation stops the ticking
// promptly on its own.
//
// A successful update spawns the new binary (update.Restart) and then
// os.Exit(0)s this process outright, rather than trying to hot-swap
// in place — the simplest restart mechanism, and the same one a
// kardianos/service-wrapped daemon's own OS-level restart policy
// (launchd KeepAlive, systemd Restart=, Windows SCM recovery) is
// already expected to tolerate for any other reason this process
// might exit. See docs/co-308-fleet-rollout.md for the current
// limitation this implies (no coordination with that external
// restart policy beyond "the binary at this path is already the new
// one by the time anything restarts it").
func runUpdateLoop(ctx context.Context, lazy *lazyAPI) {
	execPath, err := os.Executable()
	if err != nil {
		slog.Warn("update: could not determine the running executable's path — the daily update check is disabled for this run", "error", err)
		return
	}

	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()

	checkOnce(ctx, lazy, execPath, update.Restart, os.Exit)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkOnce(ctx, lazy, execPath, update.Restart, os.Exit)
		}
	}
}

// checkOnce runs one update cycle against execPath. restart/exit are
// update.Restart and os.Exit in production, injected so a test can
// observe "this is the point where production would restart and
// exit" without actually taking the test binary down — os.Exit has no
// way to be undone or faked otherwise.
func checkOnce(
	ctx context.Context,
	lazy *lazyAPI,
	execPath string,
	restart func(execPath string, args []string) error,
	exit func(code int),
) {
	// A transient pin-lookup failure must never be read as "no pin
	// configured" — that would let an update past a real pin slip
	// through during a server hiccup. Skip this cycle entirely
	// instead; the next one tries again.
	pinned, err := lazy.pinnedVersion(ctx)
	if err != nil {
		slog.Warn("update: fetching the pinned version failed, skipping this check", "error", err)
		return
	}

	result, err := update.CheckAndApply(ctx, update.Config{
		ManifestURL:    updateManifestURL,
		PublicKeyHex:   updatePublicKeyHex,
		CurrentVersion: version,
		PinnedVersion:  pinned,
		ExecPath:       execPath,
	})
	if err != nil {
		slog.Warn("update: check failed", "error", err)
		return
	}
	if !result.Applied {
		return
	}

	slog.Info("update: applied, restarting", "from", result.FromVersion, "to", result.ToVersion)
	if err := restart(execPath, os.Args[1:]); err != nil {
		slog.Error("update: applied the new binary but restarting into it failed — the service manager's own restart policy will have to pick it up eventually", "error", err)
		return
	}
	exit(0)
}
