// Package snapshot implements CO-201: after each successful chunk
// upload, taking a non-destructive snapshot of the session's working
// tree — using a temporary git index so the developer's real index,
// stash, branch and index.lock are never touched — and pushing it as a
// commit to a hidden ref on the repo's own GitHub remote. Bytes never
// flow through Supabase for this (I9): the push is laptop-to-GitHub
// directly, authenticated with the developer's own git credentials;
// register-snapshot only ever receives the resulting pointer
// (session_id, seq, head_sha, wip_ref, tree_sha).
package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/project"
)

// minInterval is CO-201's "snapshots run at most every 5 minutes per
// session, plus once on SessionEnd" — the per-session rate limit
// MaybeSnapshot enforces unless final is set.
const minInterval = 5 * time.Minute

// maxUntrackedFileSize is CO-201's "untracked files larger than 50MB
// are excluded" — only ever applies to *untracked* files; a tracked
// file that happens to be large is never excluded (excluding it would
// silently corrupt the snapshot's tree relative to what's actually
// checked in). A var, not a const, so tests can shrink it rather than
// writing a real 50MB+ fixture file.
var maxUntrackedFileSize int64 = 50 * 1024 * 1024

// TokenSource returns a valid access token for the caller.
type TokenSource func(ctx context.Context) (string, error)

// ClientSource returns the Edge Function client to use.
type ClientSource func() (*api.Client, error)

// CurrentUserFunc returns the logged-in developer's GitHub login — used
// to build the hidden ref path refs/carryon/<login>/<session_id>, since
// that's who the snapshot commit belongs to on their own GitHub remote.
type CurrentUserFunc func(ctx context.Context) (string, error)

// Config configures a Snapshotter. Every field is required.
type Config struct {
	ClientSource ClientSource
	TokenSource  TokenSource
	CurrentUser  CurrentUserFunc
	// StateDir is ~/.carryon/snapshot-state — this package's own
	// per-session Pointer files live here, one per session.
	StateDir string
	// RepoStateDir is ~/.carryon/snapshot-repo-state (CO-202) — where the
	// per-repo "the ref push was rejected, use the bundle fallback for a
	// while" memory lives. Required; a repo's rejection state must
	// survive across sessions and agent restarts, same as everything
	// else this package persists.
	RepoStateDir string
	// HTTPClient is used for the PUT to a fallback bundle's signed
	// upload URL (CO-202). Defaults to a 60-second-timeout client.
	HTTPClient *http.Client

	// now is overridable so tests can control the 5-minute rate limit
	// deterministically without real sleeps.
	now func() time.Time
}

// Snapshotter implements CO-201's snapshot capture.
type Snapshotter struct {
	clientSource ClientSource
	tokenSource  TokenSource
	currentUser  CurrentUserFunc
	stateDir     string
	repoStateDir string
	httpClient   *http.Client
	now          func() time.Time
}

// New validates cfg and returns a Snapshotter.
func New(cfg Config) (*Snapshotter, error) {
	if cfg.ClientSource == nil {
		return nil, fmt.Errorf("snapshot: ClientSource is required")
	}
	if cfg.TokenSource == nil {
		return nil, fmt.Errorf("snapshot: TokenSource is required")
	}
	if cfg.CurrentUser == nil {
		return nil, fmt.Errorf("snapshot: CurrentUser is required")
	}
	if cfg.StateDir == "" {
		return nil, fmt.Errorf("snapshot: StateDir is required")
	}
	if cfg.RepoStateDir == "" {
		return nil, fmt.Errorf("snapshot: RepoStateDir is required")
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Snapshotter{
		clientSource: cfg.ClientSource,
		tokenSource:  cfg.TokenSource,
		currentUser:  cfg.CurrentUser,
		stateDir:     cfg.StateDir,
		repoStateDir: cfg.RepoStateDir,
		httpClient:   httpClient,
		now:          now,
	}, nil
}

// MaybeSnapshot takes a new working-tree snapshot of sessionID's repo
// at cwd if enough time has passed since the last attempt (final, for
// SessionEnd, bypasses that gate) and the tree has actually changed
// since the last snapshot taken. cwd not being inside a git repository
// (or having no remote) is reported as success, not an error — nothing
// to snapshot is a normal outcome, not a failure of the upload that
// triggered this check.
func (s *Snapshotter) MaybeSnapshot(ctx context.Context, sessionID, cwd string, final bool) error {
	prior, err := Load(s.stateDir, sessionID)
	if err != nil {
		return fmt.Errorf("loading snapshot state: %w", err)
	}

	now := s.now()
	if !final && !prior.LastAttemptAt.IsZero() && now.Sub(prior.LastAttemptAt) < minInterval {
		return nil
	}

	toplevel, err := project.Toplevel(ctx, cwd)
	if err != nil {
		return nil // no repo — nothing to snapshot
	}

	indexPath, cleanup, err := newTempIndex()
	if err != nil {
		return err
	}
	defer cleanup()

	// Empty headSHA means no commits yet — handled below by omitting
	// read-tree's seed and commit-tree's parent, not by failing.
	headSHA, _ := project.HeadSHA(ctx, toplevel)

	if headSHA != "" {
		if err := readTreeHead(ctx, toplevel, indexPath); err != nil {
			return fmt.Errorf("reading HEAD tree into temp index: %w", err)
		}
	}
	if err := addAll(ctx, toplevel, indexPath); err != nil {
		return fmt.Errorf("staging working tree: %w", err)
	}

	excluded, err := excludeLargeUntrackedFiles(ctx, toplevel, indexPath)
	if err != nil {
		return fmt.Errorf("excluding large untracked files: %w", err)
	}

	treeSHA, err := writeTree(ctx, toplevel, indexPath)
	if err != nil {
		return fmt.Errorf("writing tree: %w", err)
	}

	next := Pointer{
		TreeSHA:            treeSHA,
		NextSeq:            prior.NextSeq,
		LastAttemptAt:      now,
		ExcludedLargeFiles: excluded,
	}

	if treeSHA == prior.TreeSHA {
		// Unchanged: no commit, no push, no register-snapshot call —
		// but LastAttemptAt (and a possibly-updated excluded-files
		// list) still needs persisting so the rate limit and status
		// warning both stay current.
		return Save(s.stateDir, sessionID, next)
	}

	commitSHA, err := commitTree(ctx, toplevel, treeSHA, headSHA, fmt.Sprintf("carryon snapshot %s", sessionID))
	if err != nil {
		return fmt.Errorf("creating commit object: %w", err)
	}

	login, err := s.currentUser(ctx)
	if err != nil {
		return fmt.Errorf("getting current user: %w", err)
	}
	remoteName, err := project.RemoteName(ctx, toplevel)
	if err != nil {
		return fmt.Errorf("resolving remote: %w", err)
	}
	refName := "refs/carryon/" + login + "/" + sessionID

	// remoteURL (best-effort) keys the per-repo "use the bundle fallback
	// for a while" memory (CO-202) — two sessions in the same repo must
	// share it, since the rejection reason (a pre-receive hook, no push
	// permission) is a property of the remote, not of either session.
	// RemoteURL's raw form, not ResolveRemote's normalized one — this
	// bookkeeping only needs to be stable for this repo's own git
	// config, not to match a server-registered project.
	remoteURL, _ := project.RemoteURL(ctx, toplevel)

	useBundle := false
	if remoteURL != "" {
		fb, _ := loadRepoFallback(s.repoStateDir, remoteURL)
		if !fb.LastRejectedAt.IsZero() && now.Sub(fb.LastRejectedAt) < bundleFallbackWindow {
			useBundle = true
		}
	}

	regInput := api.RegisterSnapshotInput{
		SessionID: sessionID,
		Seq:       next.NextSeq,
		HeadSHA:   headSHA,
		TreeSHA:   treeSHA,
	}

	if !useBundle {
		if err := pushSnapshot(ctx, toplevel, remoteName, commitSHA, refName); err != nil {
			if remoteURL != "" {
				_ = saveRepoFallback(s.repoStateDir, remoteURL, repoFallback{LastRejectedAt: now})
			}
			useBundle = true
		} else {
			regInput.WipRef = refName
		}
	}

	if useBundle {
		if err := s.uploadBundle(ctx, toplevel, commitSHA, remoteName, sessionID, next.NextSeq); err != nil {
			return fmt.Errorf("uploading fallback bundle: %w", err)
		}
	}

	token, err := s.tokenSource(ctx)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}
	client, err := s.clientSource()
	if err != nil {
		return fmt.Errorf("getting api client: %w", err)
	}
	if err := client.RegisterSnapshot(ctx, token, regInput); err != nil {
		return fmt.Errorf("registering snapshot: %w", err)
	}

	next.NextSeq = prior.NextSeq + 1
	return Save(s.stateDir, sessionID, next)
}

// uploadBundle is CO-202's fallback path: create a bundle of only the
// objects missing from remoteName, get a signed upload URL for it via
// sign-snapshot-bundle, and PUT it there.
func (s *Snapshotter) uploadBundle(ctx context.Context, toplevel, commitSHA, remoteName, sessionID string, seq int) error {
	bundlePath, cleanup, err := createBundle(ctx, toplevel, commitSHA, remoteName, sessionID)
	if err != nil {
		return fmt.Errorf("creating bundle: %w", err)
	}
	defer cleanup()

	data, err := os.ReadFile(bundlePath) // #nosec G304 -- bundlePath is our own temp file, created a few lines above
	if err != nil {
		return fmt.Errorf("reading bundle file: %w", err)
	}

	token, err := s.tokenSource(ctx)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}
	client, err := s.clientSource()
	if err != nil {
		return fmt.Errorf("getting api client: %w", err)
	}

	signed, err := client.SignSnapshotBundle(ctx, token, api.SignSnapshotBundleInput{SessionID: sessionID, Seq: seq})
	if err != nil {
		return fmt.Errorf("signing bundle upload: %w", err)
	}

	return s.put(ctx, client.StorageUploadURL(signed.ObjectKey, signed.UploadToken), data)
}

func (s *Snapshotter) put(ctx context.Context, url string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("building PUT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("PUT returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// excludeLargeUntrackedFiles drops every untracked file over
// maxUntrackedFileSize from indexPath (addAll will already have staged
// it) and returns their paths, so the caller can persist/report them.
func excludeLargeUntrackedFiles(ctx context.Context, repoDir, indexPath string) ([]string, error) {
	files, err := untrackedFiles(ctx, repoDir)
	if err != nil {
		return nil, err
	}

	var excluded []string
	for _, f := range files {
		info, err := os.Stat(filepath.Join(repoDir, f)) // #nosec G304 -- f comes from `git ls-files` under repoDir, not external input
		if err != nil {
			continue // raced with a delete between ls-files and stat — nothing left to exclude
		}
		if info.Size() <= maxUntrackedFileSize {
			continue
		}
		if err := forceRemoveFromIndex(ctx, repoDir, indexPath, f); err != nil {
			return excluded, err
		}
		excluded = append(excluded, f)
	}
	return excluded, nil
}
