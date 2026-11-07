package ls

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/api"
)

func sess(id, project, dev, branch, title, status string, lastActivity time.Time) api.SessionSummary {
	return api.SessionSummary{
		ID: id, ProjectID: "p-" + project, ProjectName: project, OwnerLogin: dev,
		Branch: branch, Title: title, LastActivityAt: lastActivity, Status: status,
	}
}

func TestResolveShortIDExactFullMatch(t *testing.T) {
	sessions := []api.SessionSummary{
		sess("11111111-1111-4111-8111-111111111111", "acme-api", "ali", "main", "", "live", time.Time{}),
	}
	got, err := ResolveShortID(sessions, "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("ResolveShortID() error = %v", err)
	}
	if got.ID != sessions[0].ID {
		t.Errorf("got.ID = %q, want %q", got.ID, sessions[0].ID)
	}
}

func TestResolveShortIDUniquePrefix(t *testing.T) {
	sessions := []api.SessionSummary{
		sess("11111111-aaaa-4111-8111-111111111111", "acme-api", "ali", "main", "", "live", time.Time{}),
		sess("22222222-bbbb-4222-8222-222222222222", "acme-api", "sara", "main", "", "live", time.Time{}),
	}
	got, err := ResolveShortID(sessions, "11111111")
	if err != nil {
		t.Fatalf("ResolveShortID() error = %v", err)
	}
	if got.ID != sessions[0].ID {
		t.Errorf("got.ID = %q, want %q", got.ID, sessions[0].ID)
	}
}

func TestResolveShortIDIsCaseInsensitive(t *testing.T) {
	sessions := []api.SessionSummary{
		sess("AbCdEf12-1111-4111-8111-111111111111", "acme-api", "ali", "main", "", "live", time.Time{}),
	}
	got, err := ResolveShortID(sessions, "ABCDEF12")
	if err != nil {
		t.Fatalf("ResolveShortID() error = %v", err)
	}
	if got.ID != sessions[0].ID {
		t.Errorf("got.ID = %q, want %q", got.ID, sessions[0].ID)
	}
}

func TestResolveShortIDAmbiguousGivesAClearErrorListingTheMatches(t *testing.T) {
	sessions := []api.SessionSummary{
		sess("aaaaaaaa-1111-4111-8111-111111111111", "acme-api", "ali", "main", "", "live", time.Time{}),
		sess("aaaaaaab-2222-4222-8222-222222222222", "acme-web", "sara", "main", "", "live", time.Time{}),
	}
	_, err := ResolveShortID(sessions, "aaaaaaa")

	var ambiguous *AmbiguousIDError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("ResolveShortID() error = %v, want *AmbiguousIDError", err)
	}
	if len(ambiguous.Matches) != 2 {
		t.Fatalf("len(ambiguous.Matches) = %d, want 2", len(ambiguous.Matches))
	}
	msg := ambiguous.Error()
	if !strings.Contains(msg, "acme-api") || !strings.Contains(msg, "acme-web") {
		t.Errorf("error message = %q, want it to list both matching projects", msg)
	}
	if !strings.Contains(msg, "ali") || !strings.Contains(msg, "sara") {
		t.Errorf("error message = %q, want it to list both matching developers", msg)
	}
}

func TestResolveShortIDNotFound(t *testing.T) {
	sessions := []api.SessionSummary{
		sess("11111111-1111-4111-8111-111111111111", "acme-api", "ali", "main", "", "live", time.Time{}),
	}
	_, err := ResolveShortID(sessions, "zzzzzzzz")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("ResolveShortID() error = %v, want ErrSessionNotFound", err)
	}
}

func TestResolveShortIDEmptyInput(t *testing.T) {
	_, err := ResolveShortID(nil, "")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("ResolveShortID() error = %v, want ErrSessionNotFound", err)
	}
}

func TestShortID(t *testing.T) {
	if got := ShortID("11111111-1111-4111-8111-111111111111"); got != "11111111" {
		t.Errorf("ShortID() = %q, want %q", got, "11111111")
	}
	if got := ShortID("short"); got != "short" {
		t.Errorf("ShortID() = %q, want %q (unchanged, already shorter)", got, "short")
	}
}

func TestParseSinceDays(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	got, err := ParseSince("7d", now)
	if err != nil {
		t.Fatalf("ParseSince() error = %v", err)
	}
	want := now.Add(-7 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("ParseSince(7d) = %v, want %v", got, want)
	}
}

func TestParseSinceHoursMinutesSecondsWeeks(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"24h": 24 * time.Hour,
		"30m": 30 * time.Minute,
		"45s": 45 * time.Second,
		"2w":  2 * 7 * 24 * time.Hour,
	}
	for input, d := range cases {
		got, err := ParseSince(input, now)
		if err != nil {
			t.Fatalf("ParseSince(%q) error = %v", input, err)
		}
		if want := now.Add(-d); !got.Equal(want) {
			t.Errorf("ParseSince(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseSinceRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "7", "d", "7days", "-7d", "7.5d"} {
		if _, err := ParseSince(bad, time.Now()); err == nil {
			t.Errorf("ParseSince(%q) error = nil, want an error", bad)
		}
	}
}

func TestRenderEmptyList(t *testing.T) {
	got := Render(nil, time.Now())
	if got != "No sessions found.\n" {
		t.Errorf("Render(nil) = %q", got)
	}
}

func TestRenderShowsEveryColumn(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	sessions := []api.SessionSummary{
		sess("11111111-1111-4111-8111-111111111111", "acme-api", "ali", "main", "fix payments retry", "live", now.Add(-5*time.Minute)),
	}
	got := Render(sessions, now)

	for _, want := range []string{"11111111", "acme-api", "ali", "main", "fix payments retry", "5m ago", "live"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() = %q, missing %q", got, want)
		}
	}
}

func TestRenderUsesDashForEmptyFields(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	sessions := []api.SessionSummary{
		sess("11111111-1111-4111-8111-111111111111", "acme-api", "ali", "", "", "live", now),
	}
	got := Render(sessions, now)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	dataLine := lines[1]
	if strings.Count(dataLine, "-") < 2 {
		t.Errorf("Render() data line = %q, want a dash for the empty branch and title columns", dataLine)
	}
}

func TestRenderTruncatesLongTitlesWithEllipsis(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	longTitle := strings.Repeat("x", 50)
	sessions := []api.SessionSummary{
		sess("11111111-1111-4111-8111-111111111111", "acme-api", "ali", "main", longTitle, "live", now),
	}
	got := Render(sessions, now)
	if strings.Contains(got, longTitle) {
		t.Errorf("Render() = %q, want the 50-char title truncated", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("Render() = %q, want an ellipsis for the truncated title", got)
	}
}

func TestRenderFitsWithin120Columns(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	sessions := []api.SessionSummary{
		sess(
			"11111111-1111-4111-8111-111111111111",
			strings.Repeat("p", 60), strings.Repeat("d", 60), strings.Repeat("b", 60), strings.Repeat("t", 60),
			"ended", now.Add(-72*time.Hour),
		),
	}
	got := Render(sessions, now)
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if len([]rune(line)) > 120 {
			t.Errorf("line %q is %d runes, want <= 120", line, len([]rune(line)))
		}
	}
}

func TestRenderRelativeTimeGranularity(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		30 * time.Second: "30s ago",
		5 * time.Minute:  "5m ago",
		3 * time.Hour:    "3h ago",
		2 * 24 * time.Hour: "2d ago",
	}
	for d, want := range cases {
		sessions := []api.SessionSummary{sess("11111111-1111-4111-8111-111111111111", "p", "d", "b", "t", "live", now.Add(-d))}
		got := Render(sessions, now)
		if !strings.Contains(got, want) {
			t.Errorf("Render() with delta %v = %q, want it to contain %q", d, got, want)
		}
	}
}
