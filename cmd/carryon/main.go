// Command carryon is the laptop binary: the Claude Code hook, the capture
// agent, and the pull/export CLI. See CLAUDE.md and .claude/rules/ for the
// invariants and conventions every subcommand must respect.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Set via -ldflags by CI and .goreleaser.yaml (CO-101).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// updatePublicKeyHex and updateManifestURL (CO-308) are set via
// -ldflags at release-build time, same mechanism as version/commit/
// date above — see .goreleaser.yaml. updatePublicKeyHex deliberately
// defaults to "" (not a real key baked into this source tree): until
// a real production signing key exists and a release build sets it,
// internal/update.CheckAndApply treats an empty key as "update
// checking disabled" and never trusts an unsigned manifest — see that
// package's own ParseAndVerify doc comment, and
// docs/co-308-fleet-rollout.md for the one-time key-provisioning step
// this depends on.
var (
	updatePublicKeyHex = ""
	updateManifestURL  = "https://github.com/devminified/carryon/releases/latest/download/manifest.json"
)

// errNotImplemented is returned by subcommands that are scaffolded but not
// yet built (CO-101). It is the one sentinel every stub RunE returns, so
// main can translate it to exit code 2 without each stub duplicating that
// logic.
var errNotImplemented = errors.New("not implemented")

func main() {
	os.Exit(run())
}

func run() int {
	err := newRootCmd().Execute()
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, "carryon:", err)
	return exitCodeForErr(err)
}

// exitCodeForErr maps a non-nil root-command error to its exit code —
// split out from run() so the mapping itself (in particular,
// errNotImplemented's code 2) stays unit-testable even when no current
// subcommand is actually a stub.
func exitCodeForErr(err error) int {
	if errors.Is(err, errNotImplemented) {
		return 2
	}
	return 1
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "carryon",
		Short:         "Capture, pull, and resume Claude Code sessions across your team.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newVersionCmd(),
		newAdminCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newHookCmd(),
		newAgentCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newLsCmd(),
		newPullCmd(),
		newExportCmd(),
		newRenameCmd(),
	)

	return root
}
