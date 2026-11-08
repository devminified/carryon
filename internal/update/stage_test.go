package update

import (
	"context"
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
)

// buildFixtureBinary compiles a tiny standalone binary standing in for
// a real carryon release build — `version` prints a line containing
// the given version string (SelfCheck's own contract), same technique
// cmd/carryon's own e2e tests use for a fake claude binary.
func buildFixtureBinary(t *testing.T, dir, name, version string) string {
	t.Helper()
	srcPath := filepath.Join(dir, name+".go")
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
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("writing fixture source: %v", err)
	}

	exePath := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		exePath += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", exePath, srcPath).CombinedOutput() // #nosec G204 -- fixed go-toolchain invocation, test-only
	if err != nil {
		t.Fatalf("building fixture binary: %v\n%s", err, out)
	}
	return exePath
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestStageDownloadsVerifiesChecksumAndWritesAnExecutableFile(t *testing.T) {
	payload := []byte("fake binary bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	execPath := filepath.Join(dir, "carryon")
	if err := os.WriteFile(execPath, []byte("old binary"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing fake exec: %v", err)
	}

	asset := Asset{URL: srv.URL, SHA256: sha256Hex(payload)}
	staged, err := Stage(context.Background(), http.DefaultClient, asset, execPath)
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	defer CleanupStaged(staged)

	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("reading staged file: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("staged content = %q, want %q", got, payload)
	}
	if filepath.Dir(staged) != dir {
		t.Errorf("staged path %q not in the same directory as execPath (%q) — swap wouldn't be atomic", staged, dir)
	}
}

func TestStageRejectsAChecksumMismatch(t *testing.T) {
	// This ticket's own "a tampered binary is refused" testing point,
	// the download-time half: even with a validly-signed manifest, a
	// MITM'd or compromised-CDN asset URL serving different bytes than
	// what was signed must never be accepted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered bytes, not what was signed"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	execPath := filepath.Join(dir, "carryon")
	asset := Asset{URL: srv.URL, SHA256: sha256Hex([]byte("the real, signed bytes"))}

	_, err := Stage(context.Background(), http.DefaultClient, asset, execPath)
	if err == nil {
		t.Fatal("Stage() with a checksum mismatch = nil error, want ErrChecksumMismatch")
	}
}

func TestSelfCheckAcceptsARealBinaryReportingTheRightVersion(t *testing.T) {
	dir := t.TempDir()
	bin := buildFixtureBinary(t, dir, "goodbinary", "9.9.9")

	if err := SelfCheck(context.Background(), bin, "9.9.9"); err != nil {
		t.Errorf("SelfCheck() error = %v, want nil", err)
	}
}

func TestSelfCheckRejectsABinaryReportingTheWrongVersion(t *testing.T) {
	// This ticket's own "a failed update rolls back" testing point:
	// SelfCheck is what Swap is gated behind, so a binary that doesn't
	// pass this never gets anywhere near replacing the live one.
	dir := t.TempDir()
	bin := buildFixtureBinary(t, dir, "wrongversion", "1.0.0")

	if err := SelfCheck(context.Background(), bin, "2.0.0"); err == nil {
		t.Error("SelfCheck() for a binary reporting the wrong version = nil error, want a failure")
	}
}

func TestSelfCheckRejectsANonExecutableFile(t *testing.T) {
	dir := t.TempDir()
	notABinary := filepath.Join(dir, "notabinary")
	if err := os.WriteFile(notABinary, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing fixture: %v", err)
	}
	if err := SelfCheck(context.Background(), notABinary, "1.0.0"); err == nil {
		t.Error("SelfCheck() for a script that exits 1 = nil error, want a failure")
	}
}

func TestSwapReplacesTheExecutableAndCleansUpTheBackup(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "carryon")
	if err := os.WriteFile(execPath, []byte("old version"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing old exec: %v", err)
	}
	staged := filepath.Join(dir, "carryon.staged")
	if err := os.WriteFile(staged, []byte("new version"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing staged exec: %v", err)
	}

	if err := Swap(execPath, staged); err != nil {
		t.Fatalf("Swap() error = %v", err)
	}

	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("reading execPath after swap: %v", err)
	}
	if string(got) != "new version" {
		t.Errorf("execPath content after Swap() = %q, want %q", got, "new version")
	}
	if _, err := os.Stat(execPath + ".old"); !os.IsNotExist(err) {
		t.Errorf("execPath+.old still exists after a successful swap (err = %v), want it cleaned up", err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("staged path still exists after Swap(), want it moved")
	}
}

func TestSwapFailsCleanlyWhenTheStagedFileIsMissing(t *testing.T) {
	// Simulates a failure partway through — execPath must end up
	// restored to its original content, never left missing.
	dir := t.TempDir()
	execPath := filepath.Join(dir, "carryon")
	if err := os.WriteFile(execPath, []byte("original"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("writing exec: %v", err)
	}

	err := Swap(execPath, filepath.Join(dir, "does-not-exist"))
	if err == nil {
		t.Fatal("Swap() with a missing staged file = nil error, want a failure")
	}

	got, readErr := os.ReadFile(execPath)
	if readErr != nil {
		t.Fatalf("execPath is gone after a failed Swap(): %v", readErr)
	}
	if string(got) != "original" {
		t.Errorf("execPath content after a failed Swap() = %q, want the original restored", got)
	}
}
