package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// newAgentCmd is a placeholder for CO-107's capture agent daemon. It
// deliberately does not exit immediately (the way the other stubs do,
// returning errNotImplemented): CO-106's `carryon install` registers this
// command as a real OS-level service (launchd/systemd/Windows Service) and
// starts it, so an instant non-zero exit here would crash-loop against the
// service manager until CO-107 actually lands. Idling until the service
// manager stops it is the safe placeholder behavior; CO-107 replaces the
// body of this command with the real tailing/upload loop.
func newAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent",
		Short: "Run the capture agent daemon (CO-107)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "carryon agent: capture loop not yet implemented (CO-107); idling until stopped")
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			<-ctx.Done()
			return nil
		},
	}
}
