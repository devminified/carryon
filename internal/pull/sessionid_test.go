package pull

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewSessionIDLooksLikeAUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID() error = %v", err)
		}
		if !uuidV4Re.MatchString(id) {
			t.Fatalf("NewSessionID() = %q, doesn't look like a UUIDv4", id)
		}
		if seen[id] {
			t.Fatalf("NewSessionID() produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestRewriteSessionIDLineReplacesTheField(t *testing.T) {
	out, ok := RewriteSessionIDLine([]byte(`{"sessionId":"old-id","type":"user"}`), "new-id")
	if !ok {
		t.Fatal("ok = false")
	}
	want := `{"sessionId":"new-id","type":"user"}`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}

func TestRewriteSessionIDLineReplacesEveryOccurrenceAtAnyDepth(t *testing.T) {
	in := `{"type":"user","sessionId":"old-id","nested":{"sessionId":"old-id"},"list":[{"sessionId":"old-id"}]}`
	out, ok := RewriteSessionIDLine([]byte(in), "new-id")
	if !ok {
		t.Fatal("ok = false")
	}
	want := `{"type":"user","sessionId":"new-id","nested":{"sessionId":"new-id"},"list":[{"sessionId":"new-id"}]}`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}

func TestRewriteSessionIDLineLeavesLinesWithoutTheFieldUnchanged(t *testing.T) {
	in := `{"note":"no session id here","n":3.50}`
	out, ok := RewriteSessionIDLine([]byte(in), "new-id")
	if !ok {
		t.Fatal("ok = false")
	}
	if string(out) != in {
		t.Errorf("got %s, want byte-identical %s", out, in)
	}
}

func TestRewriteSessionIDLineDoesNotMatchASimilarlyNamedKey(t *testing.T) {
	in := `{"sessionIdNotQuiteTheRightKey":"old-id","SessionId":"old-id"}`
	out, ok := RewriteSessionIDLine([]byte(in), "new-id")
	if !ok {
		t.Fatal("ok = false")
	}
	if string(out) != in {
		t.Errorf("got %s, want byte-identical %s (only the exact key \"sessionId\" should match)", out, in)
	}
}

func TestRewriteSessionIDLineNeverTouchesAKeyNamedSessionID(t *testing.T) {
	// "sessionId" as a KEY name elsewhere (not in value position) must
	// never be rewritten -- only a "sessionId" key's VALUE is a target.
	in := `{"sessionId":"a key named sessionId, as a value, is just a string"}`
	out, ok := RewriteSessionIDLine([]byte(in), "new-id")
	if !ok {
		t.Fatal("ok = false")
	}
	want := `{"sessionId":"new-id"}`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}

func TestRewriteSessionIDLineInvalidJSONIsKeptUnchanged(t *testing.T) {
	in := `not valid json`
	out, ok := RewriteSessionIDLine([]byte(in), "new-id")
	if ok {
		t.Error("ok = true, want false")
	}
	if string(out) != in {
		t.Errorf("got %s, want unchanged %s", out, in)
	}
}

func TestRewriteSessionIDLineRejectsExcessiveNestingDepth(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxJSONDepth+10; i++ {
		b.WriteString(`{"a":`)
	}
	b.WriteString(`"x"`)
	for i := 0; i < maxJSONDepth+10; i++ {
		b.WriteString(`}`)
	}
	in := b.String()
	out, ok := RewriteSessionIDLine([]byte(in), "new-id")
	if ok {
		t.Error("ok = true, want false for excessive nesting")
	}
	if string(out) != in {
		t.Error("changed a line it rejected for depth")
	}
}

func TestRewriteSessionIDFilePreservesLineCountAndOrder(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	lines := []string{
		`{"sessionId":"old","seq":0}`,
		`not json`,
		`{"note":"no id","seq":2}`,
		`{"sessionId":"old","seq":3}`,
	}
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing src: %v", err)
	}

	dst := filepath.Join(dir, "dst.jsonl")
	result, err := RewriteSessionIDFile(src, dst, "new-id")
	if err != nil {
		t.Fatalf("RewriteSessionIDFile() error = %v", err)
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
	want := []string{
		`{"sessionId":"new-id","seq":0}`,
		`not json`,
		`{"note":"no id","seq":2}`,
		`{"sessionId":"new-id","seq":3}`,
	}
	for i := range want {
		if gotLines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, gotLines[i], want[i])
		}
	}

	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("a .tmp file was left behind: %v", err)
	}
}

func TestRewriteSessionIDFileFailsCleanlyWhenTheSourceDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := RewriteSessionIDFile(filepath.Join(dir, "missing.jsonl"), filepath.Join(dir, "dst.jsonl"), "new-id")
	if err == nil {
		t.Fatal("error = nil, want an error for a missing source file")
	}
}
