// Command carryon-loadtest is CO-505's own `make loadtest` tooling —
// never shipped to a developer's laptop, never imported by the
// carryon binary itself (same "release-adjacent, not laptop code"
// shape as cmd/carryon-sign-manifest). It drives internal/loadtest's
// real simulation: N developers each running a real agent.Uploader
// against a real backend at a jittered cadence, plus a team-wide
// stream of real pulls verifying NFR-04's byte-identical rebuild.
//
// A small, fast run against local Supabase (the `make loadtest`
// default) needs no special authorization — it costs nothing and
// touches nothing but a throwaway local stack. The real scenario the
// ticket describes — 50 developers x 8 hours against real Supabase
// Cloud staging — is genuinely costly and is refused outright unless
// -confirm-real-cost is passed, which this tool only accepts once an
// operator has actually read docs/co-505-load-test.md and decided to
// spend the money; see isLocalAPIURL below for exactly what counts as
// "free to run without that flag."
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/devminified/carryon/internal/loadtest"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "carryon-loadtest:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("carryon-loadtest", flag.ContinueOnError)
	apiURL := fs.String("api-url", "http://127.0.0.1:54421", "Supabase project URL to load-test against")
	anonKey := fs.String("anon-key", "sb_publishable_ACJWlzQHlZjBrEguHvfOxg_3BJgxAaH", "Supabase anon key for -api-url")
	credentialsFile := fs.String("credentials-file", "", "JSON array of {\"email\":...,\"password\":...} — real accounts an operator has already provisioned on the target backend (see docs/co-505-load-test.md). Required unless -project-id is omitted for a local dry run against seed.sql's own ali/sara users.")
	projectID := fs.String("project-id", "", "an existing project's ID that every credential already has access to (resumer+ for the ones that will be asked to pull) — this tool never registers or grants access itself")
	developers := fs.Int("developers", 2, "number of simulated developers (must be <= the number of credentials supplied)")
	duration := fs.Duration("duration", 2*time.Minute, "real (unscaled) duration each developer simulates — the ticket's own scenario is 8h")
	speed := fs.Float64("speed", 60, "wall-clock compression factor: -duration is divided by this. 1 = real time (an actual soak); the default compresses a short dry run")
	pullsPerHour := fs.Float64("pulls-per-hour", 5, "team-wide pull rate, matching the ticket's '5 pulls per hour across the team'")
	networkDropRate := fs.Float64("network-drop-rate", 0.02, "fraction (0..1) of HTTP requests that simulate a dropped connection")
	seed := fs.Int64("seed", 0, "deterministic PRNG seed; 0 derives one from the current time")
	outPath := fs.String("out", "loadtest-report.md", "where to write the markdown report")
	confirmRealCost := fs.Bool("confirm-real-cost", false, "required to target a non-local -api-url — see docs/co-505-load-test.md before passing this")
	dbURL := fs.String("db-url", "postgresql://postgres:postgres@127.0.0.1:54422/postgres", "local dev DB URL, used only to auto-provision and clean up a throwaway project when -project-id and -api-url are both local")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !isLocalAPIURL(*apiURL) && !*confirmRealCost {
		return fmt.Errorf(
			"-api-url %q is not a recognized local address, which this tool treats as a real, billed backend.\n"+
				"Running this tool against a real Supabase Cloud project spends real money (Edge Function invocations, Storage, compute) and real Anthropic API credits if any simulated session reaches summarize-session.\n"+
				"Read docs/co-505-load-test.md, decide this is wanted, and pass -confirm-real-cost to proceed", *apiURL)
	}

	var creds []loadtest.Credential
	var err error
	if *credentialsFile != "" {
		creds, err = loadCredentials(*credentialsFile)
		if err != nil {
			return err
		}
	} else if isLocalAPIURL(*apiURL) {
		// Local dry run against the same seeded users every other
		// local/CI test in this codebase already relies on
		// (web/e2e/seed.sql, cmd/carryon's own e2e_test.go) — no
		// credentials file needed for `make loadtest`'s own default.
		creds = []loadtest.Credential{
			{Email: "ali@devminified.com", Password: "password123"},
			{Email: "sara@devminified.com", Password: "password123"},
		}
	} else {
		return fmt.Errorf("-credentials-file is required for a non-local -api-url — see docs/co-505-load-test.md")
	}

	ctx := context.Background()

	resolvedProjectID := *projectID
	if resolvedProjectID == "" {
		if !isLocalAPIURL(*apiURL) {
			return fmt.Errorf("-project-id is required for a non-local -api-url: this tool never registers a project against a real backend itself — see docs/co-505-load-test.md")
		}
		// `make loadtest`'s own zero-setup default: register a
		// throwaway project against the local dev stack (same
		// project.NormalizeRemote + register-project path every real
		// `carryon` does) and grant every credential beyond the first
		// resumer access directly in the DB — the same technique
		// cmd/carryon's own e2e_test.go uses for its seeded users —
		// then clean it up once the run finishes.
		proj, cleanup, err := provisionLocalProject(ctx, *apiURL, *anonKey, *dbURL, creds)
		if err != nil {
			return fmt.Errorf("provisioning a local dry-run project: %w", err)
		}
		defer cleanup()
		resolvedProjectID = proj
	}

	report, err := loadtest.Run(ctx, loadtest.Options{
		APIURL:          *apiURL,
		AnonKey:         *anonKey,
		Credentials:     creds,
		ProjectID:       resolvedProjectID,
		Developers:      *developers,
		RunFor:          *duration,
		Speed:           *speed,
		PullsPerHour:    *pullsPerHour,
		NetworkDropRate: *networkDropRate,
		Seed:            *seed,
	})
	if err != nil {
		return fmt.Errorf("running simulation: %w", err)
	}

	cost := loadtest.ExtrapolateMonthlyCost(report.Snapshot, 50, report.Options.RunFor, 0, 0, loadtest.DefaultPricing())
	report.CostEstimate = cost
	report.Pricing = loadtest.DefaultPricing()

	md := loadtest.RenderMarkdown(report)
	if err := os.WriteFile(*outPath, []byte(md), 0o600); err != nil {
		return fmt.Errorf("writing report to %s: %w", *outPath, err)
	}
	fmt.Printf("report written to %s\n", *outPath)
	if report.Snapshot.RebuildMismatch > 0 {
		return fmt.Errorf("NFR-04 violation: %d pull(s) did not rebuild byte-identically — see %s", report.Snapshot.RebuildMismatch, *outPath)
	}
	return nil
}

// isLocalAPIURL is the whole safety gate's decision function —
// conservative on purpose: anything that isn't obviously a loopback
// or a local-dev hostname is treated as a real backend, even if it
// turns out to actually be harmless, so a typo'd staging URL doesn't
// accidentally slip past the cost confirmation.
func isLocalAPIURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1" ||
		strings.HasSuffix(host, ".local") || host == "host.docker.internal"
}
