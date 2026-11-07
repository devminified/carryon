package claudepath

import (
	"path/filepath"
	"testing"
)

// These fixtures are real: captured by actually running the installed,
// pinned Claude Code (2.1.263) against a throwaway directory and
// reading back the resulting ~/.claude/projects/<folder> name it
// created — not guessed from the ticket's English description. See
// Encode's doc comment for what each one proves.
func TestEncodeRealFixtures(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain path, slashes only",
			in:   "/Users/cto/Documents/GitHub/carryon",
			want: "-Users-cto-Documents-GitHub-carryon",
		},
		{
			name: "dots, underscores, spaces",
			in:   "/private/tmp/claudepath-test/my.repo_name with spaces",
			want: "-private-tmp-claudepath-test-my-repo-name-with-spaces",
		},
		{
			name: "consecutive special characters are never collapsed",
			in:   "/private/tmp/claudepath-test2/a..b__c  d",
			want: "-private-tmp-claudepath-test2-a--b--c--d",
		},
		{
			name: "non-ASCII letters (including multi-byte UTF-8) are replaced too, one dash each",
			in:   "/private/tmp/claudepath-test3/café-日本語-tëst",
			want: "-private-tmp-claudepath-test3-caf------t-st",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Encode(c.in); got != c.want {
				t.Errorf("Encode(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestEncodePreservesLetterCase(t *testing.T) {
	// "GitHub" in the first real fixture above already proves this
	// (capital G and H survive), but spell it out as its own case too.
	if got := Encode("MixedCASE123"); got != "MixedCASE123" {
		t.Errorf("Encode() = %q, want letters/digits preserved exactly", got)
	}
}

func TestEncodeEmptyString(t *testing.T) {
	if got := Encode(""); got != "" {
		t.Errorf("Encode(\"\") = %q, want empty", got)
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	in := "/Users/ali/some path/repo_v2.0"
	first := Encode(in)
	for i := 0; i < 5; i++ {
		if got := Encode(in); got != first {
			t.Errorf("Encode() = %q on call %d, want %q (stable across calls)", got, i, first)
		}
	}
}

func TestProjectDirUsesEncode(t *testing.T) {
	home := withFakeHome(t)
	got, err := ProjectDir("/Users/ali/work/acme-api")
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	want := filepath.Join(home, ".claude", "projects", "-Users-ali-work-acme-api")
	if got != want {
		t.Errorf("ProjectDir() = %q, want %q", got, want)
	}
}

func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}
