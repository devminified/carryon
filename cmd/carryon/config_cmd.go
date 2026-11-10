package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/config"
)

// newConfigCmd groups ~/.carryon/config.toml field edits — CO-506's
// own addition, for a self-hosted install: `carryon config set
// api_url https://carryon.internal.example.com` points this same
// binary at a self-hosted stack's own Kong gateway instead of
// Supabase Cloud, with no separate self-host-only build. Before this
// ticket, config.toml only had `carryon login` (which writes
// api_url/anon_key together, prompting for both) as a way to set
// these fields — this is the first direct, scriptable path, useful
// for a fleet rollout that provisions config.toml by MDM/script
// rather than an interactive login.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read or change this machine's ~/.carryon/config.toml",
	}
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigGetCmd())
	return cmd
}

// configFields is the allowlist of keys `config set`/`config get` may
// touch — deliberately not every Config field (e.g. machine_id is
// generated and must never be hand-set; repos is a map, not a scalar
// field this command shape fits).
var configFields = map[string]struct {
	get func(*config.Config) string
	set func(*config.Config, string)
}{
	"api_url": {
		get: func(c *config.Config) string { return c.APIURL },
		set: func(c *config.Config, v string) { c.APIURL = v },
	},
	"anon_key": {
		get: func(c *config.Config) string { return c.AnonKey },
		set: func(c *config.Config, v string) { c.AnonKey = v },
	},
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config.toml field (api_url, anon_key)",
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigSet,
	}
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]
	field, ok := configFields[key]
	if !ok {
		return fmt.Errorf("unknown config key %q (expected one of: api_url, anon_key)", key)
	}
	if value == "" {
		return fmt.Errorf("value for %q must not be empty", key)
	}

	cfg, err := config.LoadRaw()
	if err != nil {
		return err
	}

	field.set(cfg, value)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s set.\n", key)
	return nil
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a config.toml field (api_url, anon_key)",
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigGet,
	}
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]
	field, ok := configFields[key]
	if !ok {
		return fmt.Errorf("unknown config key %q (expected one of: api_url, anon_key)", key)
	}

	cfg, err := config.LoadRaw()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), field.get(cfg))
	return nil
}
