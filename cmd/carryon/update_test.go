package main

// CO-308's own wiring test for cmd/carryon's glue around
// internal/update — the manifest fetch/verify/stage/self-check/swap
// mechanism itself is already thoroughly real-tested in
// internal/update's own test suite (httptest.Server-served signed
// manifests and real fixture binaries); what's specific to this
// package is: the build-time ldflags vars (updatePublicKeyHex/
// updateManifestURL), lazyAPI.pinnedVersion's cache, and checkOnce's
// control flow around them.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/update"
)

// seededLazyAPI returns a *lazyAPI whose pinnedVersion cache is
// pre-populated, so pinnedVersion(ctx) returns pinned without ever
// calling resolve() (which needs a real ~/.carryon/config.toml and a
// logged-in session). Setting the cache fields directly is legitimate
// here — this test's subject is checkOnce's control flow, not
// lazyAPI's own network fetch (separately covered below).
func seededLazyAPI(pinned string) *lazyAPI {
	return &lazyAPI{
		cachedPinnedVersion:  pinned,
		pinnedVersionAt:      time.Now(),
		pinnedVersionFetched: true,
	}
}

func TestLazyAPIPinnedVersionFailsGracefullyWhenNotConfigured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	lazy := &lazyAPI{}
	if _, err := lazy.pinnedVersion(context.Background()); err == nil {
		t.Fatal("pinnedVersion() on an unconfigured machine = nil error, want a graceful failure")
	}
}

// buildFixtureCarryonBinary compiles a tiny standalone binary standing
// in for a real carryon release build — `version` prints a line
// containing the given version string, SelfCheck's own contract (same
// technique internal/update's own tests and cmd/carryon's e2e tests
// use for their fixture binaries).
func buildFixtureCarryonBinary(t *testing.T, dir, version string) string {
	t.Helper()
	src := fmt.Sprintf(`package main

import "fmt"
import "os"

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "version" {
		fmt.Println("carryon %s (commit test, built test)")
		return
	}
	os.Exit(1)
}
`, version)
	srcPath := filepath.Join(dir, "fixture.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("writing fixture source: %v", err)
	}
	exePath := filepath.Join(dir, "fixture-carryon")
	if runtime.GOOS == "windows" {
		exePath += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", exePath, srcPath).CombinedOutput() // #nosec G204 -- fixed go-toolchain invocation, test-only
	if err != nil {
		t.Fatalf("building fixture binary: %v\n%s", err, out)
	}
	return exePath
}

func TestCheckOnceAppliesAnUpdateAndCallsRestartThenExit(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generating test keypair: %v", err)
	}
	dir := t.TempDir()

	bin := buildFixtureCarryonBinary(t, dir, "2.0.0")
	assetBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading fixture binary: %v", err)
	}
	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(assetBytes)
	}))
	defer assetSrv.Close()

	// checkOnce has no way to inject GOOS/GOARCH (update.Config derives
	// them from the real runtime when left empty), so the fixture
	// manifest must name the real platform this test actually runs on.
	sum := sha256.Sum256(assetBytes)
	manifest := update.Manifest{
		Version: "2.0.0",
		Assets: []update.Asset{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: assetSrv.URL, SHA256: hex.EncodeToString(sum[:])},
		},
	}
	envelope, err := update.SignManifest(manifest, priv)
	if err != nil {
		t.Fatalf("SignManifest() error = %v", err)
	}
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(envelope)
	}))
	defer manifestSrv.Close()

	execPath := filepath.Join(dir, "carryon-under-test")
	if err := os.WriteFile(execPath, []byte("old binary"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing fake exec: %v", err)
	}

	origManifestURL, origPublicKey, origVersion := updateManifestURL, updatePublicKeyHex, version
	updateManifestURL = manifestSrv.URL
	updatePublicKeyHex = hex.EncodeToString(pub)
	version = "1.0.0"
	t.Cleanup(func() {
		updateManifestURL, updatePublicKeyHex, version = origManifestURL, origPublicKey, origVersion
	})

	var restartedPath string
	exitCode := -1
	restart := func(p string, _ []string) error { restartedPath = p; return nil }
	exit := func(code int) { exitCode = code }

	checkOnce(context.Background(), seededLazyAPI(""), execPath, restart, exit)

	if restartedPath != execPath {
		t.Errorf("restart was called with %q, want %q", restartedPath, execPath)
	}
	if exitCode != 0 {
		t.Errorf("exit was called with %d, want 0", exitCode)
	}
	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("reading execPath after update: %v", err)
	}
	if string(got) != string(assetBytes) {
		t.Error("execPath wasn't actually swapped to the new binary's bytes")
	}
}

func TestCheckOnceSkipsGracefullyWhenThePinnedVersionLookupFails(t *testing.T) {
	// Deliberately not relying on whatever real ~/.carryon/config.toml
	// this dev machine happens to have — pointed at a fresh, never-
	// configured HOME so lazyAPI.resolve() fails the same
	// deterministic way everywhere (CI included), not a refresh-token
	// failure contingent on this one machine's local login state.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	calledRestart := false
	restart := func(string, []string) error { calledRestart = true; return nil }
	exit := func(int) { t.Fatal("exit should never be called when the pin lookup fails") }

	checkOnce(context.Background(), &lazyAPI{}, "/nonexistent/carryon", restart, exit)

	if calledRestart {
		t.Error("restart was called despite the pin lookup failing")
	}
}
