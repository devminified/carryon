package update

import (
	"fmt"
	"os"
	"os/exec"
)

// Restart spawns a new, detached process running execPath with args,
// inheriting the environment and standard streams, and returns once
// it has successfully started (not once it exits). The caller is
// responsible for exiting the current process immediately afterward
// (os.Exit(0), after any cleanup like closing a log file) — Restart
// itself never exits the calling process, so it stays unit-testable:
// a test can assert a child actually started without that child
// taking the test binary down with it.
//
// A spawn-and-exit, rather than syscall.Exec's in-place process-image
// replacement, is used uniformly across every OS this binary targets
// — syscall.Exec has no Windows equivalent at all, and one portable
// implementation here is simpler than a build-tag split for a once-
// a-day operation where the extra process hop costs nothing anyone
// would notice.
func Restart(execPath string, args []string) error {
	cmd := exec.Command(execPath, args...) // #nosec G204 -- execPath is our own just-swapped-in binary
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("update: restarting into %s: %w", execPath, err)
	}
	// Deliberately not Wait()ed on — it's meant to outlive this
	// process, which is about to exit.
	return nil
}
