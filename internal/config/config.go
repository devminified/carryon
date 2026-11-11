// Package config reads the laptop's ~/.carryon/config.toml, which points
// the binary at a backend (local, staging, production, or self-hosted) —
// the same build works against any of them by changing this one file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ErrNotConfigured is returned when ~/.carryon/config.toml doesn't exist yet.
var ErrNotConfigured = errors.New("carryon is not configured: create ~/.carryon/config.toml with api_url and anon_key")

// Config is the laptop-level backend configuration.
type Config struct {
	// APIURL is the Supabase project URL, e.g. https://xyz.supabase.co or
	// http://127.0.0.1:54421 for local dev.
	APIURL string `toml:"api_url"`
	// AnonKey is the project's anon/publishable key (safe to embed client-side).
	AnonKey string `toml:"anon_key"`
	// Repos remembers where `carryon pull` (CO-206) already found or
	// cloned a project's local working copy, so later pulls don't ask
	// again or clone a second copy. Keyed by the project's canonical
	// remote (project.NormalizeRemote's form — host/owner/repo, stable
	// across machines and remote-URL spellings), valued by an absolute
	// local path. Omitted entirely from a fresh config.toml until the
	// first pull actually needs it.
	Repos map[string]string `toml:"repos,omitempty"`

	// MachineID (CO-410) identifies this laptop install, stable across
	// restarts, for the agent's own heartbeat (internal/api.Client.
	// Heartbeat) — distinct from the signed-in user, since one
	// developer can run the agent on more than one machine and the
	// admin "Agents" page lists per-machine, not per-developer, rows.
	// Generated once, lazily, by Load() below the first time it's
	// empty; never derived from hostname/MAC (both can collide or
	// change across a reinstall) and never sent anywhere but this
	// laptop's own heartbeat calls.
	MachineID string `toml:"machine_id,omitempty"`
}

// Dir returns ~/.carryon, creating it with mode 0700 if it doesn't exist.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	dir := filepath.Join(home, ".carryon")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return dir, nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads ~/.carryon/config.toml. Returns ErrNotConfigured if it doesn't
// exist yet.
func Load() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotConfigured
	}

	var cfg Config
	if _, err := toml.DecodeFile(p, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	if cfg.APIURL == "" {
		return nil, fmt.Errorf("%s: api_url is required", p)
	}
	if cfg.AnonKey == "" {
		return nil, fmt.Errorf("%s: anon_key is required", p)
	}

	if cfg.MachineID == "" {
		id, err := generateMachineID()
		if err != nil {
			return nil, fmt.Errorf("generating machine id: %w", err)
		}
		cfg.MachineID = id
		if err := Save(&cfg); err != nil {
			return nil, fmt.Errorf("persisting generated machine id: %w", err)
		}
	}
	return &cfg, nil
}

// LoadRaw reads ~/.carryon/config.toml like Load, but skips the
// api_url/anon_key presence checks and never generates or persists a
// machine ID. For `carryon config set/get` (CO-506): a self-host
// bootstrap sets api_url and anon_key in two separate commands, so
// the file is legitimately, temporarily incomplete between them —
// Load()'s strict validation exists for every other command, which
// genuinely needs a fully-usable client, not for this one. Returns a
// zero-value Config (not an error) when the file doesn't exist yet,
// since `config set` on a brand-new machine is itself the
// bootstrapping step.
func LoadRaw() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}

	var cfg Config
	if _, err := toml.DecodeFile(p, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	return &cfg, nil
}

func generateMachineID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Save writes cfg to ~/.carryon/config.toml, mode 0600 (it's not secret,
// but there's no reason to make it world-readable either).
func Save(cfg *Config) error {
	p, err := path()
	if err != nil {
		return err
	}

	tmp := p + ".tmp"
	// tmp is built from our own computed ~/.carryon path, not external
	// input.
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmp, err)
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmp, p, err)
	}
	return nil
}
