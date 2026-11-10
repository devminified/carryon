package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/claudepath"
)

// newDebugCmd groups commands that exist only to support the nightly
// Claude Code compatibility workflow (CO-503) and local debugging —
// Hidden so they don't clutter `carryon --help` for a real developer,
// never documented in README.md's command list. Nothing here writes
// any state or needs a logged-in session; encode-path is a pure,
// read-only wrapper around internal/claudepath's own real,
// fixture-verified encoder (CO-209), used by the nightly workflow's
// own script to confirm Claude Code's project-folder naming hasn't
// changed between versions.
func newDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "debug",
		Short:  "Internal debugging commands",
		Hidden: true,
	}
	cmd.AddCommand(newDebugEncodePathCmd())
	return cmd
}

func newDebugEncodePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "encode-path <absolute-path>",
		Short: "Print Claude Code's own ~/.claude/projects/<encoded> folder name for a path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), claudepath.Encode(args[0]))
			return err
		},
	}
}
