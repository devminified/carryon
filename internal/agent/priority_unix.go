//go:build unix

package agent

import (
	"log/slog"
	"syscall"
)

// lowerPriority best-effort lowers this process's OS scheduling priority
// (nice 10) so the agent doesn't compete with the developer's foreground
// work for CPU, per CO-107's "runs at reduced priority" requirement.
// Failing to lower priority (e.g. a sandboxed environment that denies it)
// is logged and otherwise ignored — it's a nice-to-have, not worth failing
// the agent over.
func lowerPriority() {
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10); err != nil {
		slog.Warn("could not lower process priority", "error", err)
	}
}
