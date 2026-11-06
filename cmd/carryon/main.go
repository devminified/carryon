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
		newLoginCmd(),
		newLogoutCmd(),
		newHookCmd(),
		newAgentCmd(),
		newStatusCmd(),
		newLsCmd(),
		newPullCmd(),
		newExportCmd(),
		newRenameCmd(),
	)

	return root
}
