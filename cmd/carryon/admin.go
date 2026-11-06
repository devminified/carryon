package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/project"
)

// newAdminCmd groups administrative commands. CO-104's project
// registration is the first; CO-410 replaces this with a web UI, but the
// CLI stays as the scriptable path.
func newAdminCmd() *cobra.Command {
	admin := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands",
	}
	admin.AddCommand(newAdminProjectCmd())
	return admin
}

func newAdminProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage registered projects",
	}
	cmd.AddCommand(newAdminProjectAddCmd())
	return cmd
}

func newAdminProjectAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> <remote>",
		Short: "Register a project so Carryon captures sessions in it",
		Args:  cobra.ExactArgs(2),
		RunE:  runAdminProjectAdd,
	}
}

func runAdminProjectAdd(cmd *cobra.Command, args []string) error {
	name, rawRemote := args[0], args[1]

	canonical, err := project.NormalizeRemote(rawRemote)
	if err != nil {
		return fmt.Errorf("invalid remote %q: %w", rawRemote, err)
	}

	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			return fmt.Errorf("%w (see README for how to point carryon at a Supabase project)", err)
		}
		return err
	}

	provider, err := loadAuthProvider()
	if err != nil {
		return err
	}
	token, err := provider.AccessToken(cmd.Context())
	if err != nil {
		return err
	}

	client := api.NewClient(cfg.APIURL, cfg.AnonKey)
	result, err := client.RegisterProject(cmd.Context(), token, name, canonical)
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == 409 {
			return fmt.Errorf("a project with remote %q is already registered", canonical)
		}
		return fmt.Errorf("registering project: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Registered %q as %s (id %s). You're its first admin.\n", result.Name, result.GitRemote, result.ID)
	return nil
}
