package pull

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devminified/carryon/internal/api"
)

func TestRewriteLineReplacesAWholeStringValue(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/Users/bob/carryon/work/acme-api-sess1")
	out, ok := r.RewriteLine([]byte(`{"cwd":"/Users/ali/work/acme-api","note":"hello"}`))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"cwd":"/Users/bob/carryon/work/acme-api-sess1","note":"hello"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineReplacesAPathWithAContinuingSubpath(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/Users/bob/carryon/work/acme-api-sess1")
	out, ok := r.RewriteLine([]byte(`{"text":"see /Users/ali/work/acme-api/src/main.go for details"}`))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"text":"see /Users/bob/carryon/work/acme-api-sess1/src/main.go for details"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineDoesNotRewriteASharedPrefixOnly(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/Users/bob/new")
	in := `{"text":"see /Users/ali/work/acme-api-old/src/main.go for details"}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	if string(out) != in {
		t.Errorf("RewriteLine() = %s, want unchanged %s", out, in)
	}
}

func TestRewriteLineDoesNotRewriteAPathEmbeddedAsASuffix(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/Users/bob/new")
	in := `{"text":"not /opt/backup/Users/ali/work/acme-api at all"}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	if string(out) != in {
		t.Errorf("RewriteLine() = %s, want unchanged %s", out, in)
	}
}

func TestRewriteLineHandlesNestedArraysAndObjects(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/new")
	in := `{"a":1,"b":[1,2,"/Users/ali/work/acme-api"],"c":{"d":"/Users/ali/work/acme-api/x"}}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"a":1,"b":[1,2,"/new"],"c":{"d":"/new/x"}}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineNeverRewritesObjectKeys(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/new")
	// The key name itself is textually identical to the old root -- it
	// must stay untouched; only the VALUE gets rewritten.
	in := `{"/Users/ali/work/acme-api":"/Users/ali/work/acme-api"}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"/Users/ali/work/acme-api":"/new"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineInvalidJSONIsKeptUnchanged(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/new")
	in := `not valid json at all`
	out, ok := r.RewriteLine([]byte(in))
	if ok {
		t.Error("RewriteLine() ok = true, want false for invalid JSON")
	}
	if string(out) != in {
		t.Errorf("RewriteLine() = %s, want unchanged %s", out, in)
	}
}

func TestRewriteLinePreservesNumberFormattingAndKeyOrder(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/new")
	// No match at all here -- confirms a non-matching line passes
	// through byte for byte (no reordering, no number reformatting from
	// a full-tree remarshal, which would turn 3.50 into 3.5 and might
	// reorder these keys alphabetically).
	in := `{"z":3.50,"a":"unrelated","m":[1,2,3]}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	if string(out) != in {
		t.Errorf("RewriteLine() = %s, want byte-identical %s", out, in)
	}
}

func TestRewriteLinePreservesKeyOrderEvenWhenAValueIsRewritten(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/new")
	in := `{"z":1,"cwd":"/Users/ali/work/acme-api","a":2}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"z":1,"cwd":"/new","a":2}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineWindowsRootIsCaseInsensitive(t *testing.T) {
	r := NewPathRewriter(`C:\Users\ali\acme-api`, "/home/bob/acme-api")
	in := `{"windows":"C:\\Users\\ali\\acme-api\\src\\main.go","other":"C:\\Users\\ALI\\ACME-API\\x"}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"windows":"/home/bob/acme-api/src/main.go","other":"/home/bob/acme-api/x"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLinePosixRootIsCaseSensitive(t *testing.T) {
	// A different-case POSIX path is a genuinely different path on a
	// case-sensitive filesystem -- must NOT match.
	r := NewPathRewriter("/Users/ali/work/acme-api", "/new")
	in := `{"text":"/Users/ALI/work/ACME-API"}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	if string(out) != in {
		t.Errorf("RewriteLine() = %s, want unchanged %s", out, in)
	}
}

// --- Cross-platform fixtures: macOS/Linux -> Windows -> macOS ---

func TestRewriteLineMacToLinux(t *testing.T) {
	r := NewPathRewriter("/Users/ali/acme-api", "/home/bob/acme-api")
	out, ok := r.RewriteLine([]byte(`{"cwd":"/Users/ali/acme-api/src/main.go"}`))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"cwd":"/home/bob/acme-api/src/main.go"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineLinuxToWindows(t *testing.T) {
	r := NewPathRewriter("/home/ali/acme-api", `C:\Users\bob\acme-api`)
	out, ok := r.RewriteLine([]byte(`{"cwd":"/home/ali/acme-api/src/main.go"}`))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"cwd":"C:\\Users\\bob\\acme-api\\src\\main.go"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineWindowsToMac(t *testing.T) {
	r := NewPathRewriter(`C:\Users\ali\acme-api`, "/Users/bob/acme-api")
	in := `{"cwd":"C:\\Users\\ali\\acme-api\\src\\main.go"}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"cwd":"/Users/bob/acme-api/src/main.go"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineRewritesAPathInsideAToolResultCodeBlock(t *testing.T) {
	r := NewPathRewriter("/Users/ali/acme-api", "/home/bob/acme-api")
	// A realistic tool_result shape: a multi-line text blob containing
	// a code fence that itself mentions the old path. Built via
	// json.Marshal (not hand-escaped) so the newlines inside the code
	// block are guaranteed validly encoded.
	content := "Here is the file:\n```go\n// Package main lives at /Users/ali/acme-api/cmd/main.go\n```\nDone."
	encodedContent, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("json.Marshal(content): %v", err)
	}
	in := `{"type":"tool_result","content":` + string(encodedContent) + `}`
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output isn't valid JSON: %v", err)
	}
	rewrittenContent, _ := decoded["content"].(string)
	if strings.Contains(rewrittenContent, "/Users/ali/acme-api") {
		t.Errorf("content still contains the old path: %q", rewrittenContent)
	}
	if !strings.Contains(rewrittenContent, "/home/bob/acme-api/cmd/main.go") {
		t.Errorf("content missing the rewritten path: %q", rewrittenContent)
	}
}

func TestRewriteLineWithAliasesAppliesTheLongestMatch(t *testing.T) {
	r := NewPathRewriter("/Users/ali/work/acme-api", "/new-primary")
	r.AddAlias("/Users/ali/work/acme-api/vendor/sub", "/new-alias")
	// The longer, more specific alias should win over the shorter
	// primary root when both would otherwise match at the same start.
	out, ok := r.RewriteLine([]byte(`{"a":"/Users/ali/work/acme-api/vendor/sub/file.go"}`))
	if !ok {
		t.Fatal("RewriteLine() ok = false")
	}
	want := `{"a":"/new-alias/file.go"}`
	if string(out) != want {
		t.Errorf("RewriteLine() = %s, want %s", out, want)
	}
}

func TestRewriteLineRejectsExcessiveNestingDepth(t *testing.T) {
	r := NewPathRewriter("/old", "/new")
	var b strings.Builder
	for i := 0; i < maxJSONDepth+10; i++ {
		b.WriteString(`{"a":`)
	}
	b.WriteString(`"/old"`)
	for i := 0; i < maxJSONDepth+10; i++ {
		b.WriteString(`}`)
	}
	in := b.String()
	out, ok := r.RewriteLine([]byte(in))
	if ok {
		t.Error("RewriteLine() ok = true, want false for excessive nesting")
	}
	if string(out) != in {
		t.Error("RewriteLine() changed a line it rejected for depth")
	}
}

func TestRewriteLineAtMaxDepthStillWorks(t *testing.T) {
	r := NewPathRewriter("/old", "/new")
	var open, closeB strings.Builder
	for i := 0; i < maxJSONDepth-1; i++ {
		open.WriteString(`{"a":`)
		closeB.WriteString(`}`)
	}
	in := open.String() + `"/old"` + closeB.String()
	out, ok := r.RewriteLine([]byte(in))
	if !ok {
		t.Fatal("RewriteLine() ok = false for a line within the depth limit")
	}
	if !strings.Contains(string(out), `"/new"`) {
		t.Errorf("RewriteLine() = %s, want it to contain the rewritten path", out)
	}
}

func TestRewriteFilePreservesLineCountAndOrder(t *testing.T) {
	r := NewPathRewriter("/Users/ali/acme-api", "/new")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	lines := []string{
		`{"seq":0,"cwd":"/Users/ali/acme-api"}`,
		`not json`,
		`{"seq":2,"cwd":"/elsewhere"}`,
		`{"seq":3,"cwd":"/Users/ali/acme-api/sub"}`,
	}
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing src: %v", err)
	}

	dst := filepath.Join(dir, "dst.jsonl")
	result, err := r.RewriteFile(src, dst)
	if err != nil {
		t.Fatalf("RewriteFile() error = %v", err)
	}
	if result.Lines != 4 {
		t.Errorf("Lines = %d, want 4", result.Lines)
	}
	if result.LinesRewritten != 2 {
		t.Errorf("LinesRewritten = %d, want 2", result.LinesRewritten)
	}
	if result.LinesUnparseable != 1 {
		t.Errorf("LinesUnparseable = %d, want 1", result.LinesUnparseable)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	gotLines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(gotLines) != 4 {
		t.Fatalf("got %d lines, want 4: %q", len(gotLines), gotLines)
	}
	if gotLines[1] != "not json" {
		t.Errorf("unparseable line = %q, want it kept verbatim", gotLines[1])
	}
	if gotLines[2] != `{"seq":2,"cwd":"/elsewhere"}` {
		t.Errorf("non-matching line changed: %q", gotLines[2])
	}
	if !strings.Contains(gotLines[0], `"/new"`) {
		t.Errorf("line 0 not rewritten: %q", gotLines[0])
	}
	if !strings.Contains(gotLines[3], `"/new/sub"`) {
		t.Errorf("line 3 not rewritten: %q", gotLines[3])
	}

	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("a .tmp file was left behind: %v", err)
	}
}

func TestRewriteFilePreservesFileWithNoTrailingNewline(t *testing.T) {
	r := NewPathRewriter("/old", "/new")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	if err := os.WriteFile(src, []byte(`{"a":"/old"}`), 0o600); err != nil { // no trailing \n
		t.Fatalf("writing src: %v", err)
	}
	dst := filepath.Join(dir, "dst.jsonl")
	if _, err := r.RewriteFile(src, dst); err != nil {
		t.Fatalf("RewriteFile() error = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	if string(got) != `{"a":"/new"}` {
		t.Errorf("got %q, want no trailing newline preserved", got)
	}
}

func TestRewriteFileEmptySourceProducesEmptyOutput(t *testing.T) {
	r := NewPathRewriter("/old", "/new")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	if err := os.WriteFile(src, nil, 0o600); err != nil {
		t.Fatalf("writing src: %v", err)
	}
	dst := filepath.Join(dir, "dst.jsonl")
	result, err := r.RewriteFile(src, dst)
	if err != nil {
		t.Fatalf("RewriteFile() error = %v", err)
	}
	if result.Lines != 0 {
		t.Errorf("Lines = %d, want 0", result.Lines)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRewriteFileFailsCleanlyWhenTheSourceDoesNotExist(t *testing.T) {
	r := NewPathRewriter("/old", "/new")
	dir := t.TempDir()
	_, err := r.RewriteFile(filepath.Join(dir, "does-not-exist.jsonl"), filepath.Join(dir, "dst.jsonl"))
	if err == nil {
		t.Fatal("RewriteFile() error = nil, want an error for a missing source file")
	}
}

func TestRewriteFileWritesIntoANewSubdirectory(t *testing.T) {
	r := NewPathRewriter("/old", "/new")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	if err := os.WriteFile(src, []byte(`{"a":"/old"}`+"\n"), 0o600); err != nil {
		t.Fatalf("writing src: %v", err)
	}
	dst := filepath.Join(dir, "nested", "deeper", "dst.jsonl")
	if _, err := r.RewriteFile(src, dst); err != nil {
		t.Fatalf("RewriteFile() error = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	if string(got) != `{"a":"/new"}`+"\n" {
		t.Errorf("got %q", got)
	}
}

// TestPullAndRewritePipelineComposesCorrectly chains CO-205's Rebuild
// and CO-207's RewriteFile exactly as CO-211's real pull command will:
// rebuild a multi-chunk session from a manifest, then rewrite the
// result in place.
func TestPullAndRewritePipelineComposesCorrectly(t *testing.T) {
	server := newFakeChunkServer(t)
	data := []byte(
		`{"cwd":"/Users/ali/acme-api","event":"start"}` + "\n" +
			`{"text":"edited /Users/ali/acme-api/main.go"}` + "\n" +
			`{"cwd":"/elsewhere","event":"noise"}` + "\n",
	)
	chunks := registerStream(server, mainStreamID, data, 40)
	manifest := &api.Manifest{Chunks: chunks}

	destDir := t.TempDir()
	rebuildResult, err := Rebuild(context.Background(), server.client(), manifest, destDir, DefaultConfig())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	r := NewPathRewriter("/Users/ali/acme-api", "/home/bob/carryon/work/acme-api-sess1")
	rewritten := filepath.Join(destDir, "session.rewritten.jsonl")
	rewriteResult, err := r.RewriteFile(rebuildResult.MainFile, rewritten)
	if err != nil {
		t.Fatalf("RewriteFile() error = %v", err)
	}
	if rewriteResult.Lines != 3 {
		t.Errorf("Lines = %d, want 3", rewriteResult.Lines)
	}
	if rewriteResult.LinesRewritten != 2 {
		t.Errorf("LinesRewritten = %d, want 2", rewriteResult.LinesRewritten)
	}

	got, err := os.ReadFile(rewritten)
	if err != nil {
		t.Fatalf("reading rewritten file: %v", err)
	}
	gotLines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	want := []string{
		`{"cwd":"/home/bob/carryon/work/acme-api-sess1","event":"start"}`,
		`{"text":"edited /home/bob/carryon/work/acme-api-sess1/main.go"}`,
		`{"cwd":"/elsewhere","event":"noise"}`,
	}
	for i, w := range want {
		if gotLines[i] != w {
			t.Errorf("line %d = %q, want %q", i, gotLines[i], w)
		}
	}
}
