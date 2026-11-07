// CO-209: place a rebuilt, rewritten session's files where Claude Code
// itself looks for them — ~/.claude/projects/<claudepath.Encode(cwd)>
// — verified against a live, pinned Claude Code run's own real
// folder/file layout (see claudepath.Encode's doc comment, and
// internal/reader.Subagents' own doc comment for the subagents/
// convention this mirrors in reverse). Claude Code never sees a
// partial file: each destination is created with O_EXCL, so a
// same-named file already existing there aborts cleanly rather than
// silently overwriting it, and a failed copy removes what it started
// instead of leaving a corrupt one behind.
package pull

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/devminified/carryon/internal/claudepath"
)

// ErrAlreadyPlaced is returned by Place when a file already exists at
// the destination a new (freshly generated, CO-208) session ID maps
// to — this should be essentially impossible in practice, but CO-209
// calls for refusing to overwrite regardless.
var ErrAlreadyPlaced = errors.New("a session file already exists at this id's destination")

// PlaceInput is what Place needs: the already-rebuilt-and-rewritten
// files (CO-205/207/208's output) and where Claude Code should find
// them.
type PlaceInput struct {
	// WorktreePath is the restored worktree's absolute path (CO-206) —
	// encoded to find Claude Code's project folder.
	WorktreePath string
	// SessionID is the new session ID (CO-208) — names the placed file.
	SessionID string
	// MainFile is the rewritten session.jsonl to place.
	MainFile string
	// SubagentFiles maps each subagent's agent ID to its rewritten
	// transcript path (CO-109's reader.Subagent.AgentID).
	SubagentFiles map[string]string
}

// PlaceResult is what Place placed.
type PlaceResult struct {
	MainPath      string
	SubagentPaths map[string]string
}

// Place copies in.MainFile to
// ~/.claude/projects/<encoded>/<SessionID>.jsonl and every subagent
// file to .../<SessionID>/subagents/agent-<agentID>.jsonl, matching
// Claude Code's own layout exactly. Returns ErrAlreadyPlaced — wrapped,
// check with errors.Is — if the main file's destination already
// exists; a subagent collision is reported the same way.
func Place(in PlaceInput) (*PlaceResult, error) {
	projectDir, err := claudepath.ProjectDir(in.WorktreePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", projectDir, err)
	}

	mainDst := filepath.Join(projectDir, in.SessionID+".jsonl")
	if err := placeOneFile(in.MainFile, mainDst); err != nil {
		return nil, err
	}

	result := &PlaceResult{MainPath: mainDst, SubagentPaths: map[string]string{}}
	if len(in.SubagentFiles) == 0 {
		return result, nil
	}

	subDir := filepath.Join(projectDir, in.SessionID, "subagents")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", subDir, err)
	}
	for agentID, src := range in.SubagentFiles {
		dst := filepath.Join(subDir, "agent-"+agentID+".jsonl")
		if err := placeOneFile(src, dst); err != nil {
			return nil, err
		}
		result.SubagentPaths[agentID] = dst
	}
	return result, nil
}

// placeOneFile copies src to dst, refusing to overwrite an existing
// file at dst. O_EXCL on the destination itself is what makes this
// atomic and race-free — unlike the temp-file + rename pattern used
// everywhere else in this codebase, there's deliberately no rename
// here: renaming would silently replace an existing file on most
// platforms, exactly what this function must never do.
func placeOneFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- src is the caller's own rebuilt/rewritten file
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600) // #nosec G304 -- dst is built from claudepath.ProjectDir + our own session/agent IDs
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w: %s", ErrAlreadyPlaced, dst)
		}
		return fmt.Errorf("creating %s: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("copying to %s: %w", dst, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("fsyncing %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("closing %s: %w", dst, err)
	}
	return nil
}
