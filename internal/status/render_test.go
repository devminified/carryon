package status

import (
	"strings"
	"testing"
	"time"
)

// fixedNow anchors every relative-time calculation in these snapshot
// tests, per Render's own doc comment on why it takes now explicitly.
var fixedNow = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func TestRenderLoggedOut(t *testing.T) {
	got := Render(Report{
		LoggedIn:   false,
		BackendURL: "http://127.0.0.1:54421",
		Agent:      AgentInfo{Running: false},
	}, fixedNow)

	want := "Not logged in — run `carryon login`.\n" +
		"Backend: http://127.0.0.1:54421\n" +
		"Agent: not running — run `carryon install`, or start it with `carryon agent`.\n"
	if got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderAgentDown(t *testing.T) {
	got := Render(Report{
		LoggedIn:   true,
		User:       &UserInfo{Email: "ali@devminified.com", UserName: "ali"},
		BackendURL: "http://127.0.0.1:54421",
		Agent:      AgentInfo{Running: false},
	}, fixedNow)

	want := "Logged in as ali@devminified.com (ali)\n" +
		"Backend: http://127.0.0.1:54421\n" +
		"Agent: not running — run `carryon install`, or start it with `carryon agent`.\n"
	if got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderHealthy(t *testing.T) {
	lastUpload := fixedNow.Add(-12 * time.Second)
	got := Render(Report{
		LoggedIn:   true,
		User:       &UserInfo{Email: "ali@devminified.com", UserName: "ali"},
		BackendURL: "http://127.0.0.1:54421",
		Agent:      AgentInfo{Running: true, Version: "1.2.3", UptimeSeconds: 8040}, // 2h14m
		Sessions: []SessionInfo{
			{
				SessionID:    "a1b2c3d4-1111-4111-8111-aaaaaaaaaaaa",
				Project:      "github.com/devminified/acme-api",
				LastUploadAt: &lastUpload,
				BytesBehind:  0,
			},
		},
	}, fixedNow)

	if !strings.Contains(got, "Agent: running (v1.2.3, up 2h14m)") {
		t.Errorf("Render() missing the expected agent line:\n%s", got)
	}
	if !strings.Contains(got, "a1b2c3d4") {
		t.Errorf("Render() missing the session row:\n%s", got)
	}
	if !strings.Contains(got, "12s ago") {
		t.Errorf("Render() missing the relative upload time:\n%s", got)
	}
	if !strings.Contains(got, "0 B") {
		t.Errorf("Render() missing the bytes-behind column:\n%s", got)
	}
	if strings.Contains(got, "blocked") {
		t.Errorf("Render() mentions \"blocked\" for a healthy session:\n%s", got)
	}
}

func TestRenderLagging(t *testing.T) {
	lastUpload := fixedNow.Add(-45 * time.Minute)
	got := Render(Report{
		LoggedIn:   true,
		User:       &UserInfo{Email: "ali@devminified.com"},
		BackendURL: "http://127.0.0.1:54421",
		Agent:      AgentInfo{Running: true, Version: "1.2.3", UptimeSeconds: 120},
		Sessions: []SessionInfo{
			{
				SessionID:    "d4e5f6a7-1111-4111-8111-bbbbbbbbbbbb",
				Project:      "github.com/devminified/acme-api",
				LastUploadAt: &lastUpload,
				BytesBehind:  1536,
			},
		},
	}, fixedNow)

	if !strings.Contains(got, "45m ago") {
		t.Errorf("Render() missing the lagging relative time:\n%s", got)
	}
	if !strings.Contains(got, "1.5 KB") {
		t.Errorf("Render() missing the bytes-behind column:\n%s", got)
	}
}

func TestRenderShowsExcludedLargeFilesWarning(t *testing.T) {
	lastUpload := fixedNow.Add(-12 * time.Second)
	got := Render(Report{
		LoggedIn:   true,
		User:       &UserInfo{Email: "ali@devminified.com"},
		BackendURL: "http://127.0.0.1:54421",
		Agent:      AgentInfo{Running: true, Version: "1.2.3", UptimeSeconds: 60},
		Sessions: []SessionInfo{
			{
				SessionID:          "a1b2c3d4-1111-4111-8111-aaaaaaaaaaaa",
				Project:            "github.com/devminified/acme-api",
				LastUploadAt:       &lastUpload,
				BytesBehind:        0,
				ExcludedLargeFiles: []string{"big.bin", "huge.zip"},
			},
		},
	}, fixedNow)

	if !strings.Contains(got, "2 large file(s) excluded from snapshot") {
		t.Errorf("Render() missing the excluded-large-files warning:\n%s", got)
	}
}

func TestRenderBlocked(t *testing.T) {
	got := Render(Report{
		LoggedIn:   true,
		User:       &UserInfo{Email: "ali@devminified.com"},
		BackendURL: "http://127.0.0.1:54421",
		Agent:      AgentInfo{Running: true, Version: "1.2.3", UptimeSeconds: 60},
		Sessions: []SessionInfo{
			{
				SessionID:     "z9y8x7w6-1111-4111-8111-cccccccccccc",
				Project:       "github.com/devminified/blocked-one",
				BytesBehind:   340,
				Blocked:       true,
				BlockedReason: "forbidden: fatal upload error — won't be retried automatically",
			},
		},
		SkippedRepos: []string{"github.com/someone/unregistered-repo"},
	}, fixedNow)

	if !strings.Contains(got, "blocked: forbidden") {
		t.Errorf("Render() missing the blocked reason:\n%s", got)
	}
	if !strings.Contains(got, "-  ") { // "LAST UPLOAD" column shows "-" when it never succeeded
		t.Errorf("Render() missing the dash for a never-uploaded session:\n%s", got)
	}
	if !strings.Contains(got, "Skipped repos (not registered with Carryon):") {
		t.Errorf("Render() missing the skipped-repos section:\n%s", got)
	}
	if !strings.Contains(got, "github.com/someone/unregistered-repo") {
		t.Errorf("Render() missing the skipped repo itself:\n%s", got)
	}
}
