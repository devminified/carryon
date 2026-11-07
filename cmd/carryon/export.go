package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/pull"
)

func newExportCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "export <session-id>",
		Short: "Rebuild a session file without placing or launching it",
		Long: "Rebuild a session's main transcript from its manifest, for manual\n" +
			"use and debugging — unlike `carryon pull`, the result is neither\n" +
			"path/session-ID rewritten nor placed under ~/.claude/projects.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(cmd, args[0], output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "file to write the rebuilt session to (required)")
	return cmd
}

func runExport(cmd *cobra.Command, sessionID, output string) error {
	if output == "" {
		return fmt.Errorf("--output (-o) is required")
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

	return exportWithClient(cmd, client, token, sessionID, output)
}

// exportWithClient is runExport's actual pipeline — same seam as
// pullWithClient, for the same reason (testable against a real local
// Supabase without touching the real OS keychain).
func exportWithClient(cmd *cobra.Command, client *api.Client, token, sessionID, output string) error {
	ctx := cmd.Context()

	manifest, err := client.GetManifest(ctx, token, sessionID)
	if err != nil {
		return fmt.Errorf("fetching manifest: %w", err)
	}

	destDir, err := os.MkdirTemp("", "carryon-export-")
	if err != nil {
		return fmt.Errorf("creating a temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(destDir) }()

	result, err := pull.Rebuild(ctx, client, manifest, destDir, pull.DefaultConfig())
	if err != nil {
		return fmt.Errorf("rebuilding session: %w", err)
	}
	if result.MainFile == "" {
		return fmt.Errorf("session %s has no main transcript to export", sessionID)
	}

	if err := copyFile(result.MainFile, output); err != nil {
		return fmt.Errorf("writing %s: %w", output, err)
	}

	if n := len(result.SubagentFiles); n > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Exported %s. %d subagent stream(s) were not exported.\n", output, n)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Exported %s.\n", output)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- src is this process's own rebuilt temp file
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst) // #nosec G304 -- dst is the operator-supplied --output path
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
