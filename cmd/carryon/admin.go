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
	admin.AddCommand(newAdminDeleteCmd())
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

// newAdminDeleteCmd is CO-307's "carryon admin delete <sid> --reason" —
// deletes a single session outright (its chunks, code snapshots, and
// their Storage objects), with a mandatory reason recorded in
// audit_log as the only remaining record once the session's own data
// is gone. Any fork of this session survives, reparented to no parent
// (delete-session/index.ts's own doc comment: the existing
// parent_session_id FK does this automatically).
func newAdminDeleteCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a single session outright (admin-only, requires --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminDelete(cmd, args[0], reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why this session is being deleted (required)")
	return cmd
}

func runAdminDelete(cmd *cobra.Command, sessionID, reason string) error {
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
	if err := client.DeleteSession(cmd.Context(), token, sessionID, reason); err != nil {
		var apiErr *api.Error
		if api.IsNotFound(err) {
			return fmt.Errorf("no such session %q", sessionID)
		}
		if api.IsForbidden(err) {
			return fmt.Errorf("only an admin of this session's project can delete it")
		}
		if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
			return fmt.Errorf("--reason is required: %s", apiErr.Message)
		}
		return fmt.Errorf("deleting session: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted session %s.\n", sessionID)
	return nil
}
