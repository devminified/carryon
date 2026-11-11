package loadtest

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/pull"
)

// Options configures one CO-505 run. See docs/co-505-load-test.md for
// the full operator guide, including why a real 50-developer x 8-hour
// run against real Supabase Cloud staging needs explicit human
// confirmation before it's ever actually executed — this package
// itself enforces nothing about cost; that gate lives in
// cmd/carryon-loadtest, the one place that knows whether a given
// invocation targets a real, billed backend.
type Options struct {
	APIURL      string
	AnonKey     string
	Credentials []Credential
	ProjectID   string

	// Developers is how many of Credentials to actually run — allows a
	// short credentials file to be reused across runs of different
	// sizes. Must be <= len(Credentials).
	Developers int
	// RunFor is the real (unscaled) duration each developer simulates
	// — the ticket's "8 hours each."
	RunFor time.Duration
	// Speed compresses RunFor and the 30-120s reply interval into
	// actual wall-clock time: an 8-hour run at Speed=480 takes 1 real
	// minute. Speed=1 runs at real time, for an actual staging soak.
	Speed float64
	// PullsPerHour is team-wide, matching the ticket's "5 pulls per
	// hour across the team" — not per developer.
	PullsPerHour float64
	// NetworkDropRate is the fraction (0..1) of HTTP requests (both
	// Edge Function calls and chunk PUT/GET) that fail with a
	// simulated dropped connection — the ticket's "random network
	// drops." 0 disables this entirely.
	NetworkDropRate float64

	// WorkDir holds every developer's synthetic transcript, upload
	// state, and pull-rebuild scratch space. A fresh os.MkdirTemp if
	// empty.
	WorkDir string

	Seed int64 // 0 means "derive from time.Now()", for a non-reproducible real run
}

// MinReplyInterval/MaxReplyInterval match the ticket's own "one reply
// every 30-120 seconds" — not configurable via flags, since they're
// the scenario's own definition, not a tuning knob.
const (
	MinReplyInterval = 30 * time.Second
	MaxReplyInterval = 120 * time.Second
)

// Report is Run's full output — everything docs/co-505-load-test.md's
// report format needs.
type Report struct {
	Options      Options
	Snapshot     Snapshot
	CostEstimate CostEstimate
	Pricing      Pricing
	Duration     time.Duration // real wall-clock time the run actually took
	Errors       []string      // non-fatal problems worth surfacing (a developer that errored out, a pull mismatch)
}

// Run executes one CO-505 load/soak/cost simulation: Developers
// simulated developers, each uploading a realistic transcript on a
// jittered cadence for RunFor (compressed by Speed), plus a team-wide
// pull loop firing at PullsPerHour that verifies NFR-04's
// byte-identical rebuild for a real, randomly-chosen, already-uploaded
// session. Returns once every developer has finished and the pull
// loop has been given a chance to drain.
func Run(ctx context.Context, opt Options) (*Report, error) {
	if opt.Developers <= 0 {
		return nil, fmt.Errorf("loadtest: Developers must be > 0")
	}
	if opt.Developers > len(opt.Credentials) {
		return nil, fmt.Errorf("loadtest: Developers (%d) exceeds the number of supplied credentials (%d)", opt.Developers, len(opt.Credentials))
	}
	if opt.RunFor <= 0 {
		return nil, fmt.Errorf("loadtest: RunFor must be > 0")
	}

	workDir := opt.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "carryon-loadtest-*")
		if err != nil {
			return nil, fmt.Errorf("creating work dir: %w", err)
		}
	}

	seed := opt.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	metrics := &Metrics{}
	start := time.Now()

	type sessionRecord struct {
		sessionID   string
		accessToken string
		finalBytes  []byte
	}
	var (
		sessionsMu sync.Mutex
		sessions   []sessionRecord
		errsMu     sync.Mutex
		errs       []string
	)
	recordErr := func(format string, args ...any) {
		errsMu.Lock()
		defer errsMu.Unlock()
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	pullCtx, stopPulls := context.WithCancel(ctx)
	defer stopPulls()
	var pullWG sync.WaitGroup
	pullWG.Add(1)
	go func() {
		defer pullWG.Done()
		runPullLoop(pullCtx, opt, metrics, func() (sessionID, accessToken string, want []byte, ok bool) {
			sessionsMu.Lock()
			defer sessionsMu.Unlock()
			if len(sessions) == 0 {
				return "", "", nil, false
			}
			r := sessions[rand.Intn(len(sessions))] // #nosec G404 -- picking a random fixture session, not a security context
			return r.sessionID, r.accessToken, r.finalBytes, true
		}, recordErr)
	}()

	var devWG sync.WaitGroup
	for i := 0; i < opt.Developers; i++ {
		i := i
		devWG.Add(1)
		go func() {
			defer devWG.Done()
			cred := opt.Credentials[i]
			rnd := rand.New(rand.NewSource(seed + int64(i))) // #nosec G404 -- traffic shaping, not a security context
			httpClient := httpClientFor(opt.NetworkDropRate, rnd.Int63())
			clientFactory, accessToken, err := bootstrapDeveloper(ctx, opt, cred, httpClient)
			if err != nil {
				recordErr("developer %s: bootstrap: %v", cred.Email, err)
				return
			}
			devCwd := filepath.Join(workDir, fmt.Sprintf("dev-%d-repo", i))
			if err := os.MkdirAll(devCwd, 0o755); err != nil { // #nosec G301 -- this run's own throwaway fixture repo dir, not sensitive state
				recordErr("developer %s: %v", cred.Email, err)
				return
			}
			sessionID, finalBytes, err := runDeveloper(ctx, workDir, developerOptions{
				cred:          cred,
				accessToken:   accessToken,
				projectID:     opt.ProjectID,
				cwd:           devCwd,
				httpClient:    httpClient,
				clientFactory: clientFactory,
				minReply:      MinReplyInterval,
				maxReply:      MaxReplyInterval,
				speed:         opt.Speed,
				runFor:        opt.RunFor,
				metrics:       metrics,
				rnd:           rnd,
			})
			if err != nil {
				recordErr("developer %s: %v", cred.Email, err)
				return
			}
			sessionsMu.Lock()
			sessions = append(sessions, sessionRecord{sessionID: sessionID, accessToken: accessToken, finalBytes: finalBytes})
			sessionsMu.Unlock()
		}()
	}
	devWG.Wait()
	stopPulls()
	pullWG.Wait()

	return &Report{
		Options:  opt,
		Snapshot: metrics.Snapshot(),
		Duration: time.Since(start),
		Errors:   errs,
	}, nil
}

// httpClientFor builds the *http.Client every one piece of this
// developer's traffic (Edge Function calls and chunk PUT/GET) shares,
// wrapped in a FlakyTransport when dropRate > 0.
func httpClientFor(dropRate float64, seed int64) *http.Client {
	base := &http.Client{Timeout: 60 * time.Second}
	if dropRate <= 0 {
		return base
	}
	base.Transport = &FlakyTransport{
		Next: http.DefaultTransport,
		Rate: dropRate,
		rnd:  rand.New(rand.NewSource(seed)), // #nosec G404 -- traffic shaping, not a security context
	}
	return base
}

// bootstrapDeveloper signs in cred against the real backend and
// returns a ready-to-use api.Client factory plus the access token —
// the same password-grant technique cmd/carryon's own e2e_test.go
// uses for local/CI, and the real credential an operator provisions
// ahead of time for a real staging run (see docs/co-505-load-test.md).
func bootstrapDeveloper(ctx context.Context, opt Options, cred Credential, httpClient *http.Client) (func() (*api.Client, error), string, error) {
	// A one-time retry here, not internal/upload's own per-chunk retry
	// logic — sign-in happens once per developer, so a single
	// simulated network drop (NetworkDropRate) shouldn't permanently
	// sideline an otherwise-healthy developer before it ever starts.
	var accessToken string
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		accessToken, err = passwordGrant(ctx, opt.APIURL, opt.AnonKey, cred.Email, cred.Password, httpClient)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, "", fmt.Errorf("signing in: %w", err)
	}
	client := api.NewClientWithHTTP(opt.APIURL, opt.AnonKey, httpClient)
	return func() (*api.Client, error) { return client, nil }, accessToken, nil
}

// runPullLoop fires PullsPerHour-rate pulls against randomly-chosen
// already-uploaded sessions, each a real GetManifest + pull.Rebuild —
// the same production code path internal/pull's own CLI command uses
// — and records whether the rebuilt bytes exactly match what that
// developer actually uploaded (NFR-04). pick returns false until at
// least one developer has produced a session.
func runPullLoop(ctx context.Context, opt Options, metrics *Metrics, pick func() (sessionID, accessToken string, want []byte, ok bool), recordErr func(string, ...any)) {
	if opt.PullsPerHour <= 0 {
		return
	}
	realInterval := time.Duration(float64(time.Hour) / opt.PullsPerHour)
	interval := scaleDuration(realInterval, opt.Speed)
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sessionID, accessToken, want, ok := pick()
			if !ok {
				continue
			}
			simulateOnePull(ctx, opt, sessionID, accessToken, want, metrics, recordErr)
		}
	}
}

func simulateOnePull(ctx context.Context, opt Options, sessionID, accessToken string, want []byte, metrics *Metrics, recordErr func(string, ...any)) {
	httpClient := httpClientFor(opt.NetworkDropRate, time.Now().UnixNano())
	client := api.NewClientWithHTTP(opt.APIURL, opt.AnonKey, httpClient)

	destDir, err := os.MkdirTemp("", "carryon-loadtest-pull-*")
	if err != nil {
		recordErr("pull %s: %v", sessionID, err)
		return
	}
	defer func() { _ = os.RemoveAll(destDir) }()

	start := time.Now()
	manifest, err := client.GetManifest(ctx, accessToken, sessionID)
	if err != nil {
		metrics.RecordPull(time.Since(start), 0, err, false)
		recordErr("pull %s: GetManifest: %v", sessionID, err)
		return
	}
	result, err := pull.Rebuild(ctx, client, manifest, destDir, pull.Config{
		HTTPClient: httpClient,
		Workers:    pull.DefaultConfig().Workers,
		MaxRetries: pull.DefaultConfig().MaxRetries,
	})
	elapsed := time.Since(start)
	if err != nil {
		metrics.RecordPull(elapsed, 0, err, false)
		recordErr("pull %s: Rebuild: %v", sessionID, err)
		return
	}

	// #nosec G304 -- result.MainFile is this run's own just-created temp path
	got, readErr := os.ReadFile(result.MainFile)
	if readErr != nil {
		metrics.RecordPull(elapsed, result.TotalBytes, readErr, false)
		recordErr("pull %s: reading rebuilt file: %v", sessionID, readErr)
		return
	}
	byteIdentical := string(got) == string(want)
	if !byteIdentical {
		recordErr("pull %s: rebuilt %d bytes did not match the %d bytes uploaded (NFR-04 violation)", sessionID, len(got), len(want))
	}
	metrics.RecordPull(elapsed, result.TotalBytes, nil, byteIdentical)
}
