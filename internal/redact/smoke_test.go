package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSmokeRedactsASimpleAnthropicKey(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	in := `{"type":"user","message":{"content":"my key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}}`
	out, n, ok := r.RedactLine([]byte(in))
	if !ok {
		t.Fatal("ok = false, want true (valid JSON)")
	}
	if n != 1 {
		t.Fatalf("redactions = %d, want 1", n)
	}
	if strings.Contains(string(out), "sk-ant-api03") {
		t.Errorf("output still contains the secret: %s", out)
	}
	if !strings.Contains(string(out), "[REDACTED:anthropic-api-key]") {
		t.Errorf("output missing placeholder: %s", out)
	}
	// still valid JSON
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, out)
	}
}

// TestSmokeOverlappingRuleMatchExtendsCoverageInsteadOfDroppingIt is a
// regression test for a real I5 bug found via CO-301's own real-world
// validation (NFR-07): generic-api-key's "<label>-key: <value>" shape
// can match starting earlier than, but ending before, a more specific
// rule's own match for the real value that follows — e.g.
// "aws-secret-access-key: aws_secret_access_key=<40-char value>" has
// generic-api-key matching "access-key: aws_secret_access_key" (ending
// right before "="), while aws-secret-access-key's own rule matches
// "aws_secret_access_key=<value>" starting inside that same range.
// resolveOverlaps used to just discard the second, longer match
// outright because its start fell within the first match's range —
// which left the real secret value (everything from "=" onward,
// covered only by the discarded match) completely unredacted. It must
// now extend the kept span's coverage instead of dropping the overlap.
func TestSmokeOverlappingRuleMatchExtendsCoverageInsteadOfDroppingIt(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	secretValue := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN" // 40 chars
	in, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": "aws-secret-access-key: aws_secret_access_key=" + secretValue,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	out, n, ok := r.RedactLine(in)
	if !ok {
		t.Fatal("ok = false, want true (valid JSON)")
	}
	if n == 0 {
		t.Fatal("redactions = 0, want at least 1")
	}
	if strings.Contains(string(out), secretValue) {
		t.Fatalf("the real secret value leaked past redaction: %s", out)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
}

func TestResolveOverlapsExtendsAnOverlappingLongerSpanInsteadOfDroppingIt(t *testing.T) {
	// span B starts inside span A but ends further — the exact shape
	// that used to lose span B's extra coverage entirely.
	spans := []ruleSpan{
		{start: 0, end: 10, id: "a"},
		{start: 5, end: 20, id: "b"},
	}
	got := resolveOverlaps(spans)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (merged into one covering span)", len(got))
	}
	if got[0].start != 0 || got[0].end != 20 {
		t.Fatalf("got[0] = {%d, %d}, want {0, 20} (full union of both spans)", got[0].start, got[0].end)
	}
}

func TestResolveOverlapsDropsASpanFullyContainedInAnAlreadyKeptOne(t *testing.T) {
	// span B is entirely inside span A — nothing to extend, B is just
	// redundant with A.
	spans := []ruleSpan{
		{start: 0, end: 20, id: "a"},
		{start: 5, end: 10, id: "b"},
	}
	got := resolveOverlaps(spans)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].start != 0 || got[0].end != 20 {
		t.Fatalf("got[0] = {%d, %d}, want {0, 20} (unchanged — B added no new coverage)", got[0].start, got[0].end)
	}
}

func TestResolveOverlapsPrefersTheLongerSpanAtTheSameStart(t *testing.T) {
	spans := []ruleSpan{
		{start: 0, end: 5, id: "short"},
		{start: 0, end: 15, id: "long"},
	}
	got := resolveOverlaps(spans)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].end != 15 {
		t.Fatalf("got[0].end = %d, want 15 (the longer match at the same start sorts first and is kept)", got[0].end)
	}
}

func TestResolveOverlapsKeepsNonOverlappingSpansSeparate(t *testing.T) {
	spans := []ruleSpan{
		{start: 0, end: 5, id: "a"},
		{start: 10, end: 15, id: "b"},
	}
	got := resolveOverlaps(spans)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (no overlap, both kept)", len(got))
	}
}

func TestSmokeDenylistedToolResultFullyReplaced(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	toolUse := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/Users/ali/project/.env"}}]}}`
	toolResult := `{"type":"user","message":{"content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"SECRET_KEY=abc123"}]}}`

	out1, n1, ok1 := r.RedactLine([]byte(toolUse))
	if !ok1 {
		t.Fatal("tool_use line: ok = false")
	}
	if n1 != 0 {
		t.Errorf("tool_use line redactions = %d, want 0 (nothing in it should match a secret rule)", n1)
	}
	if string(out1) != toolUse {
		t.Errorf("tool_use line changed unexpectedly:\ngot:  %s\nwant: %s", out1, toolUse)
	}

	out2, n2, ok2 := r.RedactLine([]byte(toolResult))
	if !ok2 {
		t.Fatal("tool_result line: ok = false")
	}
	if n2 != 1 {
		t.Errorf("tool_result redactions = %d, want 1", n2)
	}
	if strings.Contains(string(out2), "SECRET_KEY") || strings.Contains(string(out2), "abc123") {
		t.Errorf("denylisted content leaked: %s", out2)
	}
	if !strings.Contains(string(out2), "[REDACTED:denylisted-file .env]") {
		t.Errorf("missing denylist placeholder: %s", out2)
	}
	var v map[string]any
	if err := json.Unmarshal(out2, &v); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, out2)
	}
}

func TestSmokeNonDenylistedReadIsUnaffected(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	toolUse := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Read","input":{"file_path":"/Users/ali/project/main.go"}}]}}`
	toolResult := `{"type":"user","message":{"content":[{"tool_use_id":"toolu_2","type":"tool_result","content":"package main\n"}]}}`

	if _, _, ok := r.RedactLine([]byte(toolUse)); !ok {
		t.Fatal("tool_use: ok = false")
	}
	out, n, ok := r.RedactLine([]byte(toolResult))
	if !ok {
		t.Fatal("tool_result: ok = false")
	}
	if n != 0 {
		t.Errorf("redactions = %d, want 0", n)
	}
	if string(out) != toolResult {
		t.Errorf("unaffected tool_result changed:\ngot:  %s\nwant: %s", out, toolResult)
	}
}

func TestSmokeInvalidJSONFallsBackToRawScan(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	in := `not valid json but contains sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789 anyway`
	out, n, ok := r.RedactLine([]byte(in))
	if ok {
		t.Fatal("ok = true, want false for invalid JSON")
	}
	if n != 1 {
		t.Errorf("redactions = %d, want 1", n)
	}
	if strings.Contains(string(out), "sk-ant-api03") {
		t.Errorf("raw fallback failed to redact: %s", out)
	}
}

// TestSmokeRejectsTrailingBytesAfterACompleteJSONValue covers a stray
// closing delimiter — but not via redactJSON's own len(stack)==0 guard
// inside the closing-delimiter case: verified empirically, a lone
// closing delimiter with no matching open is always rejected by
// json.Decoder.Token() itself (as a syntax error) before it would ever
// be returned as a json.Delim token, so that guard is unreachable
// defensive code, not a path this input can actually exercise. This
// still belongs here as a real-input regression test for "trailing
// garbage after a complete value is rejected, not silently accepted."
func TestSmokeRejectsTrailingBytesAfterACompleteJSONValue(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	in := `{"a":1}}`
	_, _, ok := r.RedactLine([]byte(in))
	if ok {
		t.Error("ok = true, want false for trailing bytes after a complete JSON value")
	}
}

func TestSmokeRejectsExcessiveNestingDepth(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	var b strings.Builder
	for i := 0; i < maxJSONDepth+10; i++ {
		b.WriteString(`{"a":`)
	}
	b.WriteString(`"sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"`)
	for i := 0; i < maxJSONDepth+10; i++ {
		b.WriteString(`}`)
	}
	in := b.String()
	out, _, ok := r.RedactLine([]byte(in))
	if ok {
		t.Error("ok = true, want false for excessive nesting")
	}
	// Falls back to the raw scan — I5 still holds even when the
	// JSON-aware walk itself refuses to go this deep.
	if strings.Contains(string(out), "sk-ant-api03") {
		t.Errorf("raw fallback failed to redact a too-deeply-nested line: %s", out)
	}
}

func TestSmokeAtMaxDepthStillWorks(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	var open, closeB strings.Builder
	for i := 0; i < maxJSONDepth-1; i++ {
		open.WriteString(`{"a":`)
		closeB.WriteString(`}`)
	}
	in := open.String() + `"sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"` + closeB.String()
	out, n, ok := r.RedactLine([]byte(in))
	if !ok {
		t.Fatal("ok = false for a line within the depth limit")
	}
	if n != 1 {
		t.Errorf("redactions = %d, want 1", n)
	}
	if strings.Contains(string(out), "sk-ant-api03") {
		t.Errorf("secret survived at max depth: %s", out)
	}
}

func TestSmokeDenylistedToolResultWithArrayShapedContent(t *testing.T) {
	// The real Anthropic API shape sometimes has tool_result.content as
	// an array of content blocks (e.g. text + image), not a bare
	// string — this must be fully replaced too, brackets included.
	r := NewRedactor(DefaultConfig())
	toolUse := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_4","name":"Read","input":{"file_path":"/Users/ali/project/credentials.json"}}]}}`
	toolResult := `{"type":"user","message":{"content":[{"tool_use_id":"toolu_4","type":"tool_result","content":[{"type":"text","text":"API_KEY=super-secret"}]}]}}`

	if _, _, ok := r.RedactLine([]byte(toolUse)); !ok {
		t.Fatal("tool_use: ok = false")
	}
	out, n, ok := r.RedactLine([]byte(toolResult))
	if !ok {
		t.Fatal("tool_result: ok = false")
	}
	if n != 1 {
		t.Errorf("redactions = %d, want 1", n)
	}
	if strings.Contains(string(out), "super-secret") {
		t.Errorf("array-shaped denylisted content leaked: %s", out)
	}
	if !strings.Contains(string(out), `"[REDACTED:denylisted-file credentials.json]"`) {
		t.Errorf("missing denylist placeholder: %s", out)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, out)
	}
}

func TestSmokeRedactBytesHandlesMultipleCompleteLines(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	in := `{"type":"user","message":{"content":"sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}}` + "\n" +
		`{"type":"assistant","message":{"content":"nothing sensitive here"}}` + "\n"

	out, n := r.RedactBytes([]byte(in))
	if n != 1 {
		t.Errorf("redactions = %d, want 1", n)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), out)
	}
	if strings.Contains(lines[0], "sk-ant-api03") {
		t.Errorf("line 1 still contains the secret: %s", lines[0])
	}
	if !strings.Contains(lines[1], "nothing sensitive here") {
		t.Errorf("line 2 changed unexpectedly: %s", lines[1])
	}
	for _, l := range lines {
		var v map[string]any
		if err := json.Unmarshal([]byte(l), &v); err != nil {
			t.Errorf("line is not valid JSON: %v: %s", err, l)
		}
	}
}

func TestSmokeRedactBytesPreservesATrailingPartialLine(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	in := `{"type":"user","message":{"content":"plain"}}` + "\n" + `{"partial": tr`

	out, _ := r.RedactBytes([]byte(in))
	if string(out) != in {
		t.Errorf("RedactBytes() = %q, want unchanged %q (trailing partial line)", out, in)
	}
}

func TestSmokeCrossBatchPendingSurvivesSeparateRedactorInstances(t *testing.T) {
	r1 := NewRedactor(DefaultConfig())
	toolUse := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_3","name":"Read","input":{"file_path":"/Users/ali/project/id_rsa"}}]}}`
	if _, _, ok := r1.RedactLine([]byte(toolUse)); !ok {
		t.Fatal("tool_use: ok = false")
	}
	pending := r1.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want exactly 1 entry", pending)
	}

	// Simulate a restart: a fresh Redactor, state restored from disk.
	r2 := NewRedactor(DefaultConfig())
	r2.SetPending(pending)

	toolResult := `{"type":"user","message":{"content":[{"tool_use_id":"toolu_3","type":"tool_result","content":"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"}]}}`
	out, n, ok := r2.RedactLine([]byte(toolResult))
	if !ok {
		t.Fatal("tool_result: ok = false")
	}
	if n != 1 {
		t.Errorf("redactions = %d, want 1", n)
	}
	if strings.Contains(string(out), "BEGIN OPENSSH") {
		t.Errorf("denylisted content leaked across the simulated restart: %s", out)
	}
	if len(r2.Pending()) != 0 {
		t.Errorf("pending not cleared after resolving: %+v", r2.Pending())
	}
}
