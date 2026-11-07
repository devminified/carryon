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
}

// githubLoginCacheTTL mirrors internal/upload's claude-version cache
// window — a developer's GitHub login practically never changes within
// an agent process's lifetime.
const githubLoginCacheTTL = 24 * time.Hour

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
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	realUploader, err := upload.New(upload.Config{
		ClientSource: lazy.client,
		TokenSource:  lazy.accessToken,
		StateDir:     filepath.Join(carryonDir, "state"),
		Snapshotter:  snap,
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	uploader := filter.New(filter.Config{
		Inner:        realUploader,
		MatchProject: lazy.matchProject,
		SkippedPath:  filepath.Join(carryonDir, "skipped-repos.json"),
	})

	a, err := agent.New(agent.Config{
		QueuePath:  filepath.Join(carryonDir, "queue.jsonl"),
		CursorPath: filepath.Join(carryonDir, "agent-cursor.json"),
		StatusPath: filepath.Join(carryonDir, "agent-status.json"),
		Version:    version,
		Uploader:   uploader,
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	go func() { p.done <- a.Run(ctx) }()
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
