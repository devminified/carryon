//go:build windows

package agent

import (
	"log/slog"

	"golang.org/x/sys/windows"
)

// lowerPriority best-effort sets this process to the below-normal
// priority class — the Windows equivalent of priority_unix.go's nice 10,
// per CO-107's "runs at reduced priority" requirement.
func lowerPriority() {
	h, err := windows.GetCurrentProcess()
	if err != nil {
		slog.Warn("could not lower process priority", "error", err)
		return
	}
	if err := windows.SetPriorityClass(h, windows.BELOW_NORMAL_PRIORITY_CLASS); err != nil {
		slog.Warn("could not lower process priority", "error", err)
	}
}
