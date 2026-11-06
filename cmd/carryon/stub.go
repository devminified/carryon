package main

import "github.com/spf13/cobra"

// notImplementedCmd scaffolds a subcommand whose real implementation lands
// in a later ticket. It exists so `carryon --help` documents the full CLI
// surface from CO-101 onward, per the Overview tab's command list.
func notImplementedCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newLsCmd() *cobra.Command {
	return notImplementedCmd("ls", "List the sessions you can see (CO-204)")
}

func newPullCmd() *cobra.Command {
	return notImplementedCmd("pull", "Pull and resume a session on this machine (CO-211)")
}

func newExportCmd() *cobra.Command {
	return notImplementedCmd("export", "Rebuild a session file without placing or launching it (CO-211)")
}

func newRenameCmd() *cobra.Command {
	return notImplementedCmd("rename", "Rename a session (CO-405)")
}
