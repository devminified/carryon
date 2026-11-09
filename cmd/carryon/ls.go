package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/ls"
)

// lsFlags collects ls's flags so runLs stays a plain function of its
// inputs — easier to unit test than reaching back into *cobra.Command
// for each one.
type lsFlags struct {
	project, dev, status, since, file string
	limit                             int
	json                              bool
}

func newLsCmd() *cobra.Command {
	var f lsFlags

	cmd := &cobra.Command{
		Use:   "ls [session-id]",
		Short: "List the sessions you can see",
		Long: "List the sessions you can see, newest activity first. An optional\n" +
			"session ID (or short ID — its first 8 characters) narrows the list\n" +
			"to that one session.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(cmd, args, f)
		},
	}
	cmd.Flags().StringVar(&f.project, "project", "", "filter to one project by name")
	cmd.Flags().StringVar(&f.dev, "dev", "", "filter to one developer by GitHub login")
	cmd.Flags().StringVar(&f.status, "status", "", "filter by status: live or ended")
	cmd.Flags().StringVar(&f.since, "since", "", "only sessions active since, e.g. 7d, 24h, 30m")
	cmd.Flags().StringVar(&f.file, "file", "", "only sessions that touched this file (path relative to the repo root, or a directory prefix)")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "maximum rows to return (server default: 50)")
	cmd.Flags().BoolVar(&f.json, "json", false, "print as JSON")
	return cmd
}

func runLs(cmd *cobra.Command, args []string, f lsFlags) error {
	// Validate every flag before touching config/auth/network, so a typo
	// fails the same clear way whether or not this machine is configured
	// or logged in — same shape as admin's invalid-remote check.
	if f.status != "" && f.status != "live" && f.status != "ended" {
		return fmt.Errorf("invalid --status %q: want live or ended", f.status)
	}
	in := api.ListSessionsInput{Project: f.project, Dev: f.dev, Status: f.status, Limit: f.limit, File: f.file}
	if f.since != "" {
		cutoff, err := ls.ParseSince(f.since, time.Now())
		if err != nil {
			return err
		}
		in.Since = &cutoff
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	provider, err := auth.NewProvider(cfg)
	if err != nil {
		return err
	}
	token, err := provider.AccessToken(cmd.Context())
	if err != nil {
		return err
	}

	client := api.NewClient(cfg.APIURL, cfg.AnonKey)
	sessions, err := client.ListSessions(cmd.Context(), token, in)
	if err != nil {
		return err
	}

	if len(args) == 1 {
		match, err := ls.ResolveShortID(sessions, args[0])
		if err != nil {
			return err
		}
		sessions = []api.SessionSummary{match}
	}

	if f.json {
		data, err := json.MarshalIndent(sessions, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding sessions as json: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}

	_, err = fmt.Fprint(cmd.OutOrStdout(), ls.Render(sessions, time.Now()))
	return err
}
