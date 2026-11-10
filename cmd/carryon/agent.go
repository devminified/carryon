package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/filter"
	"github.com/devminified/carryon/internal/logfile"
	"github.com/devminified/carryon/internal/redact"
	"github.com/devminified/carryon/internal/service"
	"github.com/devminified/carryon/internal/snapshot"
	"github.com/devminified/carryon/internal/upload"
	kservice "github.com/kardianos/service"
	"github.com/spf13/cobra"
)

// lazyAPI resolves an api.Client and auth.Provider on first successful
// use, retrying on every call until config.Load() and auth.NewProvider()
// both succeed — so the agent service doesn't need to be configured or
// logged in before `carryon install` starts it (CO-106): it just can't
// look up project registration until it is, and each affected Upload()
// attempt fails gracefully and retries on the agent's normal debounce
// schedule rather than preventing the service from starting at all. Once
// resolved, the same *auth.Provider is reused across calls so its
// in-memory access-token cache actually does something.
type lazyAPI struct {
	mu           sync.Mutex
	cachedClient *api.Client
	provider     *auth.Provider

	// cachedLogin/loginFetchedAt cache the GitHub login CO-201's
	// snapshot push needs for its hidden ref name — fetched via a real
	// network call (GET /auth/v1/user), so it's cached the same way
	// internal/upload caches claude --version: re-checked at most once
	// per githubLoginCacheTTL, not on every snapshot attempt.
	cachedLogin    string
	loginFetchedAt time.Time

	// cachedRedactConfig/redactConfigAt/redactConfigFetched cache
	// get-config's (CO-301) response the same way — re-checked at most
	// once per redactConfigCacheTTL, not on every chunk. Until the first
	// successful fetch, and on any fetch failure thereafter,
	// redactConfig falls back to redact.DefaultConfig() (or the last
	// known-good merged config) rather than ever returning an empty/
	// weakened rule set — I5 means RedactConfigSource must never cause
	// a chunk to upload with fewer protections than the built-in
	// defaults just because the server was briefly unreachable.
	cachedRedactConfig  redact.Config
	redactConfigAt      time.Time
	redactConfigFetched bool

	// cachedPinnedVersion/pinnedVersionAt/pinnedVersionFetched (CO-308)
	// cache get-config's pinnedVersion field the same way, on its own
	// independent schedule — see pinnedVersion's own doc comment for
	// why it's a separate cache rather than sharing redactConfig's.
	cachedPinnedVersion  string
	pinnedVersionAt      time.Time
	pinnedVersionFetched bool
}

// githubLoginCacheTTL mirrors internal/upload's claude-version cache
// window — a developer's GitHub login practically never changes within
// an agent process's lifetime.
const githubLoginCacheTTL = 24 * time.Hour

// redactConfigCacheTTL is the same caching window applied to get-config
// (CO-301) — the server's disabled-rule/denylist-addition config
// changes rarely enough that fetching it on every upload chunk would be
// pure waste.
const redactConfigCacheTTL = 24 * time.Hour

// redactConfigFetchTimeout bounds the get-config network call —
// RedactConfigSource (upload.Config) has no error return and must
// return promptly every time, so a slow or unreachable server can't be
// allowed to stall an upload; it just falls back below.
const redactConfigFetchTimeout = 5 * time.Second

func (l *lazyAPI) matchProject(ctx context.Context, canonicalRemote string) (*api.MatchProjectResult, error) {
	client, provider, err := l.resolve()
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	token, err := provider.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return client.MatchProject(ctx, token, canonicalRemote)
}

// client and accessToken expose the same lazy resolution as matchProject,
// in the shapes upload.Config needs (upload.ClientSource, upload.TokenSource).
func (l *lazyAPI) client() (*api.Client, error) {
	client, _, err := l.resolve()
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return client, nil
}

func (l *lazyAPI) accessToken(ctx context.Context) (string, error) {
	_, provider, err := l.resolve()
	if err != nil {
		return "", fmt.Errorf("agent: %w", err)
	}
	return provider.AccessToken(ctx)
}

// githubLogin matches snapshot.CurrentUserFunc — the developer's GitHub
// login, for CO-201's hidden ref name.
func (l *lazyAPI) githubLogin(ctx context.Context) (string, error) {
	_, provider, err := l.resolve()
	if err != nil {
		return "", fmt.Errorf("agent: %w", err)
	}

	l.mu.Lock()
	if l.cachedLogin != "" && time.Since(l.loginFetchedAt) < githubLoginCacheTTL {
		login := l.cachedLogin
		l.mu.Unlock()
		return login, nil
	}
	l.mu.Unlock()

	user, err := provider.CurrentUser(ctx)
	if err != nil {
		return "", err
	}

	l.mu.Lock()
	l.cachedLogin = user.UserName
	l.loginFetchedAt = time.Now()
	l.mu.Unlock()
	return user.UserName, nil
}

// redactConfig matches upload.Config's RedactConfigSource shape — no
// context, no error. It fetches get-config at most once per
// redactConfigCacheTTL, merging the result onto redact.DefaultConfig()
// via redact.ApplyServerConfig. Any failure along the way (not yet
// logged in, network error, server error) falls back to the last
// known-good merged config, or plain redact.DefaultConfig() if there
// hasn't been one yet — never to a weaker, empty rule set.
func (l *lazyAPI) redactConfig() redact.Config {
	l.mu.Lock()
	if l.redactConfigFetched && time.Since(l.redactConfigAt) < redactConfigCacheTTL {
		cfg := l.cachedRedactConfig
		l.mu.Unlock()
		return cfg
	}
	l.mu.Unlock()

	client, provider, err := l.resolve()
	if err != nil {
		return l.fallbackRedactConfig()
	}

	ctx, cancel := context.WithTimeout(context.Background(), redactConfigFetchTimeout)
	defer cancel()

	token, err := provider.AccessToken(ctx)
	if err != nil {
		return l.fallbackRedactConfig()
	}

	serverCfg, err := client.GetConfig(ctx, token)
	if err != nil {
		return l.fallbackRedactConfig()
	}

	merged := redact.ApplyServerConfig(redact.DefaultConfig(), serverCfg.DisabledRuleIDs, serverCfg.DenylistAdditions)

	l.mu.Lock()
	l.cachedRedactConfig = merged
	l.redactConfigAt = time.Now()
	l.redactConfigFetched = true
	l.mu.Unlock()
	return merged
}

func (l *lazyAPI) fallbackRedactConfig() redact.Config {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.redactConfigFetched {
		return l.cachedRedactConfig
	}
	return redact.DefaultConfig()
}

// pinnedVersion fetches get-config's pinnedVersion field (CO-308), its
// own separate cache from redactConfig's — a second GetConfig call
// once per redactConfigCacheTTL rather than sharing one fetch, since
// the two features (redaction overrides, update pinning) are
// otherwise unrelated and this keeps each independently testable.
// Unlike redactConfig, this returns an error on failure rather than a
// safe fallback: internal/update's own caller treats "couldn't find
// out the pin" as "skip this update cycle, try again next time," not
// as "assume no pin and update anyway" — a transient lookup failure
// must never be read as permission to update past a real pin.
func (l *lazyAPI) pinnedVersion(ctx context.Context) (string, error) {
	l.mu.Lock()
	if l.pinnedVersionFetched && time.Since(l.pinnedVersionAt) < redactConfigCacheTTL {
		v := l.cachedPinnedVersion
		l.mu.Unlock()
		return v, nil
	}
	l.mu.Unlock()

	client, provider, err := l.resolve()
	if err != nil {
		return "", err
	}
	token, err := provider.AccessToken(ctx)
	if err != nil {
		return "", err
	}
	cfg, err := client.GetConfig(ctx, token)
	if err != nil {
		return "", err
	}

	l.mu.Lock()
	l.cachedPinnedVersion = cfg.PinnedVersion
	l.pinnedVersionAt = time.Now()
	l.pinnedVersionFetched = true
	l.mu.Unlock()
	return cfg.PinnedVersion, nil
}

func (l *lazyAPI) resolve() (*api.Client, *auth.Provider, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cachedClient != nil {
		return l.cachedClient, l.provider, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	provider, err := auth.NewProvider(cfg)
	if err != nil {
		return nil, nil, err
	}

	l.cachedClient = api.NewClient(cfg.APIURL, cfg.AnonKey)
	l.provider = provider
	return l.cachedClient, l.provider, nil
}

// agentProgram adapts internal/agent.Agent to kservice.Interface. Start
// launches the agent's Run loop in the background; Stop cancels it and
// waits for it to finish — this is what gives in-flight uploads a chance
// to persist state before exit (go-conventions.md), and on Windows it's
// the *only* way to be notified of a stop at all (see service package doc
// comment).
type agentProgram struct {
	cancel    context.CancelFunc
	done      chan error
	logWriter *logfile.Writer
}

func (p *agentProgram) Start(kservice.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)

	carryonDir, err := config.Dir()
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	// CO-113: structured JSON logs, daily rotation (7 kept) — `carryon
	// logs` reads this file. Every slog.* call anywhere in this process
	// goes through it from here on, including the default Warn calls
	// internal/agent.Config.logf falls back to.
	logWriter, err := logfile.New(filepath.Join(carryonDir, "logs"), "agent")
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	p.logWriter = logWriter
	slog.SetDefault(slog.New(slog.NewJSONHandler(logWriter, nil)))

	lazy := &lazyAPI{}

	snap, err := snapshot.New(snapshot.Config{
		ClientSource: lazy.client,
		TokenSource:  lazy.accessToken,
		CurrentUser:  lazy.githubLogin,
		StateDir:     filepath.Join(carryonDir, "snapshot-state"),
		RepoStateDir: filepath.Join(carryonDir, "snapshot-repo-state"),
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	realUploader, err := upload.New(upload.Config{
		ClientSource:       lazy.client,
		TokenSource:        lazy.accessToken,
		StateDir:           filepath.Join(carryonDir, "state"),
		Snapshotter:        snap,
		RedactConfigSource: lazy.redactConfig,
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	uploader := filter.New(filter.Config{
		Inner:        realUploader,
		MatchProject: lazy.matchProject,
		SkippedPath:  filepath.Join(carryonDir, "skipped-repos.json"),
	})

	statusPath := filepath.Join(carryonDir, "agent-status.json")
	a, err := agent.New(agent.Config{
		QueuePath:  filepath.Join(carryonDir, "queue.jsonl"),
		CursorPath: filepath.Join(carryonDir, "agent-cursor.json"),
		StatusPath: statusPath,
		Version:    version,
		Uploader:   uploader,
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	go func() { p.done <- a.Run(ctx) }()
	go runUpdateLoop(ctx, lazy) // CO-308: daily self-update check, fire-and-forget — see its own doc comment
	// CO-410/CO-504: admin "Agents" page check-in, fire-and-forget —
	// realUploader (not the filter-wrapped uploader Agent itself
	// holds) is what actually counts attempts/errors/redactions, so
	// the heartbeat reads straight from it rather than through
	// filter.Filter's own pass-through.
	go runHeartbeatLoop(ctx, lazy, statusPath, filepath.Join(carryonDir, "state"), realUploader)
	return nil
}

func (p *agentProgram) Stop(kservice.Service) error {
	p.cancel()
	err := <-p.done
	if p.logWriter != nil {
		_ = p.logWriter.Close()
	}
	return err
}

func newAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent",
		Short: "Run the capture agent daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := service.New(&agentProgram{})
			if err != nil {
				return fmt.Errorf("agent: %w", err)
			}
			if err := s.Run(); err != nil {
				return fmt.Errorf("agent: %w", err)
			}
			return nil
		},
	}
}
