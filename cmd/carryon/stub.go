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

func newInstallCmd() *cobra.Command {
	return notImplementedCmd("install", "Install the Carryon hooks and background agent (CO-106)")
}

func newHookCmd() *cobra.Command {
	return notImplementedCmd("hook", "Record a Claude Code hook event to the local queue (CO-105)")
}

func newAgentCmd() *cobra.Command {
	return notImplementedCmd("agent", "Run the capture agent daemon (CO-107)")
}

func newStatusCmd() *cobra.Command {
	return notImplementedCmd("status", "Show login, agent, and session status (CO-113)")
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
