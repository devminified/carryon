package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/service"
	kservice "github.com/kardianos/service"
	"github.com/spf13/cobra"
)

// logUploader is a placeholder agent.Uploader until CO-108 (project
// filter), CO-109 (incremental reader) and CO-110 (chunk upload pipeline)
// land — CO-107 only needs something that satisfies the interface so its
// scheduling engine (debounce, worker pool, cursor, compaction) runs for
// real against genuine queue.jsonl events.
type logUploader struct{}

func (logUploader) Upload(_ context.Context, sessionID string) error {
	slog.Info("would upload session (not yet implemented, see CO-108/CO-109/CO-110)", "session_id", sessionID)
	return nil
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

	a, err := agent.New(agent.Config{
		QueuePath:  filepath.Join(carryonDir, "queue.jsonl"),
		CursorPath: filepath.Join(carryonDir, "agent-cursor.json"),
		Uploader:   logUploader{},
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
