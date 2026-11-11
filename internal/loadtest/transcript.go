// Package loadtest implements CO-505: a scripted, repeatable simulation
// of many developers running Claude Code against a real Carryon backend
// at once — one real agent.Uploader per simulated developer, uploading
// a realistic, incrementally-growing transcript at a jittered cadence,
// plus a team-wide stream of real pulls (internal/pull.Rebuild) that
// verify NFR-04's byte-identical rebuild. Everything here drives the
// same production code paths (internal/upload, internal/pull,
// internal/api) a real laptop uses — this package only supplies the
// fake Claude Code content and the orchestration loop around it.
package loadtest

import (
	"encoding/json"
	"fmt"
	"math/rand"
)

// toolOutputChance is how often a simulated reply includes a large
// (10MB-class) tool result — the ticket's own "occasional 10 MB tool
// outputs" — rare enough that most turns look like an ordinary short
// exchange.
const toolOutputChance = 0.05

// largeToolOutputBytes matches the ticket's "10 MB tool outputs"
// wording. Built from a repeating, compressible pattern (real tool
// output — a build log, a large file read — is rarely high-entropy
// text) rather than random bytes, so it exercises chunking (CO-110's
// 8MB split) without pretending to be something redaction would flag.
const largeToolOutputBytes = 10 * 1024 * 1024

// turnLine is one simulated transcript line — enough fields for
// internal/redact, internal/reader and internal/upload's own metadata
// gathering to treat it like a real Claude Code JSONL line, without
// reproducing that schema in full (this package never parses a
// transcript back out of these lines; only the real pull/rebuild path
// does, against the real bytes written to disk).
type turnLine struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Type      string `json:"type"`
	Seq       int    `json:"seq"`
	Text      string `json:"text"`
}

// nextTurn returns the JSONL bytes for one simulated turn (ending in
// "\n", matching every other real writer in this codebase). rnd is
// injected (not package-global) so a test can assert the exact
// toolOutputChance/size behavior deterministically.
func nextTurn(rnd *rand.Rand, sessionID, cwd string, seq int) []byte {
	text := fmt.Sprintf("turn %d: can you look at %s/handler.go and explain the retry logic?", seq, cwd)
	if rnd.Float64() < toolOutputChance {
		text = largeToolOutputText(seq)
	}
	line := turnLine{SessionID: sessionID, Cwd: cwd, Type: "assistant", Seq: seq, Text: text}
	encoded, err := json.Marshal(line)
	if err != nil {
		// turnLine has no field that can fail to encode (plain strings
		// and an int) — a programmer error, not an expected failure.
		panic(fmt.Sprintf("loadtest: marshaling a synthetic turn: %v", err))
	}
	return append(encoded, '\n')
}

// largeToolOutputText builds a deterministic, compressible string of
// roughly largeToolOutputBytes — standing in for a real oversized tool
// result (a build log, a big file read) without needing a real build
// to produce one.
func largeToolOutputText(seq int) string {
	unit := fmt.Sprintf("build step %d: compiling... ok\n", seq)
	repeats := largeToolOutputBytes/len(unit) + 1
	buf := make([]byte, 0, len(unit)*repeats)
	for i := 0; i < repeats; i++ {
		buf = append(buf, unit...)
	}
	return string(buf)
}
