package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Config is everything CheckAndApply needs. ManifestURL and
// PublicKeyHex are normally baked-in build-time defaults
// (cmd/carryon/main.go's ldflags, same mechanism as main.version);
// every other field is overridable for tests.
type Config struct {
	ManifestURL    string
	PublicKeyHex   string // "" disables update checking entirely — see ParseAndVerify
	CurrentVersion string
	PinnedVersion  string // "" means no pin; fetched by the caller from get-config
	ExecPath       string
	GOOS, GOARCH   string // defaults to runtime.GOOS/GOARCH if empty
	HTTPClient     httpClient
}

func (c *Config) applyDefaults() {
	if c.GOOS == "" || c.GOARCH == "" {
		goos, goarch := CurrentPlatform()
		if c.GOOS == "" {
			c.GOOS = goos
		}
		if c.GOARCH == "" {
			c.GOARCH = goarch
		}
	}
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
}

// Result is CheckAndApply's outcome for logging/status purposes.
type Result struct {
	Applied     bool
	FromVersion string
	ToVersion   string
}

// CheckAndApply runs one full update cycle: fetch and verify the
// manifest, decide whether an update is warranted (ShouldUpdate),
// stage and self-check the target asset, and only then swap it into
// place. Returns Result{Applied: false} with a nil error for every
// "nothing to do" case (updates disabled, already current, blocked by
// a pin, no asset for this platform) — those aren't failures, just
// no-ops. An error means something that SHOULD have been checked
// failed (bad signature, checksum mismatch, failed self-check,
// swap I/O error); in every one of those cases the live binary at
// ExecPath is guaranteed untouched.
func CheckAndApply(ctx context.Context, cfg Config) (Result, error) {
	cfg.applyDefaults()

	if cfg.PublicKeyHex == "" {
		return Result{}, nil // no production signing key provisioned yet — see ParseAndVerify's doc comment
	}

	body, err := download(ctx, cfg.HTTPClient, cfg.ManifestURL)
	if err != nil {
		return Result{}, err
	}

	manifest, err := ParseAndVerify(body, cfg.PublicKeyHex)
	if err != nil {
		if errors.Is(err, ErrUpdatesDisabled) {
			return Result{}, nil
		}
		return Result{}, err
	}

	if !ShouldUpdate(cfg.CurrentVersion, manifest.Version, cfg.PinnedVersion) {
		return Result{}, nil
	}

	asset, ok := FindAsset(manifest, cfg.GOOS, cfg.GOARCH)
	if !ok {
		return Result{}, fmt.Errorf("update: manifest %s has no asset for %s/%s", manifest.Version, cfg.GOOS, cfg.GOARCH)
	}

	staged, err := Stage(ctx, cfg.HTTPClient, asset, cfg.ExecPath)
	if err != nil {
		return Result{}, err
	}

	if err := SelfCheck(ctx, staged, manifest.Version); err != nil {
		CleanupStaged(staged)
		return Result{}, err
	}

	if err := Swap(cfg.ExecPath, staged); err != nil {
		CleanupStaged(staged)
		return Result{}, err
	}

	return Result{Applied: true, FromVersion: cfg.CurrentVersion, ToVersion: manifest.Version}, nil
}
