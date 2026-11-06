package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/reader"
	"github.com/devminified/carryon/internal/state"
)

// logsPollInterval is how often `carryon logs -f` checks the agent's log
// file for new lines — a human watching a terminal doesn't need
// anything tighter than this.
const logsPollInterval = 500 * time.Millisecond

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the capture agent's logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new log lines as they're written")
	return cmd
}

func runLogs(cmd *cobra.Command, follow bool) error {
	carryonDir, err := config.Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(carryonDir, "logs", "agent.log")

	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No agent logs yet — has `carryon agent` ever run?")
		if !follow {
			return nil
		}
	}

	// reader.Read (not the plain offset-tracking ReadNew) on purpose:
	// its rewrite/truncation detection means a log rotating out from
	// under a running `carryon logs -f` (CO-113's daily rotation) is
	// picked up as "start over from the new file's beginning" instead
	// of getting stuck waiting for an offset the rotated-away file will
	// never reach again.
	var ptr state.Pointer
	for {
		data, next, _, err := reader.Read(path, ptr)
		if err != nil {
			return fmt.Errorf("logs: %w", err)
		}
		ptr = next
		if len(data) > 0 {
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return err
			}
		}
		if !follow {
			return nil
		}
		time.Sleep(logsPollInterval)
	}
}
