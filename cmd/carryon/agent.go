package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/filter"
	"github.com/devminified/carryon/internal/service"
	kservice "github.com/kardianos/service"
	"github.com/spf13/cobra"
)

// stubUploader is a placeholder agent.Uploader until CO-109 (incremental
// reader) and CO-110 (chunk upload pipeline) land — filter.Filter (CO-108)
// is real and wraps this, so a session that passes the project-registration
// check reaches here and this is as far as the pipeline goes for now.
type stubUploader struct{}

func (stubUploader) Upload(_ context.Context, session agent.Session) error {
	slog.Info("would upload session (not yet implemented, see CO-109/CO-110)", "session_id", session.ID)
	return nil
}

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
	mu       sync.Mutex
	client   *api.Client
	provider *auth.Provider
}

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

func (l *lazyAPI) resolve() (*api.Client, *auth.Provider, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client != nil {
		return l.client, l.provider, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	provider, err := auth.NewProvider(cfg)
	if err != nil {
		return nil, nil, err
	}

	l.client = api.NewClient(cfg.APIURL, cfg.AnonKey)
	l.provider = provider
	return l.client, l.provider, nil
}

// agentProgram adapts internal/agent.Agent to kservice.Interface. Start
// launches the agent's Run loop in the background; Stop cancels it and
// waits for it to finish — this is what gives in-flight uploads a chance
// to persist state before exit (go-conventions.md), and on Windows it's
// the *only* way to be notified of a stop at all (see service package doc
// comment).
type agentProgram struct {
	cancel context.CancelFunc
	done   chan error
}

func (p *agentProgram) Start(kservice.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)

	carryonDir, err := config.Dir()
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	lazy := &lazyAPI{}
	uploader := filter.New(filter.Config{
		Inner:        stubUploader{},
		MatchProject: lazy.matchProject,
	})

	a, err := agent.New(agent.Config{
		QueuePath:  filepath.Join(carryonDir, "queue.jsonl"),
		CursorPath: filepath.Join(carryonDir, "agent-cursor.json"),
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
	return <-p.done
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
