// Package ls implements CO-204's carryon ls: rendering the session list
// as a fixed-width table (or --json), parsing the --since duration
// flag, and resolving a short or full session ID against the list the
// caller can currently see — the "short IDs accepted anywhere a session
// ID is" convention this ticket establishes, reused by later commands
// (carryon pull, CO-205+) once they take a session ID argument.
package ls

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/devminified/carryon/internal/api"
)

// shortIDWidth is CO-204's "short IDs (the first 8 characters)".
const shortIDWidth = 8

// Column widths chosen so the table fits comfortably under the ticket's
// 120-column budget (see ls_test.go's width check) even with the
// longest header ("LAST ACTIVITY").
const (
	projectWidth = 16
	devWidth     = 12
	branchWidth  = 20
	titleWidth   = 30
	lastActWidth = 13
)

// ErrSessionNotFound is returned by ResolveShortID when idOrPrefix
// matches no session in the given list.
var ErrSessionNotFound = errors.New("no session matches that id")

// AmbiguousIDError is returned by ResolveShortID when idOrPrefix is a
// prefix of more than one session's ID. Its Error() lists every match
// so the developer can tell which one they meant without re-running
// `carryon ls` first.
type AmbiguousIDError struct {
	Prefix  string
	Matches []api.SessionSummary
}

func (e *AmbiguousIDError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d sessions:\n", e.Prefix, len(e.Matches))
	for _, s := range e.Matches {
		fmt.Fprintf(&b, "  %s  %-16s  %s\n", ShortID(s.ID), displayOrDash(s.ProjectName), displayOrDash(s.OwnerLogin))
	}
	b.WriteString("Use more characters to disambiguate.")
	return b.String()
}

// ResolveShortID finds the one session in sessions whose ID is
// idOrPrefix, or whose ID starts with it (case-insensitive). An exact
// full-ID match always wins outright, even if it also happens to prefix
// another session's ID (impossible in practice — UUIDs don't collide —
// but it keeps the precedence rule simple to state). Otherwise: zero
// prefix matches is ErrSessionNotFound, more than one is
// *AmbiguousIDError.
func ResolveShortID(sessions []api.SessionSummary, idOrPrefix string) (api.SessionSummary, error) {
	needle := strings.ToLower(strings.TrimSpace(idOrPrefix))
	if needle == "" {
		return api.SessionSummary{}, fmt.Errorf("%w: empty id", ErrSessionNotFound)
	}

	for _, s := range sessions {
		if strings.ToLower(s.ID) == needle {
			return s, nil
		}
	}

	var matches []api.SessionSummary
	for _, s := range sessions {
		if strings.HasPrefix(strings.ToLower(s.ID), needle) {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return api.SessionSummary{}, fmt.Errorf("%w: %q", ErrSessionNotFound, idOrPrefix)
	case 1:
		return matches[0], nil
	default:
		return api.SessionSummary{}, &AmbiguousIDError{Prefix: idOrPrefix, Matches: matches}
	}
}

// ShortID returns id's first shortIDWidth characters, or id itself if
// it's already shorter (defensive — every real session ID is a UUID,
// always longer than shortIDWidth).
func ShortID(id string) string {
	if len(id) <= shortIDWidth {
		return id
	}
	return id[:shortIDWidth]
}

var sinceRe = regexp.MustCompile(`^(\d+)([smhdw])$`)

// ParseSince parses CO-204's --since flag ("7d", "24h", "30m", "45s",
// "2w") into the cutoff timestamp it means, relative to now. now is a
// parameter rather than time.Now() so this stays deterministic to test.
func ParseSince(value string, now time.Time) (time.Time, error) {
	m := sinceRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid --since value %q: want a number followed by s, m, h, d, or w (e.g. 7d)", value)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since value %q: %w", value, err)
	}

	var unit time.Duration
	switch m[2] {
	case "s":
		unit = time.Second
	case "m":
		unit = time.Minute
	case "h":
		unit = time.Hour
	case "d":
		unit = 24 * time.Hour
	case "w":
		unit = 7 * 24 * time.Hour
	}
	return now.Add(-time.Duration(n) * unit), nil
}

// Render formats sessions as the fixed-width table `carryon ls` prints
// by default (as opposed to --json). now is explicit, not time.Now(),
// so relative-time output is deterministic to test — same reasoning as
// internal/status.Render.
func Render(sessions []api.SessionSummary, now time.Time) string {
	var b strings.Builder

	if len(sessions) == 0 {
		return "No sessions found.\n"
	}

	writeRow(&b, "SESSION", "PROJECT", "DEVELOPER", "BRANCH", "TITLE", "LAST ACTIVITY", "STATUS")
	for _, s := range sessions {
		writeRow(
			&b,
			ShortID(s.ID),
			truncate(displayOrDash(s.ProjectName), projectWidth),
			truncate(displayOrDash(s.OwnerLogin), devWidth),
			truncate(displayOrDash(s.Branch), branchWidth),
			truncate(displayOrDash(s.Title), titleWidth),
			formatRelative(s.LastActivityAt, now),
			s.Status,
		)
	}
	return b.String()
}

func writeRow(b *strings.Builder, id, project, dev, branch, title, lastActivity, status string) {
	fmt.Fprintf(
		b, "%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
		shortIDWidth, id,
		projectWidth, project,
		devWidth, dev,
		branchWidth, branch,
		titleWidth, title,
		lastActWidth, lastActivity,
		status,
	)
}

func truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	return s[:width-1] + "…"
}

func displayOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
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
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
