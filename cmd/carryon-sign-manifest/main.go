// Command carryon-sign-manifest is CO-308's release-side tooling —
// never shipped to a developer's laptop, never imported by the
// carryon binary itself. It has two jobs, matching
// internal/update.SignManifest's own doc comment on why this logic
// lives in that package (so both sides of the signature always agree
// on the exact envelope shape):
//
//   - genkey: generates a new Ed25519 keypair, once, by a human, for a
//     given environment (staging/production) — the private key is
//     then stored as a CI secret (e.g. UPDATE_SIGNING_PRIVATE_KEY_HEX
//     in GitHub Actions), and the public key is baked into every
//     release build via -ldflags (see .goreleaser.yaml's
//     updatePublicKeyHex). This is the one-time external setup this
//     ticket's own scope decision deferred — see
//     docs/co-308-fleet-rollout.md.
//   - sign: run by release.yml on every tag push, after goreleaser has
//     built the bare per-platform binaries — reads each one, computes
//     its sha256, builds a Manifest, signs it with the private key
//     from the environment, and writes the signed envelope to a file
//     uploaded as a release asset (manifest.json).
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/devminified/carryon/internal/update"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "carryon-sign-manifest:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: carryon-sign-manifest <genkey|sign> [flags]")
	}

	switch args[0] {
	case "genkey":
		return runGenKey()
	case "sign":
		return runSign(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q (want genkey or sign)", args[0])
	}
}

func runGenKey() error {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return fmt.Errorf("generating keypair: %w", err)
	}
	fmt.Printf("public (bake into -ldflags updatePublicKeyHex):  %s\n", hex.EncodeToString(pub))
	fmt.Printf("private (store ONLY as a CI secret, never commit): %s\n", hex.EncodeToString(priv))
	return nil
}

// assetFlag collects repeated -asset os/arch=path flags.
type assetFlag struct {
	entries []string
}

func (a *assetFlag) String() string { return fmt.Sprintf("%v", a.entries) }
func (a *assetFlag) Set(v string) error {
	a.entries = append(a.entries, v)
	return nil
}

func runSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	version := fs.String("version", "", "release version, e.g. 1.2.3 (required)")
	privateKeyHex := fs.String("private-key-hex", "", "hex-encoded ed25519 private key (required; normally read from an env var by the caller, never passed literally in a CI log)")
	baseURL := fs.String("base-url", "", "URL prefix each asset is reachable at once uploaded, e.g. https://github.com/devminified/carryon/releases/download/v1.2.3 (required)")
	out := fs.String("out", "manifest.json", "path to write the signed manifest envelope to")
	var assets assetFlag
	fs.Var(&assets, "asset", "repeatable: os/arch=local-file-path, e.g. darwin/arm64=dist/carryon_darwin_arm64/carryon")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *version == "" || *privateKeyHex == "" || *baseURL == "" || len(assets.entries) == 0 {
		return fmt.Errorf("-version, -private-key-hex, -base-url, and at least one -asset are all required")
	}

	privBytes, err := hex.DecodeString(*privateKeyHex)
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid -private-key-hex: %w", err)
	}

	manifest := update.Manifest{Version: *version}
	for _, entry := range assets.entries {
		platform, path, ok := splitOnce(entry, "=")
		if !ok {
			return fmt.Errorf("invalid -asset %q, want os/arch=path", entry)
		}
		goos, goarch, ok := splitOnce(platform, "/")
		if !ok {
			return fmt.Errorf("invalid -asset platform %q, want os/arch", platform)
		}

		data, err := os.ReadFile(path) // #nosec G304 -- local release-build artifact path, operator-supplied CLI arg
		if err != nil {
			return fmt.Errorf("reading asset %q: %w", path, err)
		}

		manifest.Assets = append(manifest.Assets, update.Asset{
			OS:     goos,
			Arch:   goarch,
			URL:    *baseURL + "/" + assetFileName(path, goos, goarch),
			SHA256: sha256Hex(data),
		})
	}

	envelope, err := update.SignManifest(manifest, ed25519.PrivateKey(privBytes))
	if err != nil {
		return fmt.Errorf("signing manifest: %w", err)
	}
	if err := os.WriteFile(*out, envelope, 0o644); err != nil { //nolint:gosec // a public, signed manifest — world-readable is fine
		return fmt.Errorf("writing %s: %w", *out, err)
	}
	fmt.Printf("wrote %s (version %s, %d asset(s))\n", *out, *version, len(manifest.Assets))
	return nil
}
