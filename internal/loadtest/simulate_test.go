package loadtest

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/project"
)

// These match cmd/carryon's own e2e_test.go — the same local Supabase
// CLI dev stack, the same seeded users, the same "skip, don't fail, if
// the stack isn't up" discipline every real-local-Supabase test in
// this codebase follows.
const (
	ltAPIURL    = "http://127.0.0.1:54421"
	ltAnonKey   = "sb_publishable_ACJWlzQHlZjBrEguHvfOxg_3BJgxAaH"
	ltDBURL     = "postgresql://postgres:postgres@127.0.0.1:54422/postgres"
	ltAliEmail  = "ali@devminified.com"
	ltSaraEmail = "sara@devminified.com"
	ltPassword  = "password123"
	ltSaraID    = "22222222-2222-2222-2222-222222222222"
)

func ltPsql(t *testing.T, sql string) {
	t.Helper()
	out, err := exec.Command("psql", ltDBURL, "-v", "ON_ERROR_STOP=1", "-c", sql).CombinedOutput() // #nosec G204 -- fixed local dev DB URL, test-only
	if err != nil {
		t.Fatalf("psql %q: %v\n%s", sql, err, out)
	}
}

// TestRunEndToEndAgainstLocalSupabase proves the whole tool works
// against a real backend end to end, at a scale small enough to run
// in CI: 2 developers, a few real seconds (heavily sped up), at least
// one real pull-and-rebuild. This is the "build and locally verify
// the tooling at small scale" CO-505 itself calls for — not the real
// 50-developer x 8-hour staging run, which docs/co-505-load-test.md
// gates behind an explicit human decision.
func TestRunEndToEndAgainstLocalSupabase(t *testing.T) {
	if _, err := (&http.Client{Timeout: 3 * time.Second}).Get(ltAPIURL + "/auth/v1/health"); err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}
	if _, err := (&http.Client{Timeout: 3 * time.Second}).Get(ltAPIURL + "/functions/v1/"); err != nil {
		t.Skipf("local Supabase Edge Functions not served (run `supabase functions serve` too): %v", err)
	}

	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	aliToken, err := passwordGrant(ctx, ltAPIURL, ltAnonKey, ltAliEmail, ltPassword, httpClient)
	if err != nil {
		t.Fatalf("password grant for ali: %v", err)
	}

	client := api.NewClientWithHTTP(ltAPIURL, ltAnonKey, httpClient)
	remote, err := project.NormalizeRemote(fmt.Sprintf("file://localhost/tmp/carryon-loadtest-%d.git", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("NormalizeRemote: %v", err)
	}
	proj, err := client.RegisterProject(ctx, aliToken, "loadtest-smoke", remote)
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	t.Cleanup(func() {
		ltPsql(t, fmt.Sprintf("delete from sessions where project_id = '%s';", proj.ID))
		ltPsql(t, fmt.Sprintf("delete from project_members where project_id = '%s';", proj.ID))
		ltPsql(t, fmt.Sprintf("delete from projects where id = '%s';", proj.ID))
	})
	ltPsql(t, fmt.Sprintf(
		"insert into project_members (project_id, user_id, role) values ('%s', '%s', 'resumer');",
		proj.ID, ltSaraID))

	report, err := Run(ctx, Options{
		APIURL:          ltAPIURL,
		AnonKey:         ltAnonKey,
		Credentials:     []Credential{{Email: ltAliEmail, Password: ltPassword}, {Email: ltSaraEmail, Password: ltPassword}},
		ProjectID:       proj.ID,
		Developers:      2,
		RunFor:          8 * time.Hour,
		Speed:           4800, // 8h -> 6s of real sleeping between replies
		PullsPerHour:    120,  // frequent enough to land at least one real pull in this short a run
		NetworkDropRate: 0.05,
		Seed:            42,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.Snapshot.UploadAttempts == 0 {
		t.Fatalf("expected at least one real upload attempt, got 0: %+v", report)
	}
	if report.Snapshot.PullAttempts == 0 {
		t.Fatalf("expected at least one real pull attempt at 120 pulls/hour over this run's duration, got 0: %+v", report)
	}
	if report.Snapshot.RebuildMismatch > 0 {
		t.Fatalf("NFR-04 violation: %d pull(s) did not rebuild byte-identically. Errors: %s", report.Snapshot.RebuildMismatch, strings.Join(report.Errors, "; "))
	}
	// A dropped-connection error during a real network-drop simulation
	// is expected and already retried by internal/upload/internal/pull
	// — what must NOT happen is a pull completing successfully with
	// wrong bytes, which the RebuildMismatch check above already
	// covers. Surface the error list for visibility either way.
	if len(report.Errors) > 0 {
		t.Logf("non-fatal errors during run (expected under a non-zero NetworkDropRate): %s", strings.Join(report.Errors, "; "))
	}

	cost := ExtrapolateMonthlyCost(report.Snapshot, 50, report.Options.RunFor, 0, 0, DefaultPricing())
	if cost.TotalUSD <= 0 {
		t.Fatalf("expected a positive extrapolated monthly cost, got %+v", cost)
	}
}
