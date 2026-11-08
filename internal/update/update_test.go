package update

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAndApplyAppliesANewerSignedUpdateEndToEnd(t *testing.T) {
	pub, priv := testKeypair(t)
	dir := t.TempDir()

	bin := buildFixtureBinary(t, dir, "newrelease", "2.0.0")
	assetBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading fixture binary: %v", err)
	}

	var assetURL string
	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(assetBytes)
	}))
	defer assetSrv.Close()
	assetURL = assetSrv.URL

	manifest := Manifest{
		Version: "2.0.0",
		Assets: []Asset{
			{OS: "testos", Arch: "testarch", URL: assetURL, SHA256: sha256Hex(assetBytes)},
		},
	}
	envelope, err := SignManifest(manifest, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(envelope)
	}))
	defer manifestSrv.Close()

	execPath := filepath.Join(dir, "carryon")
	if err := os.WriteFile(execPath, []byte("old binary bytes"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing old exec: %v", err)
	}

	result, err := CheckAndApply(context.Background(), Config{
		ManifestURL:    manifestSrv.URL,
		PublicKeyHex:   hex.EncodeToString(pub),
		CurrentVersion: "1.0.0",
		ExecPath:       execPath,
		GOOS:           "testos",
		GOARCH:         "testarch",
	})
	if err != nil {
		t.Fatalf("CheckAndApply() error = %v", err)
	}
	if !result.Applied || result.ToVersion != "2.0.0" {
		t.Errorf("CheckAndApply() result = %+v, want Applied=true ToVersion=2.0.0", result)
	}

	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("reading execPath after update: %v", err)
	}
	if string(got) != string(assetBytes) {
		t.Error("execPath content after a successful update doesn't match the new binary's bytes")
	}
}

func TestCheckAndApplyIsANoOpWhenAlreadyCurrent(t *testing.T) {
	pub, priv := testKeypair(t)
	envelope, err := SignManifest(Manifest{Version: "1.0.0"}, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(envelope)
	}))
	defer manifestSrv.Close()

	result, err := CheckAndApply(context.Background(), Config{
		ManifestURL:    manifestSrv.URL,
		PublicKeyHex:   hex.EncodeToString(pub),
		CurrentVersion: "1.0.0",
		ExecPath:       filepath.Join(t.TempDir(), "carryon"),
	})
	if err != nil {
		t.Fatalf("CheckAndApply() error = %v", err)
	}
	if result.Applied {
		t.Error("CheckAndApply() when already current: Applied = true, want false")
	}
}

func TestCheckAndApplyRefusesATamperedManifestBeforeDownloadingAnything(t *testing.T) {
	pub, priv := testKeypair(t)
	envelope, err := SignManifest(Manifest{Version: "9.0.0"}, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}
	// Flip a byte to simulate a tampered/forged manifest in transit.
	tampered := []byte(string(envelope))
	for i, b := range tampered {
		if b == 'A' {
			tampered[i] = 'Z'
			break
		}
	}

	assetDownloaded := false
	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assetDownloaded = true
		_, _ = w.Write([]byte("should never be fetched"))
	}))
	defer assetSrv.Close()

	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tampered)
	}))
	defer manifestSrv.Close()

	dir := t.TempDir()
	execPath := filepath.Join(dir, "carryon")
	if err := os.WriteFile(execPath, []byte("original"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing exec: %v", err)
	}

	_, err = CheckAndApply(context.Background(), Config{
		ManifestURL:    manifestSrv.URL,
		PublicKeyHex:   hex.EncodeToString(pub),
		CurrentVersion: "1.0.0",
		ExecPath:       execPath,
	})
	if err == nil {
		t.Fatal("CheckAndApply() with a tampered manifest = nil error, want a signature failure")
	}
	if assetDownloaded {
		t.Error("asset URL was fetched despite the manifest signature being invalid")
	}
	got, _ := os.ReadFile(execPath)
	if string(got) != "original" {
		t.Error("execPath was modified despite the manifest signature being invalid")
	}
}

func TestCheckAndApplyRollsBackWhenTheStagedBinaryFailsItsSelfCheck(t *testing.T) {
	// This ticket's own "a failed update rolls back" testing point,
	// exercised through the full real pipeline: a correctly-signed
	// manifest and a checksum-valid asset that is nonetheless not a
	// real, working carryon binary for this version (e.g. a build
	// that silently produced the wrong thing) must never replace the
	// live executable.
	pub, priv := testKeypair(t)
	dir := t.TempDir()

	// A real binary, but one that reports the WRONG version — stands
	// in for "checksum matched what was signed, but what was signed
	// was itself broken."
	brokenBin := buildFixtureBinary(t, dir, "broken", "1.0.0")
	assetBytes, err := os.ReadFile(brokenBin)
	if err != nil {
		t.Fatalf("reading fixture binary: %v", err)
	}

	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(assetBytes)
	}))
	defer assetSrv.Close()

	manifest := Manifest{
		Version: "2.0.0", // the manifest claims 2.0.0, but the binary reports 1.0.0
		Assets: []Asset{
			{OS: "testos", Arch: "testarch", URL: assetSrv.URL, SHA256: sha256Hex(assetBytes)},
		},
	}
	envelope, err := SignManifest(manifest, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(envelope)
	}))
	defer manifestSrv.Close()

	execPath := filepath.Join(dir, "carryon-live")
	if err := os.WriteFile(execPath, []byte("original live binary"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing exec: %v", err)
	}

	_, err = CheckAndApply(context.Background(), Config{
		ManifestURL:    manifestSrv.URL,
		PublicKeyHex:   hex.EncodeToString(pub),
		CurrentVersion: "1.0.0",
		ExecPath:       execPath,
		GOOS:           "testos",
		GOARCH:         "testarch",
	})
	if err == nil {
		t.Fatal("CheckAndApply() with a self-check failure = nil error, want a failure")
	}

	got, readErr := os.ReadFile(execPath)
	if readErr != nil {
		t.Fatalf("execPath is gone after a failed update: %v", readErr)
	}
	if string(got) != "original live binary" {
		t.Error("execPath was replaced despite the staged binary failing its self-check — rollback didn't hold")
	}
	if _, err := os.Stat(execPath + ".staged"); !os.IsNotExist(err) {
		t.Error("a failed staged binary was left on disk instead of being cleaned up")
	}
}

func TestCheckAndApplyRespectsAPinThatBlocksTheLatestVersion(t *testing.T) {
	pub, priv := testKeypair(t)
	envelope, err := SignManifest(Manifest{Version: "9.0.0"}, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(envelope)
	}))
	defer manifestSrv.Close()

	result, err := CheckAndApply(context.Background(), Config{
		ManifestURL:    manifestSrv.URL,
		PublicKeyHex:   hex.EncodeToString(pub),
		CurrentVersion: "1.0.0",
		PinnedVersion:  "1.5.0",
		ExecPath:       filepath.Join(t.TempDir(), "carryon"),
	})
	if err != nil {
		t.Fatalf("CheckAndApply() error = %v", err)
	}
	if result.Applied {
		t.Error("CheckAndApply() with latest exceeding the pin: Applied = true, want false")
	}
}

func TestCheckAndApplyWithNoPublicKeyIsANoOp(t *testing.T) {
	result, err := CheckAndApply(context.Background(), Config{
		ManifestURL:    "http://unused.invalid/manifest.json",
		PublicKeyHex:   "",
		CurrentVersion: "1.0.0",
		ExecPath:       filepath.Join(t.TempDir(), "carryon"),
		HTTPClient: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("no HTTP call should happen when update checking is disabled")
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("CheckAndApply() error = %v", err)
	}
	if result.Applied {
		t.Error("CheckAndApply() with no public key: Applied = true, want false")
	}
}

// roundTripperFunc adapts a func to httpClient for the one test above
// that must assert no network call happens at all.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
