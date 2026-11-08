// Package update implements CO-308's daily self-update check: fetch a
// signed release manifest, verify its signature and the target asset's
// checksum, stage the new binary and confirm it actually runs *before*
// ever touching the live executable, then atomically swap it in and
// restart. "Verify before swap" is what makes a failed update a no-op
// (this ticket's own "a failed update rolls back" testing point) rather
// than something that needs an explicit rollback step — nothing is ever
// changed until every check has already passed.
package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Asset is one platform's downloadable binary for a release.
type Asset struct {
	OS     string `json:"os"`   // runtime.GOOS value: "darwin", "linux", "windows"
	Arch   string `json:"arch"` // runtime.GOARCH value: "amd64", "arm64"
	URL    string `json:"url"`
	SHA256 string `json:"sha256"` // lowercase hex, of the raw (uncompressed) binary
}

// Manifest is the release manifest's signed payload.
type Manifest struct {
	Version string  `json:"version"`
	Assets  []Asset `json:"assets"`
}

// envelope is the actual bytes a manifest URL serves: the payload
// (Manifest's own JSON, exactly as signed — never re-marshaled before
// verifying) plus a detached signature over those exact bytes. Signing
// the literal transmitted bytes, rather than some canonical
// re-encoding of the parsed struct, avoids any JSON-canonicalization
// ambiguity entirely.
type envelope struct {
	Payload   string `json:"payload"`   // base64 of the Manifest's JSON bytes
	Signature string `json:"signature"` // base64 of the ed25519 signature over those bytes
}

// ErrInvalidSignature means the envelope's signature didn't verify
// against the configured public key — the manifest (and therefore
// every asset it names) must not be trusted at all. This is the "a
// tampered binary is refused" testing point's first line of defense:
// an attacker who can't forge a signature can't get this far
// regardless of what bytes they serve at an asset URL.
var ErrInvalidSignature = fmt.Errorf("update: manifest signature verification failed")

// ParseAndVerify decodes body as a signed envelope, verifies its
// signature against publicKeyHex (a hex-encoded ed25519 public key,
// normally baked in at build time via -ldflags, same mechanism as
// main.version — see cmd/carryon/main.go), and returns the parsed
// Manifest only once that's passed. publicKeyHex == "" means update
// checking is disabled entirely (no production signing key has been
// provisioned yet — see docs/co-308-fleet-rollout.md) — returns
// ErrUpdatesDisabled, never silently trusts an unsigned manifest.
var ErrUpdatesDisabled = fmt.Errorf("update: no public key configured, update checking is disabled")

func ParseAndVerify(body []byte, publicKeyHex string) (*Manifest, error) {
	if publicKeyHex == "" {
		return nil, ErrUpdatesDisabled
	}
	pubKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("update: invalid public key configured: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("update: parsing manifest envelope: %w", err)
	}

	payloadBytes, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil, fmt.Errorf("update: decoding manifest payload: %w", err)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return nil, fmt.Errorf("update: decoding manifest signature: %w", err)
	}

	if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), payloadBytes, sigBytes) {
		return nil, ErrInvalidSignature
	}

	var m Manifest
	if err := json.Unmarshal(payloadBytes, &m); err != nil {
		return nil, fmt.Errorf("update: parsing manifest payload: %w", err)
	}
	return &m, nil
}

// SignManifest is the release-side counterpart to ParseAndVerify —
// used by the CI tooling that builds and signs a release's manifest
// (cmd/carryon-sign-manifest, scripts/, or a one-off invocation), never
// by the laptop binary itself. Exported from this package rather than
// duplicated, so both sides of the signature always agree on the exact
// envelope shape.
func SignManifest(m Manifest, privateKey ed25519.PrivateKey) ([]byte, error) {
	payloadBytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("update: marshaling manifest payload: %w", err)
	}
	sig := ed25519.Sign(privateKey, payloadBytes)
	env := envelope{
		Payload:   base64.StdEncoding.EncodeToString(payloadBytes),
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
	return json.MarshalIndent(env, "", "  ")
}

// FindAsset returns the asset matching goos/goarch, or false if the
// manifest has none for this platform.
func FindAsset(m *Manifest, goos, goarch string) (Asset, bool) {
	for _, a := range m.Assets {
		if a.OS == goos && a.Arch == goarch {
			return a, true
		}
	}
	return Asset{}, false
}

// verifyChecksum hashes data and compares it (case-insensitively) to
// wantHex. This is the second line of defense after the manifest
// signature: even a correctly-signed manifest's asset URL could be
// re-pointed by a MITM or a compromised CDN after the fact, so the
// downloaded bytes themselves are checked against the signed hash,
// not just "some file downloaded without a transport error."
func verifyChecksum(data []byte, wantHex string) error {
	got := sha256.Sum256(data)
	gotHex := hex.EncodeToString(got[:])
	if !strings.EqualFold(gotHex, wantHex) {
		return fmt.Errorf("update: %w (want sha256 %s, got %s)", ErrChecksumMismatch, wantHex, gotHex)
	}
	return nil
}

// ErrChecksumMismatch means the downloaded bytes don't match the
// manifest's signed sha256 — the other half of "a tampered binary is
// refused."
var ErrChecksumMismatch = fmt.Errorf("checksum mismatch")
