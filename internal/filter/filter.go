// Package filter implements CO-108: deciding whether a session's repo is
// one Carryon should capture at all, before any upload work happens.
package filter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/project"
)

// MatchProjectFunc looks up whether canonicalRemote is a project Carryon
// knows about that the caller is a member of. It should translate
// api.IsNotFound and api.IsForbidden into (nil, nil) — "not registered" or
// "registered but not a member" are the same outcome from Filter's point
// of view (skip) — and return any other error as-is, so Filter can tell a
// real "not applicable" result apart from a transient lookup failure.
// api.Client.MatchProject itself returns errors shaped exactly this way.
type MatchProjectFunc func(ctx context.Context, canonicalRemote string) (*api.MatchProjectResult, error)

// Config configures a Filter. Inner and MatchProject are required.
type Config struct {
	// Inner is the real upload pipeline (CO-109/CO-110). Filter never
	// calls it — and so never gives it a chance to open the session's
	// transcript file — for a session whose repo isn't a registered
	// project the caller is a member of.
	Inner agent.Uploader
	// MatchProject performs the actual project-registration lookup.
	MatchProject MatchProjectFunc
	// CacheTTL is how long a lookup result is cached per canonical
	// remote. Default 1 hour (CO-108).
	CacheTTL time.Duration

	// logf receives CO-108's exact "Repo ... is not registered with
	// Carryon" message, once per remote. Defaults to slog.Info; tests
	// override it to assert on exactly-once behavior precisely.
	logf func(msg string)
	// now is overridable so tests can control cache-expiry timing
	// deterministically without real sleeps.
	now func() time.Time
}

type cacheEntry struct {
	result    *api.MatchProjectResult
	expiresAt time.Time
}

// Filter wraps an inner agent.Uploader (agent.Uploader itself, so it
// composes directly into Config.Uploader) and implements CO-108's
// gate in front of it.
type Filter struct {
	inner        agent.Uploader
	matchProject MatchProjectFunc
	cacheTTL     time.Duration
	logf         func(msg string)
	now          func() time.Time

	mu            sync.Mutex
	cache         map[string]cacheEntry
	warnedRemotes map[string]bool
}

// New returns a Filter. Panics if cfg.Inner or cfg.MatchProject is nil —
// both are required wiring, not a runtime condition to degrade from.
func New(cfg Config) *Filter {
	if cfg.Inner == nil {
		panic("filter: Inner is required")
	}
	if cfg.MatchProject == nil {
		panic("filter: MatchProject is required")
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	logf := cfg.logf
	if logf == nil {
		logf = func(msg string) { slog.Info(msg) }
	}
	return &Filter{
		inner:         cfg.Inner,
		matchProject:  cfg.MatchProject,
		cacheTTL:      ttl,
		logf:          logf,
		now:           now,
		cache:         map[string]cacheEntry{},
		warnedRemotes: map[string]bool{},
	}
}

// Upload implements agent.Uploader (CO-107). A session whose cwd isn't
// inside a git repo, whose repo has no remote, whose remote isn't a
// registered project, or whose project the caller isn't a member of is
// skipped — Upload returns nil (success, not an error to retry) without
// ever calling Inner, and without ever touching the session's transcript
// file itself. A transient lookup failure returns an error instead, so
// the agent retries the session on its normal debounce schedule — "not
// registered" and "couldn't check right now" are different outcomes and
// must not be conflated.
func (f *Filter) Upload(ctx context.Context, session agent.Session) error {
	remote, err := project.ResolveRemote(ctx, session.Cwd)
	if err != nil {
		return nil // no repo, or no remote — nothing to register, skip quietly
	}

	match, err := f.lookup(ctx, remote)
	if err != nil {
		return agent.ClassifyError(fmt.Errorf("filter: checking project registration for %s: %w", remote, err))
	}
	if match == nil {
		f.warnOnce(remote)
		return nil
	}

	session.ProjectID = match.ProjectID // Session is passed by value — this only affects Inner's copy
	return f.inner.Upload(ctx, session)
}

func (f *Filter) lookup(ctx context.Context, remote string) (*api.MatchProjectResult, error) {
	f.mu.Lock()
	if e, ok := f.cache[remote]; ok && f.now().Before(e.expiresAt) {
		f.mu.Unlock()
		return e.result, nil
	}
	f.mu.Unlock()

	result, err := f.matchProject(ctx, remote)
	if err != nil {
		if api.IsNotFound(err) || api.IsForbidden(err) {
			f.setCache(remote, nil)
			return nil, nil
		}
		return nil, err
	}

	f.setCache(remote, result)
	return result, nil
}

func (f *Filter) setCache(remote string, result *api.MatchProjectResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache[remote] = cacheEntry{result: result, expiresAt: f.now().Add(f.cacheTTL)}
}

// warnOnce logs CO-108's exact message the first time a given remote is
// seen as unregistered, and never again for that remote — a session in an
// unregistered repo fires an upload attempt (and so this check) on every
// debounce cycle for as long as the developer keeps working in it.
func (f *Filter) warnOnce(remote string) {
	f.mu.Lock()
	already := f.warnedRemotes[remote]
	f.warnedRemotes[remote] = true
	f.mu.Unlock()

	if !already {
		f.logf(fmt.Sprintf("Repo %s is not registered with Carryon", remote))
	}
}
