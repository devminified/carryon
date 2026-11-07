package status

import (
	"fmt"
	"strings"
	"time"
)

// Render formats r as the human-readable text `carryon status` prints
// by default (as opposed to --json). now is passed explicitly, not read
// from time.Now(), so relative times ("12s ago") are deterministic —
// CO-113's own testing point calls for snapshot tests of this output
// across fixed states (logged-out, agent-down, healthy, lagging,
// blocked), which only works if Render is a pure function of its inputs.
func Render(r Report, now time.Time) string {
	var b strings.Builder

	if r.LoggedIn {
		if r.User != nil {
			fmt.Fprintf(&b, "Logged in as %s", r.User.Email)
			if r.User.UserName != "" {
				fmt.Fprintf(&b, " (%s)", r.User.UserName)
			}
			b.WriteString("\n")
		} else {
			b.WriteString("Logged in (user info unavailable — offline?)\n")
		}
	} else {
		b.WriteString("Not logged in — run `carryon login`.\n")
	}

	fmt.Fprintf(&b, "Backend: %s\n", r.BackendURL)

	if r.Agent.Running {
		fmt.Fprintf(&b, "Agent: running (v%s, up %s)\n", displayOrUnknown(r.Agent.Version), formatUptime(r.Agent.UptimeSeconds))
	} else {
		b.WriteString("Agent: not running — run `carryon install`, or start it with `carryon agent`.\n")
	}

	if len(r.Sessions) > 0 {
		b.WriteString("\nSessions:\n")
		writeSessionTable(&b, r.Sessions, now)
	}

	if len(r.SkippedRepos) > 0 {
		b.WriteString("\nSkipped repos (not registered with Carryon):\n")
		for _, repo := range r.SkippedRepos {
			fmt.Fprintf(&b, "  %s\n", repo)
		}
	}

	return b.String()
}

func writeSessionTable(b *strings.Builder, sessions []SessionInfo, now time.Time) {
	const idWidth, projectWidth, uploadWidth, behindWidth = 8, 34, 13, 9

	fmt.Fprintf(b, "  %-*s  %-*s  %-*s  %-*s  %s\n", idWidth, "SESSION", projectWidth, "PROJECT", uploadWidth, "LAST UPLOAD", behindWidth, "BEHIND", "STATUS")
	for _, s := range sessions {
		id := s.SessionID
		if len(id) > idWidth {
			id = id[:idWidth]
		}
		project := displayOrDash(s.Project)
		lastUpload := "-"
		if s.LastUploadAt != nil {
			lastUpload = formatRelative(*s.LastUploadAt, now)
		}
		statusText := "ok"
		if s.Blocked {
			statusText = "blocked: " + s.BlockedReason
		}
		if len(s.ExcludedLargeFiles) > 0 {
			statusText += fmt.Sprintf(" (%d large file(s) excluded from snapshot)", len(s.ExcludedLargeFiles))
		}
		fmt.Fprintf(b, "  %-*s  %-*s  %-*s  %-*s  %s\n", idWidth, id, projectWidth, project, uploadWidth, lastUpload, behindWidth, formatBytes(s.BytesBehind), statusText)
	}
}

func displayOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func displayOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatUptime(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatRelative(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
