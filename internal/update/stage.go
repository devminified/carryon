package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// httpClient is the subset of *http.Client this package needs —
// tests inject a fake to serve fixture bytes without a real network
// call for every case, and the one real end-to-end test
// (cmd/carryon/update_e2e_test.go, if present) uses httptest.Server
// with a real *http.Client.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// download fetches url's full body. Kept tiny and separate so Stage's
// own logic (checksum, then self-check, then swap — never any other
// order) is the part worth reading closely.
func download(ctx context.Context, client httpClient, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("update: building request for %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: downloading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: downloading %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Stage downloads asset, verifies its checksum against the manifest's
// signed value, writes it to a new executable file in the same
// directory as execPath (so the later swap is a same-filesystem
// rename, not a cross-device copy), and returns that staged path.
// Nothing at execPath itself is touched yet.
func Stage(ctx context.Context, client httpClient, asset Asset, execPath string) (string, error) {
	data, err := download(ctx, client, asset.URL)
	if err != nil {
		return "", err
	}
	if err := verifyChecksum(data, asset.SHA256); err != nil {
		return "", err
	}

	dir := filepath.Dir(execPath)
	staged := filepath.Join(dir, filepath.Base(execPath)+".staged")
	if err := os.WriteFile(staged, data, 0o755); err != nil { //nolint:gosec // an executable genuinely needs the x bit
		return "", fmt.Errorf("update: writing staged binary: %w", err)
	}
	return staged, nil
}

// ErrStagedBinarySelfCheckFailed means a downloaded, checksum-valid
// binary didn't pass its own --version sanity check — this is what
// makes "a failed update rolls back" true without any actual rollback
// step: SelfCheck always runs before Swap, so a binary that fails it
// never replaces the live one in the first place.
var ErrStagedBinarySelfCheckFailed = fmt.Errorf("update: staged binary failed its self-check")

// selfCheckTimeout bounds the staged binary's own --version
// invocation — a hung or misbehaving downloaded binary must not stall
// the daily update check indefinitely.
const selfCheckTimeout = 10 * time.Second

// SelfCheck runs `stagedPath version` and confirms its output names
// wantVersion — proof the staged bytes are a real, runnable carryon
// binary reporting the version the manifest promised, not just
// something that happened to pass its checksum.
func SelfCheck(ctx context.Context, stagedPath, wantVersion string) error {
	ctx, cancel := context.WithTimeout(ctx, selfCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, stagedPath, "version") // #nosec G204 -- stagedPath is our own just-downloaded, checksum-verified binary
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStagedBinarySelfCheckFailed, err)
	}
	if !strings.Contains(string(out), wantVersion) {
		return fmt.Errorf("%w: output %q doesn't mention version %q", ErrStagedBinarySelfCheckFailed, out, wantVersion)
	}
	return nil
}

// Swap atomically replaces execPath with the already-staged,
// already-self-checked binary at stagedPath. Only ever called once
// SelfCheck has already passed — see CheckAndApply's own ordering. The
// previous binary is kept alongside as execPath+".old" on a
// best-effort basis (removal failing — e.g. Windows still holding it
// open while this very process runs from it — is not an error; a
// leftover .old is harmless and gets overwritten by the next update).
func Swap(execPath, stagedPath string) error {
	oldPath := execPath + ".old"
	_ = os.Remove(oldPath) // clean up any leftover from a previous update, best-effort

	if err := os.Rename(execPath, oldPath); err != nil {
		return fmt.Errorf("update: moving current binary aside: %w", err)
	}
	if err := os.Rename(stagedPath, execPath); err != nil {
		// Try to restore the original so a failed swap never leaves
		// the laptop with no working binary at all.
		_ = os.Rename(oldPath, execPath)
		return fmt.Errorf("update: staging new binary into place: %w", err)
	}
	_ = os.Remove(oldPath) // best-effort; see doc comment above
	return nil
}

// CleanupStaged removes a staged binary that failed its checksum or
// self-check — CheckAndApply's own error paths call this so a rejected
// download never lingers on disk.
func CleanupStaged(stagedPath string) {
	if stagedPath != "" {
		_ = os.Remove(stagedPath)
	}
}

// CurrentPlatform is runtime.GOOS/GOARCH, as a convenience for callers
// building a Config — kept here (not inlined at every call site) so a
// test can override it without touching the runtime package.
func CurrentPlatform() (goos, goarch string) {
	return runtime.GOOS, runtime.GOARCH
}
