package main

import (
	"github.com/devminified/carryon/internal/hook"
	"github.com/spf13/cobra"
)

// newHookCmd wires the Claude Code Stop/SessionEnd hooks (installed by
// CO-106) to internal/hook. This command always exits 0 — see
// internal/hook's package doc for why: the hook path must never block or
// fail Claude Code (I1, NFR-01).
func newHookCmd() *cobra.Command {
	var final bool

	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Record a Claude Code hook event to the local queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			hook.Run(cmd.InOrStdin(), final)
			return nil
		},
	}
	cmd.Flags().BoolVar(&final, "final", false, "mark this event as SessionEnd (wired to Claude Code's SessionEnd hook)")
	return cmd
}
