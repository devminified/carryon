package project

import "testing"

func TestNormalizeRemote(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"github SCP shorthand", "git@github.com:devminified/acme-api.git", "github.com/devminified/acme-api"},
		{"github SCP shorthand no .git", "git@github.com:devminified/acme-api", "github.com/devminified/acme-api"},
		{"github https", "https://github.com/devminified/acme-api", "github.com/devminified/acme-api"},
		{"github https with .git", "https://github.com/devminified/acme-api.git", "github.com/devminified/acme-api"},
		{"github https trailing slash", "https://github.com/devminified/acme-api/", "github.com/devminified/acme-api"},
		{"github ssh:// full form", "ssh://git@github.com/devminified/acme-api.git", "github.com/devminified/acme-api"},
		{"github ssh:// with explicit port 22", "ssh://git@github.com:22/devminified/acme-api.git", "github.com/devminified/acme-api"},
		{"github mixed case", "GitHub.COM:DevMinified/Acme-API.git", "github.com/devminified/acme-api"},
		{"gitlab SCP shorthand", "git@gitlab.com:devminified/acme-api.git", "gitlab.com/devminified/acme-api"},
		{"gitlab https", "https://gitlab.com/devminified/acme-api", "gitlab.com/devminified/acme-api"},
		{"gitlab subgroup path", "https://gitlab.com/devminified/platform/acme-api.git", "gitlab.com/devminified/platform/acme-api"},
		{"bitbucket SCP shorthand", "git@bitbucket.org:devminified/acme-api.git", "bitbucket.org/devminified/acme-api"},
		{"bitbucket https", "https://bitbucket.org/devminified/acme-api.git", "bitbucket.org/devminified/acme-api"},
		{"self-hosted ssh:// with custom port", "ssh://git@git.devminified.com:2222/devminified/acme-api.git", "git.devminified.com/devminified/acme-api"},
		{"self-hosted https with custom port", "https://git.devminified.com:8443/devminified/acme-api.git", "git.devminified.com/devminified/acme-api"},
		{"self-hosted http (no tls)", "http://git.devminified.com/devminified/acme-api.git", "git.devminified.com/devminified/acme-api"},
		{"git:// protocol", "git://github.com/devminified/acme-api.git", "github.com/devminified/acme-api"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeRemote(tc.input)
			if err != nil {
				t.Fatalf("NormalizeRemote(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeRemote(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeRemoteSameRepoDifferentSpellings(t *testing.T) {
	spellings := []string{
		"git@github.com:devminified/acme-api.git",
		"https://github.com/devminified/acme-api",
		"https://github.com/devminified/acme-api.git",
		"ssh://git@github.com/devminified/acme-api.git",
		"ssh://git@github.com:22/devminified/acme-api.git",
		"GitHub.com:DevMinified/Acme-Api.git",
	}

	var want string
	for i, s := range spellings {
		got, err := NormalizeRemote(s)
		if err != nil {
			t.Fatalf("NormalizeRemote(%q) error = %v", s, err)
		}
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want the same canonical form as %q (%q)", s, got, spellings[0], want)
		}
	}
}

func TestNormalizeRemoteForkUnderDifferentOwnerDoesNotMatch(t *testing.T) {
	original, err := NormalizeRemote("git@github.com:devminified/acme-api.git")
	if err != nil {
		t.Fatal(err)
	}
	fork, err := NormalizeRemote("git@github.com:someone-else/acme-api.git")
	if err != nil {
		t.Fatal(err)
	}
	if original == fork {
		t.Errorf("a fork under a different owner must not normalize to the same project: %q == %q", original, fork)
	}
}

func TestNormalizeRemoteRejectsInvalidInput(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"not-a-remote-at-all",
		"https://github.com",     // no path
		"git@github.com:",        // empty path
		"git@github.com:.git",    // path is just .git
	}
	for _, in := range cases {
		if _, err := NormalizeRemote(in); err == nil {
			t.Errorf("NormalizeRemote(%q) = nil error, want an error", in)
		}
	}
}
