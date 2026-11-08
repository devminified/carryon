package update

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func testKeypair(t *testing.T) (pub ed25519.PublicKey, priv ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generating test keypair: %v", err)
	}
	return pub, priv
}

func TestSignManifestAndParseAndVerifyRoundTrip(t *testing.T) {
	pub, priv := testKeypair(t)
	want := Manifest{
		Version: "1.2.3",
		Assets: []Asset{
			{OS: "darwin", Arch: "arm64", URL: "https://example.com/carryon_darwin_arm64.tar.gz", SHA256: "deadbeef"},
		},
	}

	body, err := SignManifest(want, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}

	got, err := ParseAndVerify(body, hex.EncodeToString(pub))
	if err != nil {
		t.Fatalf("ParseAndVerify() error = %v", err)
	}
	if got.Version != want.Version || len(got.Assets) != 1 || got.Assets[0] != want.Assets[0] {
		t.Errorf("ParseAndVerify() = %+v, want %+v", got, want)
	}
}

func TestParseAndVerifyRejectsATamperedSignature(t *testing.T) {
	// This ticket's own "a tampered binary is refused" testing point —
	// the first line of defense, before any asset is ever downloaded.
	pub, priv := testKeypair(t)
	body, err := SignManifest(Manifest{Version: "1.0.0"}, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}

	// Flip a byte in the middle of the envelope's signature field to
	// simulate a forged/corrupted manifest without constructing one by
	// hand — any single-byte change to a valid base64 signature makes
	// it fail ed25519.Verify.
	tampered := []byte(string(body))
	for i, b := range tampered {
		if b == 'A' {
			tampered[i] = 'B'
			break
		}
	}

	if _, err := ParseAndVerify(tampered, hex.EncodeToString(pub)); err == nil {
		t.Error("ParseAndVerify(tampered signature) = nil error, want a verification failure")
	}
}

func TestParseAndVerifyRejectsAManifestSignedByADifferentKey(t *testing.T) {
	_, attackerPriv := testKeypair(t)
	realPub, _ := testKeypair(t)

	body, err := SignManifest(Manifest{Version: "1.0.0"}, attackerPriv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}

	_, err = ParseAndVerify(body, hex.EncodeToString(realPub))
	if err == nil {
		t.Error("ParseAndVerify(wrong signer) = nil error, want ErrInvalidSignature")
	}
}

func TestParseAndVerifyWithNoPublicKeyConfiguredDisablesUpdates(t *testing.T) {
	_, priv := testKeypair(t)
	body, err := SignManifest(Manifest{Version: "1.0.0"}, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}
	_, err = ParseAndVerify(body, "")
	if err != ErrUpdatesDisabled {
		t.Errorf("ParseAndVerify(no public key) error = %v, want ErrUpdatesDisabled", err)
	}
}

func TestFindAsset(t *testing.T) {
	m := &Manifest{Assets: []Asset{
		{OS: "darwin", Arch: "arm64", URL: "a"},
		{OS: "linux", Arch: "amd64", URL: "b"},
	}}
	got, ok := FindAsset(m, "linux", "amd64")
	if !ok || got.URL != "b" {
		t.Errorf("FindAsset(linux, amd64) = %+v, %v", got, ok)
	}
	if _, ok := FindAsset(m, "windows", "arm64"); ok {
		t.Error("FindAsset(windows, arm64) = true, want false (no such asset)")
	}
}
