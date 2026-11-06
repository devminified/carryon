package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/status"
)

func newStatusCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show login, agent, and session status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print the same report as JSON")
	return cmd
}

func runStatus(cmd *cobra.Command, jsonOutput bool) error {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			return fmt.Errorf("%w (see README for how to point carryon at a Supabase project)", err)
		}
		return err
	}
	provider, err := auth.NewProvider(cfg)
	if err != nil {
		return err
	}

	carryonDir, err := config.Dir()
	if err != nil {
		return err
	}
	paths := status.Paths{
		AgentStatusPath: filepath.Join(carryonDir, "agent-status.json"),
		SkippedPath:     filepath.Join(carryonDir, "skipped-repos.json"),
		StateDir:        filepath.Join(carryonDir, "state"),
	}

	report := status.Gather(cmd.Context(), cfg.APIURL, provider, paths)

	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding status as json: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}

	_, err = fmt.Fprint(cmd.OutOrStdout(), status.Render(report, time.Now()))
	return err
}
