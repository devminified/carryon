package main

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/config"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in with GitHub",
		RunE:  runLogin,
	}
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out and remove the stored session",
		RunE:  runLogout,
	}
}

func loadAuthProvider() (*auth.Provider, error) {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			return nil, fmt.Errorf("%w (see README for how to point carryon at a Supabase project)", err)
		}
		return nil, err
	}
	return auth.NewProvider(cfg)
}

func runLogin(cmd *cobra.Command, args []string) error {
	provider, err := loadAuthProvider()
	if err != nil {
		return err
	}

	if provider.LoggedIn() {
		if user, err := provider.CurrentUser(cmd.Context()); err == nil {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Already logged in as %s (%s).\n", user.UserName, user.Email)
		} else {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Already logged in.")
		}
		if !confirm(cmd, "Log in again and replace the current session?") {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
			return nil
		}
	}

	if err := provider.Login(cmd.Context()); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if user, err := provider.CurrentUser(cmd.Context()); err == nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Logged in as %s (%s).\n", user.UserName, user.Email)
	} else {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Logged in.")
	}
	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	provider, err := loadAuthProvider()
	if err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Not logged in.")
			return nil
		}
		return err
	}

	if !provider.LoggedIn() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Not logged in.")
		return nil
	}

	if err := provider.Logout(); err != nil {
		return fmt.Errorf("logout failed: %w", err)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Logged out.")
	return nil
}

// confirm asks a yes/no question on the command's own in/out streams (so
// it's swappable in tests), defaulting to no on anything but an explicit
// y/yes.
func confirm(cmd *cobra.Command, prompt string) bool {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
