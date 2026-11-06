package upload

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/devminified/carryon/internal/project"
)

// claudeVersionCacheTTL is CO-111's "claude --version cached per day" —
// re-running it on every chunk (as often as every UploadInterval) would
// be wasteful for a value that only changes when the developer updates
// Claude Code.
const claudeVersionCacheTTL = 24 * time.Hour

// metadataCommandTimeout bounds the "claude --version" exec, mirroring
// internal/project's gitCommandTimeout — this is a local, near-instant
// call, not an expected source of latency.
const metadataCommandTimeout = 5 * time.Second

// sessionMetadata is CO-111's session-identifying fields: which machine,
// which git branch and commit, and which Claude Code version produced
// this session. Gathering any one of them is best-effort — see
// gatherMetadata — so every field may legitimately be empty.
type sessionMetadata struct {
	Machine           string
	Branch            string
	HeadSHA           string
	ClaudeCodeVersion string
}

// gatherMetadata collects sessionMetadata for cwd. Every field is
// best-effort: a failure gathering any one of them (no git repo, "claude"
// not on PATH, hostname lookup failing) leaves it empty rather than ever
// failing the upload itself, which is the one thing that must not fail
// over a metadata-gathering problem.
func gatherMetadata(ctx context.Context, cwd string) sessionMetadata {
	branch, _ := project.CurrentBranch(ctx, cwd)
	headSHA, _ := project.HeadSHA(ctx, cwd)
	return sessionMetadata{
		Machine:           hostname(),
		Branch:            branch,
		HeadSHA:           headSHA,
		ClaudeCodeVersion: cachedClaudeVersion(ctx),
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

var (
	claudeVersionMu sync.Mutex
	claudeVersion   string
	claudeVersionAt time.Time
)

// cachedClaudeVersion returns "claude --version"'s output, re-running it
// at most once per claudeVersionCacheTTL. Shared across every Uploader
// instance in the process — the version is a property of the machine,
// not of any one session, so there's nothing to gain from caching it per
// session. A failed exec (e.g. a transient PATH issue) keeps the last
// known-good value rather than blanking it out, and is retried on the
// very next call rather than caching the failure for a day.
func cachedClaudeVersion(ctx context.Context) string {
	claudeVersionMu.Lock()
	defer claudeVersionMu.Unlock()

	if time.Since(claudeVersionAt) < claudeVersionCacheTTL {
		return claudeVersion
	}

	cctx, cancel := context.WithTimeout(ctx, metadataCommandTimeout)
	defer cancel()

	out, err := exec.CommandContext(cctx, "claude", "--version").Output() // #nosec G204 -- fixed command, no caller input
	if err != nil {
		return claudeVersion
	}

	claudeVersion = strings.TrimSpace(string(out))
	claudeVersionAt = time.Now()
	return claudeVersion
}
