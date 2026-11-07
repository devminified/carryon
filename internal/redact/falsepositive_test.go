package redact

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// fakeUUID builds a v4-shaped UUID that varies with seed — not a real
// random UUID generator, just a deterministic test fixture.
func fakeUUID(seed int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("uuid-seed-%d", seed)))
	hexStr := hex.EncodeToString(h[:16])
	return fmt.Sprintf("%s-%s-4%s-8%s-%s",
		hexStr[0:8], hexStr[8:12], hexStr[13:16], hexStr[17:20], hexStr[20:32])
}

func fakeGitSHA(seed int, short bool) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("sha-seed-%d", seed)))
	s := hex.EncodeToString(h[:20]) // 40 hex chars, git-SHA-1-shaped length
	if short {
		return s[:7]
	}
	return s
}

func fakeBase64Image(seed int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("img-seed-%d", seed)))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(h[:])
}

func fakeBase64Blob(seed int, n int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("blob-seed-%d", seed)))
	enc := base64.StdEncoding.EncodeToString(h[:])
	for len(enc) < n {
		enc += enc
	}
	return enc[:n]
}

// nonSecretCorpus returns 500+ realistic, non-secret transcript lines:
// UUIDs, git SHAs, base64 images/blobs, ordinary code and prose — the
// CO-301 "false-positive rate under 1%" corpus.
func nonSecretCorpus() []string {
	var lines []string

	for i := 0; i < 160; i++ {
		lines = append(lines, realisticLine(fakeUUID(i), i))
	}
	for i := 0; i < 60; i++ {
		lines = append(lines, realisticLine(fakeGitSHA(i, false), i))
	}
	for i := 0; i < 60; i++ {
		lines = append(lines, realisticLine(fakeGitSHA(i, true), i))
	}
	for i := 0; i < 60; i++ {
		lines = append(lines, realisticLine(fakeBase64Image(i), i))
	}
	for i := 0; i < 60; i++ {
		lines = append(lines, realisticLine(fakeBase64Blob(i, 40+i%30), i))
	}

	// Ordinary code/prose lines, no embedded "secret-shaped" value at
	// all — these exercise the rule set against everyday transcript
	// content rather than one isolated substitution.
	codeLines := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"I'll update the function signature to accept a context.Context as its first argument."}]}}`,
		`{"type":"user","message":{"content":[{"tool_use_id":"toolu_a","type":"tool_result","content":"ok  \tgithub.com/devminified/carryon/internal/redact\t0.490s"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_b","name":"Bash","input":{"command":"go test ./... -race","description":"run tests"}}]}}`,
		`{"type":"user","message":{"content":[{"tool_use_id":"toolu_c","type":"tool_result","content":"total 48\ndrwxr-xr-x  12 ali  staff   384 Nov  8 10:00 .\n-rw-r--r--   1 ali  staff  1024 Nov  8 09:55 go.mod"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"The function at internal/redact/walk.go:45 handles the JSON walk."}]}}`,
		`{"type":"user","message":{"content":[{"tool_use_id":"toolu_d","type":"tool_result","content":"package redact\n\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n)"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Let's bump the timeout from 5 seconds to 15 seconds to avoid flakiness."}]}}`,
		`{"type":"user","message":{"content":[{"tool_use_id":"toolu_e","type":"tool_result","content":"Branch 'co-301-redaction-engine' set up to track 'origin/co-301-redaction-engine'."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Config.APIURL defaults to http://127.0.0.1:54421 for local dev."}]}}`,
		`{"type":"user","message":{"content":[{"tool_use_id":"toolu_f","type":"tool_result","content":"2026-11-08T10:15:30Z INFO starting agent, version=0.4.2"}]}}`,
	}
	for i := 0; i < 100; i++ {
		lines = append(lines, codeLines[i%len(codeLines)])
	}

	return lines
}

// TestFalsePositiveRateUnderOnePercent is CO-301's literal testing
// point: a corpus of non-secret lines, false-positive rate under 1%.
func TestFalsePositiveRateUnderOnePercent(t *testing.T) {
	corpus := nonSecretCorpus()
	if len(corpus) < 500 {
		t.Fatalf("corpus has %d lines, want at least 500", len(corpus))
	}

	var falsePositives []string
	for _, line := range corpus {
		r := NewRedactor(DefaultConfig())
		out, n, ok := r.RedactLine([]byte(line))
		if !ok {
			t.Errorf("non-secret line wasn't valid JSON (fixture bug): %s", line)
			continue
		}
		if n > 0 {
			falsePositives = append(falsePositives, fmt.Sprintf("%q -> %q", line, out))
		}
	}

	rate := float64(len(falsePositives)) / float64(len(corpus))
	if rate >= 0.01 {
		t.Errorf("false-positive rate = %.2f%% (%d/%d), want under 1%%:\n%s",
			rate*100, len(falsePositives), len(corpus), strings.Join(falsePositives, "\n"))
	}
}
