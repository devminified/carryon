package loadtest

import (
	"encoding/json"
	"math/rand"
	"testing"
)

func TestNextTurnProducesValidJSONLine(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	line := nextTurn(rnd, "sess-1", "/repo", 0)
	if len(line) == 0 || line[len(line)-1] != '\n' {
		t.Fatalf("nextTurn did not end with a newline: %q", line)
	}
	var out turnLine
	if err := json.Unmarshal(line[:len(line)-1], &out); err != nil {
		t.Fatalf("nextTurn produced invalid JSON: %v", err)
	}
	if out.SessionID != "sess-1" || out.Cwd != "/repo" || out.Seq != 0 {
		t.Fatalf("nextTurn: unexpected fields: %+v", out)
	}
}

func TestNextTurnOccasionallyProducesLargeToolOutput(t *testing.T) {
	// A fixed seed makes this deterministic — the point is proving the
	// large-output branch fires at all and stays close to
	// largeToolOutputBytes, not exercising true randomness.
	rnd := rand.New(rand.NewSource(1))
	foundLarge := false
	for i := 0; i < 200; i++ {
		line := nextTurn(rnd, "sess-1", "/repo", i)
		if len(line) > largeToolOutputBytes/2 {
			foundLarge = true
			if len(line) < largeToolOutputBytes {
				t.Errorf("large tool output line (%d bytes) is smaller than largeToolOutputBytes (%d)", len(line), largeToolOutputBytes)
			}
		}
	}
	if !foundLarge {
		t.Fatalf("expected at least one large tool-output line in 200 turns at chance %.2f", toolOutputChance)
	}
}

func TestNextTurnMostlyProducesSmallLines(t *testing.T) {
	rnd := rand.New(rand.NewSource(2))
	small := 0
	for i := 0; i < 200; i++ {
		line := nextTurn(rnd, "sess-1", "/repo", i)
		if len(line) < 1024 {
			small++
		}
	}
	// toolOutputChance is 0.05 — the overwhelming majority of 200
	// turns should be ordinary short replies, not 10MB tool outputs.
	if small < 150 {
		t.Fatalf("expected most turns to be small; got only %d/200 small turns", small)
	}
}
