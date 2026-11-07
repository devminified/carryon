package pull

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFormatHeaderMatchesTheDocumentedShape(t *testing.T) {
	in := HeaderInfo{
		Owner:             "Ali",
		Branch:            "feature/payments",
		LastActivity:      time.Date(2026, 1, 1, 18, 42, 0, 0, time.Local),
		UncommittedFiles:  3,
		ClaudeCodeVersion: "2.1.263",
	}
	got := FormatHeader(in)
	want := "Ali · feature/payments · last activity 18:42 · 3 uncommitted files · Claude Code 2.1.263"
	if got != want {
		t.Errorf("FormatHeader() = %q, want %q", got, want)
	}
}

func TestFormatHeaderSingularizesOneFile(t *testing.T) {
	in := HeaderInfo{
		Owner:             "Ali",
		Branch:            "main",
		LastActivity:      time.Date(2026, 1, 1, 9, 5, 0, 0, time.Local),
		UncommittedFiles:  1,
		ClaudeCodeVersion: "2.1.263",
	}
	got := FormatHeader(in)
	if !strings.Contains(got, "1 uncommitted file ·") {
		t.Errorf("FormatHeader() = %q, want singular \"1 uncommitted file\"", got)
	}
}

func TestFormatHeaderZeroFilesIsPlural(t *testing.T) {
	in := HeaderInfo{UncommittedFiles: 0, LastActivity: time.Now(), ClaudeCodeVersion: "2.1.263"}
	got := FormatHeader(in)
	if !strings.Contains(got, "0 uncommitted files ·") {
		t.Errorf("FormatHeader() = %q, want plural for zero", got)
	}
}

func TestParseVersionValidAndInvalid(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"2.1.263", Version{2, 1, 263}, true},
		{"0.0.1", Version{0, 0, 1}, true},
		{" 2.1.263 ", Version{2, 1, 263}, true},
		{"2.1", Version{}, false},
		{"2.1.263.4", Version{}, false},
		{"2.1.x", Version{}, false},
		{"", Version{}, false},
		{"not a version", Version{}, false},
		{"-1.0.0", Version{}, false},
	}
	for _, c := range cases {
		got, ok := ParseVersion(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseVersion(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestCompareVersionsAllRelations(t *testing.T) {
	cases := []struct {
		name    string
		session string
		local   string
		want    VersionRelation
	}{
		{"equal", "2.1.263", "2.1.263", VersionEqual},
		{"local newer patch", "2.1.263", "2.1.264", VersionNewer},
		{"local newer minor", "2.1.263", "2.2.0", VersionNewer},
		{"local newer major", "2.1.263", "3.0.0", VersionNewer},
		{"local older patch", "2.1.263", "2.1.260", VersionOlder},
		{"local older minor", "2.1.263", "2.0.9", VersionOlder},
		{"local older major", "2.1.263", "1.9.9", VersionOlder},
		{"session unparseable", "vX", "2.1.263", VersionUnparseable},
		{"local unparseable", "2.1.263", "vX", VersionUnparseable},
		{"both unparseable", "vX", "vY", VersionUnparseable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CompareVersions(c.session, c.local); got != c.want {
				t.Errorf("CompareVersions(%q, %q) = %v, want %v", c.session, c.local, got, c.want)
			}
		})
	}
}

func TestNeedsVersionWarning(t *testing.T) {
	cases := []struct {
		name    string
		session string
		local   string
		want    bool
	}{
		{"identical", "2.1.263", "2.1.263", false},
		{"local newer", "2.1.263", "2.2.0", false},
		{"patch-only older does not warn", "2.1.263", "2.1.1", false},
		{"minor older warns", "2.1.263", "2.0.9", true},
		{"major older warns", "2.1.263", "1.9.9", true},
		{"unparseable session warns", "vX", "2.1.263", true},
		{"unparseable local warns", "2.1.263", "vX", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NeedsVersionWarning(c.session, c.local); got != c.want {
				t.Errorf("NeedsVersionWarning(%q, %q) = %v, want %v", c.session, c.local, got, c.want)
			}
		})
	}
}

// buildFakeClaude compiles a tiny standalone program masquerading as
// the real `claude` CLI and returns its path: `--version` prints a
// real-shaped version line, and otherwise it records its cwd, argv,
// and stdin to recordPath (baked in as a compile-time constant, since
// Launch itself controls every argument the real binary is called
// with) so the test can assert Launch wired everything correctly.
// Built as a real binary via `go build` rather than a shell/batch
// script: a bare .cmd file isn't reliably directly executable as a
// child process on Windows the way a POSIX shell script is, so a
// compiled binary is the one form that behaves identically everywhere.
func buildFakeClaude(t *testing.T, recordPath string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	src := fmt.Sprintf(`package main

import (
	"fmt"
	"io"
	"os"
)

const recordPath = %q

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Println("2.1.263 (Claude Code)")
		return
	}
	f, err := os.Create(recordPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	cwd, _ := os.Getwd()
	fmt.Fprintln(f, cwd)
	fmt.Fprintln(f, os.Args[1:])
	_, _ = io.Copy(f, os.Stdin)
}
`, recordPath)
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("writing fake claude source: %v", err)
	}

	exePath := filepath.Join(dir, "fakeclaude")
	if runtime.GOOS == "windows" {
		exePath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exePath, srcPath) // #nosec G204 -- fixed go-toolchain invocation, test-only
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fake claude binary: %v\n%s", err, out)
	}
	return exePath
}

func TestLocalClaudeVersionParsesTheRealOutputShape(t *testing.T) {
	bin := buildFakeClaude(t, filepath.Join(t.TempDir(), "unused"))
	got, err := LocalClaudeVersion(context.Background(), bin)
	if err != nil {
		t.Fatalf("LocalClaudeVersion() error = %v", err)
	}
	if got != "2.1.263" {
		t.Errorf("LocalClaudeVersion() = %q, want %q", got, "2.1.263")
	}
}

func TestLaunchRunsClaudeWithResumeAndForkSessionInTheWorktree(t *testing.T) {
	worktree := t.TempDir()
	record := filepath.Join(t.TempDir(), "record.txt")
	bin := buildFakeClaude(t, record)

	var stdout bytes.Buffer
	err := Launch(context.Background(), LaunchInput{
		WorktreePath: worktree,
		SessionID:    "new-id-123",
		ClaudeBin:    bin,
		Stdin:        strings.NewReader("hello from stdin\n"),
		Stdout:       &stdout,
		Stderr:       &stdout,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("reading record: %v", err)
	}
	lines := strings.SplitN(string(got), "\n", 2)
	gotCwd, err := filepath.EvalSymlinks(strings.TrimSpace(lines[0]))
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", lines[0], err)
	}
	wantCwd, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", worktree, err)
	}
	if gotCwd != wantCwd {
		t.Errorf("cwd = %q, want %q", gotCwd, wantCwd)
	}
	if !strings.Contains(string(got), "--resume new-id-123 --fork-session") {
		t.Errorf("record = %q, want it to contain the resume/fork-session args", got)
	}
	if !strings.Contains(string(got), "hello from stdin") {
		t.Errorf("record = %q, want the piped stdin to have reached the fake claude", got)
	}
}

func TestLaunchNoLaunchPrintsAPasteableCommandInstead(t *testing.T) {
	worktree := t.TempDir()
	var stdout bytes.Buffer
	err := Launch(context.Background(), LaunchInput{
		WorktreePath: worktree,
		SessionID:    "new-id-123",
		NoLaunch:     true,
		ClaudeBin:    "claude",
		Stdout:       &stdout,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	want := "cd " + shellQuote(worktree) + " && claude --resume " + shellQuote("new-id-123") + " --fork-session"
	if got != want {
		t.Errorf("printed command = %q, want %q", got, want)
	}
}

func TestLaunchNoLaunchQuotesAWorktreePathContainingASpace(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "repo with spaces")
	var stdout bytes.Buffer
	if err := Launch(context.Background(), LaunchInput{
		WorktreePath: worktree,
		SessionID:    "id",
		NoLaunch:     true,
		ClaudeBin:    "claude",
		Stdout:       &stdout,
	}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "'"+worktree+"'") {
		t.Errorf("printed command = %q, want the space-containing path single-quoted", stdout.String())
	}
}

func TestLaunchWithNoClaudeOnPathNamesHowToInstallIt(t *testing.T) {
	err := Launch(context.Background(), LaunchInput{
		WorktreePath: t.TempDir(),
		SessionID:    "id",
		ClaudeBin:    "carryon-definitely-not-a-real-binary-xyz",
	})
	if err == nil {
		t.Fatal("Launch() error = nil, want ErrClaudeNotFound")
	}
	if !errors.Is(err, ErrClaudeNotFound) {
		t.Errorf("Launch() error = %v, want errors.Is ErrClaudeNotFound", err)
	}
	if !strings.Contains(err.Error(), "install") {
		t.Errorf("Launch() error = %q, want it to say how to install claude", err)
	}
}

func TestShellQuoteEscapesEmbeddedSingleQuotes(t *testing.T) {
	got := shellQuote("it's a path")
	want := `'it'"'"'s a path'`
	if got != want {
		t.Errorf("shellQuote() = %q, want %q", got, want)
	}
}
