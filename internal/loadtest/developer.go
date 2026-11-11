package loadtest

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/upload"
)

// Credential is one developer's real sign-in for the target backend.
// For a local/CI dry run these come from seed.sql's own seeded users;
// for a real staging run, an operator provisions them ahead of time
// (same manual, documented step as every other one-time auth setup in
// this codebase — see auth.md) — this package never creates accounts.
type Credential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// developerOptions configures one simulated developer. ReplyInterval
// is the real (unscaled) 30s-120s-per-reply range the ticket
// describes; Speed compresses that into actual wall-clock sleeps so a
// local verification run doesn't need to really sit for 8 hours (see
// Options.Speed's own doc comment).
type developerOptions struct {
	cred          Credential
	accessToken   string
	projectID     string
	cwd           string
	httpClient    *http.Client
	clientFactory func() (*api.Client, error)
	minReply      time.Duration
	maxReply      time.Duration
	speed         float64
	runFor        time.Duration // real (unscaled) duration this developer simulates
	metrics       *Metrics
	rnd           *rand.Rand
}

// runDeveloper simulates one developer for the configured duration:
// writes an incrementally-growing transcript and calls the real
// agent.Uploader.Upload on a jittered cadence, exactly the shape a
// live carryon agent does for a live Claude Code session. Returns the
// session ID and the final, complete transcript bytes it produced —
// the pull side (simulatePull in simulate.go) compares a real
// rebuild's bytes against exactly this, for NFR-04.
func runDeveloper(ctx context.Context, workDir string, opt developerOptions) (sessionID string, finalBytes []byte, err error) {
	sessionID = newSyntheticSessionID(opt.rnd)
	transcriptPath := filepath.Join(workDir, sessionID+".jsonl")
	// #nosec G304 -- workDir is this run's own temp directory, not
	// attacker-influenced input.
	f, err := os.OpenFile(transcriptPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("creating synthetic transcript: %w", err)
	}
	defer func() { _ = f.Close() }()

	uploader, err := upload.New(upload.Config{
		ClientSource: opt.clientFactory,
		TokenSource:  func(context.Context) (string, error) { return opt.accessToken, nil },
		StateDir:     filepath.Join(workDir, "state", sessionID),
		HTTPClient:   opt.httpClient,
	})
	if err != nil {
		return "", nil, fmt.Errorf("building uploader: %w", err)
	}

	deadline := time.Now().Add(scaleDuration(opt.runFor, opt.speed))
	seq := 0
	for {
		line := nextTurn(opt.rnd, sessionID, opt.cwd, seq)
		seq++
		if _, err := f.Write(line); err != nil {
			return "", nil, fmt.Errorf("writing synthetic turn: %w", err)
		}
		if err := f.Sync(); err != nil {
			return "", nil, fmt.Errorf("syncing synthetic transcript: %w", err)
		}

		final := time.Now().After(deadline)
		uploadErr := recordedUpload(ctx, uploader, sessionID, opt, transcriptPath, int64(len(line)), final)

		if final {
			if uploadErr != nil {
				// A real carryon agent keeps retrying an upload on its
				// next scheduler tick rather than giving up — stand in
				// for that here with a few bounded, immediate retries
				// rather than treating this developer as "done" with
				// the server actually missing its trailing bytes
				// (which would make the pull side's NFR-04 check fail
				// for a reason that has nothing to do with pull/rebuild
				// correctness). A genuine, sustained outage still
				// surfaces as a real error from runDeveloper.
				const maxFinalRetries = 5
				for i := 0; i < maxFinalRetries && uploadErr != nil; i++ {
					uploadErr = recordedUpload(ctx, uploader, sessionID, opt, transcriptPath, 0, true)
				}
				if uploadErr != nil {
					return "", nil, fmt.Errorf("final upload never succeeded after %d retries: %w", maxFinalRetries, uploadErr)
				}
			}
			break
		}
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		sleepJittered(ctx, opt.minReply, opt.maxReply, opt.speed, opt.rnd)
	}

	finalBytes, err = os.ReadFile(transcriptPath) // #nosec G304 -- this run's own synthetic file
	if err != nil {
		return "", nil, fmt.Errorf("reading back synthetic transcript: %w", err)
	}
	return sessionID, finalBytes, nil
}

// recordedUpload calls uploader.Upload once, records its outcome into
// opt.metrics, and returns its error. bytesSent is the newly-written
// line's size for a normal call, or 0 for a bare final-flag retry
// (recordedUpload already counted those bytes once, on its first,
// failed attempt — a retry isn't new traffic, it's the same bytes
// again).
func recordedUpload(ctx context.Context, uploader *upload.Uploader, sessionID string, opt developerOptions, transcriptPath string, bytesSent int64, final bool) error {
	start := time.Now()
	err := uploader.Upload(ctx, agent.Session{
		ID:             sessionID,
		Cwd:            opt.cwd,
		TranscriptPath: transcriptPath,
		ProjectID:      opt.projectID,
		Final:          final,
	})
	_, _, redactions := uploader.TakeCounts()
	opt.metrics.RecordUpload(time.Since(start), bytesSent, int(redactions), err)
	return err
}

// scaleDuration compresses a real-world duration by speed (e.g.
// speed=480 turns 8h into 1m) — Options.Speed's own doc comment has
// the full rationale.
func scaleDuration(d time.Duration, speed float64) time.Duration {
	if speed <= 0 {
		speed = 1
	}
	return time.Duration(float64(d) / speed)
}

// sleepJittered sleeps for a uniformly random duration in
// [min, max), scaled by speed, honoring ctx cancellation — the
// ticket's "one reply every 30-120 seconds."
func sleepJittered(ctx context.Context, min, max time.Duration, speed float64, rnd *rand.Rand) {
	if max <= min {
		max = min + time.Millisecond
	}
	jitter := time.Duration(rnd.Int63n(int64(max - min)))
	d := scaleDuration(min+jitter, speed)
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

// newSyntheticSessionID returns a UUID-shaped, clearly-synthetic
// session ID — real session IDs are also UUIDs (pull.NewSessionID),
// but this package deliberately doesn't import internal/pull just to
// mint one, since there's nothing session-specific about the ID
// itself beyond being a valid UUID the schema accepts.
func newSyntheticSessionID(rnd *rand.Rand) string {
	b := make([]byte, 16)
	_, _ = rnd.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
