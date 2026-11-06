package reader

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Subagent is one subagent child stream of a session.
type Subagent struct {
	// AgentID is parsed from the filename ("agent-<AgentID>.jsonl").
	AgentID string
	// Path is the subagent transcript's full path.
	Path string
}

// Subagents returns the current subagent transcript files for the session
// whose main transcript is at transcriptPath: Claude Code places them at
// <transcriptPath-without-.jsonl>/subagents/agent-*.jsonl — verified
// against real Claude Code session data, not assumed. New subagents can
// appear mid-session, so callers re-list periodically rather than caching
// this once.
//
// A session with no subagents (the common case) returns an empty slice,
// not an error. Results are sorted by AgentID for a stable order across
// calls.
func Subagents(transcriptPath string) ([]Subagent, error) {
	sessionDir := strings.TrimSuffix(transcriptPath, ".jsonl")
	matches, err := filepath.Glob(filepath.Join(sessionDir, "subagents", "agent-*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("listing subagents for %s: %w", transcriptPath, err)
	}

	subagents := make([]Subagent, 0, len(matches))
	for _, m := range matches {
		base := filepath.Base(m)
		id := strings.TrimSuffix(strings.TrimPrefix(base, "agent-"), ".jsonl")
		subagents = append(subagents, Subagent{AgentID: id, Path: m})
	}

	sort.Slice(subagents, func(i, j int) bool { return subagents[i].AgentID < subagents[j].AgentID })
	return subagents, nil
}
