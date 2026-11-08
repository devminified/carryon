package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/devminified/carryon/internal/update"
)

func TestSplitOnce(t *testing.T) {
	before, after, ok := splitOnce("darwin/arm64=path/to/bin", "=")
	if !ok || before != "darwin/arm64" || after != "path/to/bin" {
		t.Errorf("splitOnce() = %q, %q, %v", before, after, ok)
	}
	if _, _, ok := splitOnce("no-separator", "="); ok {
		t.Error("splitOnce() with no separator = true, want false")
	}
}

func TestAssetFileNameIsPlatformQualified(t *testing.T) {
	// The real bug this guards against: two platforms' binaries both
	// being named "carryon" locally must never collide once uploaded
	// side by side as release assets under the same version.
	darwin := assetFileName("/any/path/carryon", "darwin", "arm64")
	linux := assetFileName("/any/path/carryon", "linux", "amd64")
	if darwin == linux {
		t.Errorf("assetFileName() collided across platforms: %q == %q", darwin, linux)
	}
	if assetFileName("/any/carryon.exe", "windows", "amd64") != "carryon_windows_amd64.exe" {
		t.Errorf("assetFileName() for a .exe = %q, want the .exe suffix preserved", assetFileName("/any/carryon.exe", "windows", "amd64"))
	}
}

func TestRunGenKeyPrintsAValidKeypair(t *testing.T) {
	if err := runGenKey(); err != nil {
		t.Errorf("runGenKey() error = %v", err)
	}
}

func TestRunSignProducesAVerifiableManifestForEveryAsset(t *testing.T) {
	dir := t.TempDir()
	darwinBin := filepath.Join(dir, "darwin_carryon")
	linuxBin := filepath.Join(dir, "linux_carryon")
	if err := os.WriteFile(darwinBin, []byte("fake darwin binary"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing fixture: %v", err)
	}
	if err := os.WriteFile(linuxBin, []byte("fake linux binary"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing fixture: %v", err)
	}

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generating keypair: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)

	out := filepath.Join(dir, "manifest.json")
	err = runSign([]string{
		"-version", "1.2.3",
		"-private-key-hex", hex.EncodeToString(priv),
		"-base-url", "https://example.com/releases/download/v1.2.3",
		"-asset", "darwin/arm64=" + darwinBin,
		"-asset", "linux/amd64=" + linuxBin,
		"-out", out,
	})
	if err != nil {
		t.Fatalf("runSign() error = %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading manifest output: %v", err)
	}
	manifest, err := update.ParseAndVerify(body, hex.EncodeToString(pub))
	if err != nil {
		t.Fatalf("ParseAndVerify() on the tool's own output: %v", err)
	}
	if manifest.Version != "1.2.3" {
		t.Errorf("manifest.Version = %q, want 1.2.3", manifest.Version)
	}
	if len(manifest.Assets) != 2 {
		t.Fatalf("manifest has %d assets, want 2", len(manifest.Assets))
	}

	urls := map[string]bool{}
	for _, a := range manifest.Assets {
		if urls[a.URL] {
			t.Errorf("duplicate asset URL %q across platforms", a.URL)
		}
		urls[a.URL] = true
	}
}

func TestRunSignRequiresAllFlags(t *testing.T) {
	if err := runSign([]string{"-version", "1.0.0"}); err == nil {
		t.Error("runSign() with missing required flags = nil error, want an error")
	}
}

func TestRunRejectsAnUnknownSubcommand(t *testing.T) {
	if err := run([]string{"bogus"}); err == nil {
		t.Error("run([bogus]) = nil error, want an error")
	}
}

func TestRunWithNoArgsErrors(t *testing.T) {
	if err := run(nil); err == nil {
		t.Error("run(nil) = nil error, want a usage error")
	}
}

// Sanity: json.Unmarshal of the tool's own output is a valid envelope
// shape (payload/signature strings), not some other accidental shape.
func TestManifestOutputIsAWellFormedEnvelope(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "carryon")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing fixture: %v", err)
	}
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generating keypair: %v", err)
	}
	out := filepath.Join(dir, "manifest.json")
	if err := runSign([]string{
		"-version", "1.0.0",
		"-private-key-hex", hex.EncodeToString(priv),
		"-base-url", "https://example.com",
		"-asset", "linux/amd64=" + bin,
		"-out", out,
	}); err != nil {
		t.Fatalf("runSign() error = %v", err)
	}

	var env map[string]string
	body, _ := os.ReadFile(out)
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("manifest output isn't valid JSON: %v", err)
	}
	if env["payload"] == "" || env["signature"] == "" {
		t.Errorf("envelope missing payload/signature: %+v", env)
	}
}
