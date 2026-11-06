package main

import (
	"fmt"

	"github.com/devminified/carryon/internal/install"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install the Carryon hooks and background agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := install.Install(); err != nil {
				return fmt.Errorf("install: %w", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Installed Carryon's hooks in ~/.claude/settings.json and started the capture agent.")
			return nil
		},
	}
}
