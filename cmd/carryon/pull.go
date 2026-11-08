package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/project"
	"github.com/devminified/carryon/internal/pull"
)

// pullFlags collects pull's flags so runPull stays a plain function of
// its inputs — same reasoning as ls's lsFlags.
type pullFlags struct {
	noLaunch bool
	yes      bool
	clean    bool
}

func newPullCmd() *cobra.Command {
	var f pullFlags
	cmd := &cobra.Command{
		Use:   "pull <session-id>",
		Short: "Pull and resume a session on this machine",
		Long: "Pull and resume a session on this machine: fetch its manifest,\n" +
			"locate or clone the project, restore its code snapshot into a\n" +
			"dedicated worktree, rebuild and rewrite the transcript, place it\n" +
			"where Claude Code looks for it, and resume.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPull(cmd, args[0], f)
		},
	}
	cmd.Flags().BoolVar(&f.noLaunch, "no-launch", false, "print the resume command instead of running it")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "clone the project automatically if no local copy is found")
	cmd.Flags().BoolVar(&f.clean, "clean", false, "remove old Carryon worktrees for this project before pulling")
	return cmd
}

// claudeBinEnv lets a test (or an operator with a non-standard install)
// point Launch/LocalClaudeVersion at a different claude binary, without
// adding a test-only code path to the normal pull/export flow.
const claudeBinEnv = "CARRYON_CLAUDE_BIN"

// pullHTTPTimeout matches pull.DefaultConfig's own chunk-download client.
const pullHTTPTimeout = 60 * time.Second

// cloneURL reconstructs a clonable URL from a project's canonical
// remote (project.NormalizeRemote's "host/path" form — the only shape
// the server stores, see get-manifest's gitRemote field). HTTPS is the
// one scheme that works unauthenticated for a public repo and via
// whatever git credential helper / `gh auth` setup is already on this
// machine for a private one — the same assumption CO-104/CO-108 already
// make about a developer's own git handling clone auth, just applied
// to a URL this package has to rebuild itself rather than one already
// sitting in a known git remote.
func cloneURL(canonicalRemote string) string {
	return "https://" + canonicalRemote
}

// stepErr wraps a failing step's error so it names which step failed
// and how to retry — every step runPull calls is either read-only or
// safe to simply re-run (LocateClone/RestoreWorktree reuse what's
// already there; Rebuild re-downloads; rewrite/Place target a freshly
// generated session ID each attempt, so a half-finished previous
// attempt's files are just orphaned, never collided with).
func stepErr(step, sessionID string, err error) error {
	return fmt.Errorf("%s: %w — re-run `carryon pull %s` to retry", step, err, sessionID)
}

func printStep(cmd *cobra.Command, name, detail string) {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "→ %s: %s\n", name, detail)
}

func runPull(cmd *cobra.Command, sessionID string, f pullFlags) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	provider, err := auth.NewProvider(cfg)
	if err != nil {
		return err
	}
	token, err := provider.AccessToken(cmd.Context())
	if err != nil {
		return err
	}
	client := api.NewClient(cfg.APIURL, cfg.AnonKey)

	return pullWithClient(cmd, client, token, cfg, sessionID, f)
}

// pullWithClient is runPull's actual pipeline, taking an already-
// resolved client/token/config rather than loading them itself — the
// seam that lets a test drive the real pipeline against a real local
// Supabase with a real access token (from a direct password-grant call)
// without touching auth.Provider's own credential store, which is
// backed by the real OS keychain on this machine and must never be
// written to by an automated test.
func pullWithClient(cmd *cobra.Command, client *api.Client, token string, cfg *config.Config, sessionID string, f pullFlags) error {
	ctx := cmd.Context()

	manifest, err := client.GetManifest(ctx, token, sessionID)
	if err != nil {
		return stepErr("manifest", sessionID, err)
	}
	printStep(cmd, "manifest", fmt.Sprintf("fetched (%d chunks)", len(manifest.Chunks)))

	locateResult, err := locateProject(ctx, cmd, cfg, manifest, f.yes)
	if err != nil {
		return stepErr("code", sessionID, err)
	}
	if locateResult.Cloned {
		if cfg.Repos == nil {
			cfg.Repos = map[string]string{}
		}
		cfg.Repos[manifest.Session.GitRemote] = locateResult.Path
		if err := config.Save(cfg); err != nil {
			return stepErr("code", sessionID, fmt.Errorf("saving clone location: %w", err))
		}
		printStep(cmd, "code", "cloned into "+locateResult.Path)
	} else {
		printStep(cmd, "code", "found an existing local clone at "+locateResult.Path)
	}

	if f.clean {
		removed, err := pull.CleanWorktrees(ctx, locateResult.Path)
		if err != nil {
			return stepErr("code", sessionID, fmt.Errorf("cleaning old worktrees: %w", err))
		}
		if len(removed) > 0 {
			printStep(cmd, "code", fmt.Sprintf("removed %d old worktree(s)", len(removed)))
		}
	}

	httpClient := &http.Client{Timeout: pullHTTPTimeout}
	restoreResult, err := pull.RestoreWorktree(ctx, client, httpClient, locateResult.Path, pull.RestoreInput{
		SessionID:   sessionID,
		ProjectName: manifest.Session.ProjectName,
		HeadSHA:     manifest.Session.HeadSHA,
		Snapshot:    manifest.Snapshot,
	})
	if err != nil {
		return stepErr("code", sessionID, err)
	}
	worktreeDetail := "worktree ready at " + restoreResult.Path
	if restoreResult.Warning != "" {
		worktreeDetail += " (" + restoreResult.Warning + ")"
	}
	printStep(cmd, "code", worktreeDetail)

	rebuildDir, err := os.MkdirTemp("", "carryon-pull-")
	if err != nil {
		return stepErr("chunks", sessionID, fmt.Errorf("creating a temp directory: %w", err))
	}
	defer func() { _ = os.RemoveAll(rebuildDir) }()

	rebuildResult, err := pull.Rebuild(ctx, client, manifest, rebuildDir, pull.DefaultConfig())
	if err != nil {
		return stepErr("chunks", sessionID, err)
	}
	printStep(cmd, "chunks", fmt.Sprintf("rebuilt %d bytes across %d stream(s)", rebuildResult.TotalBytes, 1+len(rebuildResult.SubagentFiles)))

	pathRewritten, err := rewriteEveryFile(rebuildDir, "path-rewritten", rebuildResult.MainFile, rebuildResult.SubagentFiles,
		func(src, dst string) error {
			_, err := pull.NewPathRewriter(manifest.Session.OriginalCwd, restoreResult.Path).RewriteFile(src, dst)
			return err
		})
	if err != nil {
		return stepErr("rewrite", sessionID, fmt.Errorf("rewriting paths: %w", err))
	}
	printStep(cmd, "rewrite", "paths updated to "+restoreResult.Path)

	newID, err := pull.NewSessionID()
	if err != nil {
		return stepErr("rewrite", sessionID, err)
	}
	idRewritten, err := rewriteEveryFile(rebuildDir, "id-rewritten", pathRewritten.main, pathRewritten.subagents,
		func(src, dst string) error {
			_, err := pull.RewriteSessionIDFile(src, dst, newID)
			return err
		})
	if err != nil {
		return stepErr("rewrite", sessionID, fmt.Errorf("rewriting session id: %w", err))
	}
	printStep(cmd, "rewrite", "forked as "+newID)

	if _, err := client.RegisterFork(ctx, token, api.RegisterForkInput{ParentSessionID: sessionID, NewSessionID: newID}); err != nil {
		return stepErr("place", sessionID, fmt.Errorf("registering fork: %w", err))
	}

	placeResult, err := pull.Place(pull.PlaceInput{
		WorktreePath:  restoreResult.Path,
		SessionID:     newID,
		MainFile:      idRewritten.main,
		SubagentFiles: idRewritten.subagents,
	})
	if err != nil {
		return stepErr("place", sessionID, err)
	}
	printStep(cmd, "place", placeResult.MainPath)

	// CO-304: the pull itself has now fully succeeded (manifest fetched,
	// worktree restored, chunks rebuilt, paths/session ID rewritten, fork
	// registered, files placed) — audit that regardless of whether the
	// user goes on to actually launch Claude Code below. Best-effort: a
	// logging failure here is never a reason to fail a pull that already
	// succeeded.
	if err := client.ConfirmPull(ctx, token, newID); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to record pull_complete: %v\n", err)
	}

	uncommitted, err := project.UncommittedFileCount(ctx, restoreResult.Path)
	if err != nil {
		uncommitted = 0 // best-effort for the header only — never fails the pull over it
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), pull.FormatHeader(pull.HeaderInfo{
		Owner:             manifest.Session.OwnerLogin,
		Branch:            manifest.Session.Branch,
		LastActivity:      manifest.Session.LastActivityAt,
		UncommittedFiles:  uncommitted,
		ClaudeCodeVersion: manifest.Session.ClaudeCodeVersion,
	}))

	claudeBin := os.Getenv(claudeBinEnv)
	if localVersion, err := pull.LocalClaudeVersion(ctx, claudeBin); err == nil {
		if pull.NeedsVersionWarning(manifest.Session.ClaudeCodeVersion, localVersion) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"Warning: this session was recorded with Claude Code %s, but this machine has %s.\n",
				manifest.Session.ClaudeCodeVersion, localVersion)
			if !confirm(cmd, "Continue anyway?") {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
				return nil
			}
		}
	}

	printStep(cmd, "launch", "resuming "+newID)
	return pull.Launch(ctx, pull.LaunchInput{
		WorktreePath: restoreResult.Path,
		SessionID:    newID,
		NoLaunch:     f.noLaunch,
		ClaudeBin:    claudeBin,
		Stdin:        cmd.InOrStdin(),
		Stdout:       cmd.OutOrStdout(),
		Stderr:       cmd.ErrOrStderr(),
	})
}

// locateProject finds or clones a local checkout of the session's
// project, honoring --yes as an always-confirm ConfirmFunc.
func locateProject(ctx context.Context, cmd *cobra.Command, cfg *config.Config, manifest *api.Manifest, yes bool) (*pull.LocateResult, error) {
	cwd, _ := os.Getwd() // best-effort — LocateClone treats "" as "no candidate from cwd"

	var confirmFn pull.ConfirmFunc
	if yes {
		confirmFn = func() bool { return true }
	} else {
		confirmFn = func() bool {
			return confirm(cmd, fmt.Sprintf("No local clone of %s found — clone it into ~/carryon/repos/%s?",
				manifest.Session.GitRemote, manifest.Session.ProjectName))
		}
	}

	return pull.LocateClone(ctx, pull.LocateInput{
		Cwd:             cwd,
		ProjectName:     manifest.Session.ProjectName,
		CanonicalRemote: manifest.Session.GitRemote,
		GitRemoteURL:    cloneURL(manifest.Session.GitRemote),
		Repos:           cfg.Repos,
	}, confirmFn)
}

// rewrittenFiles is one rewrite pass's output: the main file's new path
// and each subagent's, keyed the same way pull.Rebuild/pull.Place
// already key them (by stream/agent ID).
type rewrittenFiles struct {
	main      string
	subagents map[string]string
}

// rewriteEveryFile applies apply (either PathRewriter.RewriteFile or
// RewriteSessionIDFile, bound by the caller) to the main file and every
// subagent file, writing each into dir/subdir/ under its own original
// base name so the two rewrite passes (path, then session ID) chain
// without colliding. Subagent files are rewritten for exactly the same
// reason as the main file: a real subagent transcript carries the same
// "sessionId" field and can reference the same working-tree paths
// (verified against a real Claude Code run with a spawned subagent —
// its transcript's own sessionId matched the main session's, not a
// separate parentSessionId field).
func rewriteEveryFile(dir, subdir, mainSrc string, subagentSrc map[string]string, apply func(src, dst string) error) (*rewrittenFiles, error) {
	outDir := filepath.Join(dir, subdir)
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", outDir, err)
	}

	mainDst := filepath.Join(outDir, filepath.Base(mainSrc))
	if err := apply(mainSrc, mainDst); err != nil {
		return nil, fmt.Errorf("rewriting %s: %w", mainSrc, err)
	}

	out := &rewrittenFiles{main: mainDst, subagents: map[string]string{}}
	for agentID, src := range subagentSrc {
		dst := filepath.Join(outDir, filepath.Base(src))
		if err := apply(src, dst); err != nil {
			return nil, fmt.Errorf("rewriting subagent %s (%s): %w", agentID, src, err)
		}
		out.subagents[agentID] = dst
	}
	return out, nil
}
