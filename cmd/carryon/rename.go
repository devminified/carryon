package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/config"
)

// newRenameCmd is CO-405's `carryon rename <session-id> "<title>"`.
// Either the session's own owner or an admin of its project may
// rename it; rename-session's own server-side check enforces this
// (and ultimately RLS, see that function's doc comment) — this
// command has no role check of its own beyond what the Edge Function
// returns.
func newRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <session-id> <title>",
		Short: "Rename a session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename(cmd, args[0], args[1])
		},
	}
}

func runRename(cmd *cobra.Command, sessionID, title string) error {
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
	if err := client.RenameSession(cmd.Context(), token, sessionID, title); err != nil {
		if api.IsNotFound(err) {
			return fmt.Errorf("no such session %q", sessionID)
		}
		if api.IsForbidden(err) {
			return fmt.Errorf("only the session's owner or a project admin can rename it")
		}
		return fmt.Errorf("renaming session: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Renamed session %s to %q.\n", sessionID, title)
	return nil
}
