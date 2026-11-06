package main

import (
	"fmt"

	"github.com/devminified/carryon/internal/install"
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Carryon hooks and stop the background agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := install.Uninstall(); err != nil {
				return fmt.Errorf("uninstall: %w", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Removed Carryon's hooks from ~/.claude/settings.json and stopped the capture agent.")
			return nil
		},
	}
}
