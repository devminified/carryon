package auth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser opens url in the user's default browser. Login still prints
// the URL regardless, since this is best-effort — a headless or
// no-GUI environment is expected to fail here and the user copies the URL
// instead.
func openBrowser(url string) error {
	// url is passed as an argv element, never through a shell, so there's
	// no command-injection vector here — gosec's G204 can't tell argv
	// from shell interpolation apart, hence the explicit suppressions.
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) // #nosec G204
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) // #nosec G204
	default: // linux and other unix-likes
		cmd = exec.Command("xdg-open", url) // #nosec G204
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}
	return nil
}
