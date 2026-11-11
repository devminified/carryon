package main

import "testing"

func TestIsLocalAPIURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://127.0.0.1:54421", true},
		{"http://localhost:54421", true},
		{"http://host.docker.internal:54421", true},
		{"https://my-machine.local:3000", true},
		{"https://abcd1234.supabase.co", false},
		{"https://carryon-staging.example.com", false},
		{"not a url at all", false},
	}
	for _, c := range cases {
		if got := isLocalAPIURL(c.url); got != c.want {
			t.Errorf("isLocalAPIURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestRunRefusesNonLocalURLWithoutConfirmation(t *testing.T) {
	err := run([]string{"-api-url", "https://carryon-staging.example.com", "-project-id", "x"})
	if err == nil {
		t.Fatal("expected run() to refuse a non-local -api-url without -confirm-real-cost, got nil error")
	}
}
