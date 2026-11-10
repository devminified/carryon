package main

// CO-211's real end-to-end test: one developer ("ali") uploads a real
// session through the real upload pipeline (CO-110/CO-111) against a
// real local Supabase instance; a second developer ("sara"), who only
// has resumer access, pulls it through the real cobra pull command.
// Verified for real: the manifest fetch, worktree restore from a real
// git snapshot ref, chunk rebuild, path rewrite, session-ID rewrite and
// fork registration, placement at Claude Code's real project-folder
// path, and the exact "claude --resume --fork-session" invocation.
//
// Two real identities and two real local git clones stand in for "two
// different machines" — nothing in the actual system (everything flows
// through Storage/Postgres, never a shared filesystem) requires two
// literal OS-level CI runners for this to be a genuine test of the
// cross-user flow, so this runs as one test in one process rather than
// a two-runner GitHub Actions matrix.
//
// Gated like every other real-local-Supabase test in this codebase
// (internal/auth's TestLocalSupabaseAuthTokenEndpointShape): it tries
// the real stack and skips if it's unreachable, rather than requiring
// `supabase status` to be checked out of band. Run it for real with
// `supabase start` already up.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/claudepath"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/project"
	"github.com/devminified/carryon/internal/pull"
	"github.com/devminified/carryon/internal/upload"
)

const (
	e2eAliEmail   = "ali@devminified.com"
	e2eSaraEmail  = "sara@devminified.com"
	e2ePassword   = "password123"
	e2eSaraUserID = "22222222-2222-2222-2222-222222222222"
)

// e2eAPIURL/e2eAnonKey/e2eDBURL default to the local Supabase CLI
// dev stack's own fixed values (`supabase start`'s well-known demo
// keys/ports), but are overridable via env — CO-506's self-hosted
// stack's own CI job points these at a docker-compose-run stack
// instead, running this exact same test unmodified against it (the
// self-host compose file's own ports/JWT_SECRET are set to match
// these same defaults for that job specifically — see
// .github/workflows/selfhost-e2e.yml).
var (
	e2eAPIURL  = envOr("E2E_API_URL", "http://127.0.0.1:54421")
	e2eAnonKey = envOr("E2E_ANON_KEY", "sb_publishable_ACJWlzQHlZjBrEguHvfOxg_3BJgxAaH")
	e2eDBURL   = envOr("E2E_DB_URL", "postgresql://postgres:postgres@127.0.0.1:54422/postgres")
)

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// passwordGrantToken signs in as email/password directly against
// Supabase Auth's token endpoint — the same real endpoint
// internal/auth's PKCE flow ultimately hits, just via the grant type a
// test can drive without a browser.
func passwordGrantToken(email, password string) (accessToken string, err error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, err := http.NewRequest(http.MethodPost, e2eAPIURL+"/auth/v1/token?grant_type=password", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("apikey", e2eAnonKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Msg         string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 || out.AccessToken == "" {
		return "", fmt.Errorf("password grant for %s: HTTP %d %s %s", email, resp.StatusCode, out.Error, out.Msg)
	}
	return out.AccessToken, nil
}

func e2ePsql(t *testing.T, sql string) {
	t.Helper()
	out, err := exec.Command("psql", e2eDBURL, "-v", "ON_ERROR_STOP=1", "-c", sql).CombinedOutput() // #nosec G204 -- fixed local dev DB URL, test-only
	if err != nil {
		t.Fatalf("psql %q: %v\n%s", sql, err, out)
	}
}

// e2eQueryRow runs a single-row, pipe-delimited SELECT and scans each
// column into dest in order.
func e2eQueryRow(t *testing.T, sql string, dest ...*string) {
	t.Helper()
	out, err := exec.Command("psql", e2eDBURL, "-t", "-A", "-F", "|", "-v", "ON_ERROR_STOP=1", "-c", sql).Output() // #nosec G204 -- fixed local dev DB URL, test-only
	if err != nil {
		t.Fatalf("psql query %q: %v", sql, err)
	}
	line := strings.TrimSpace(string(out))
	fields := strings.Split(line, "|")
	if len(fields) != len(dest) {
		t.Fatalf("psql query %q returned %d field(s) (%q), want %d", sql, len(fields), line, len(dest))
	}
	for i, f := range fields {
		*dest[i] = f
	}
}

func e2eRunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// buildFakeClaudeForE2E compiles a tiny standalone binary standing in
// for the real claude CLI (same technique as internal/pull's own
// launch_test.go, duplicated here rather than exported across packages
// for one test — not worth a shared package for this alone, per this
// project's "wait for the third occurrence" rule): `--version` prints a
// fixed, clearly-synthetic version; any other invocation records its
// argv and cwd.
func buildFakeClaudeForE2E(t *testing.T, recordPath string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	src := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

const recordPath = %q

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Println("9.9.9 (Claude Code)")
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
}
`, recordPath)
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("writing fake claude source: %v", err)
	}

	exePath := filepath.Join(dir, "fakeclaude")
	if runtime.GOOS == "windows" {
		exePath += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", exePath, srcPath).CombinedOutput() // #nosec G204 -- fixed go-toolchain invocation, test-only
	if err != nil {
		t.Fatalf("building fake claude binary: %v\n%s", err, out)
	}
	return exePath
}

func TestPullEndToEndAgainstLocalSupabase(t *testing.T) {
	aliToken, err := passwordGrantToken(e2eAliEmail, e2ePassword)
	if err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}
	saraToken, err := passwordGrantToken(e2eSaraEmail, e2ePassword)
	if err != nil {
		t.Fatalf("password grant for sara failed (ali's succeeded, so the stack is up): %v", err)
	}

	// Auth and Edge Functions are separate local containers — a
	// password grant succeeding only proves Auth is up. Without this
	// second check, `supabase start` alone (no `supabase functions
	// serve`) hangs on the first Edge Function call until api.Client's
	// own 30s HTTP timeout, instead of skipping promptly like every
	// other real-Supabase test in this codebase.
	if _, err := (&http.Client{Timeout: 3 * time.Second}).Get(e2eAPIURL + "/functions/v1/"); err != nil {
		t.Skipf("local Supabase Edge Functions not served (run `supabase functions serve` too): %v", err)
	}

	ctx := context.Background()
	client := api.NewClient(e2eAPIURL, e2eAnonKey)

	// --- real git fixtures: a bare "remote", ali's clone with a commit ---
	root := t.TempDir()
	bareRemote := filepath.Join(root, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bareRemote).CombinedOutput(); err != nil { // #nosec G204 -- fixed args, test-only
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	aliRepo := filepath.Join(root, "ali-repo")
	remoteURL := "file://localhost" + bareRemote
	e2eRunGit(t, root, "clone", "-q", remoteURL, aliRepo)
	e2eRunGit(t, aliRepo, "config", "user.email", "ali@devminified.com")
	e2eRunGit(t, aliRepo, "config", "user.name", "Ali")
	if err := os.WriteFile(filepath.Join(aliRepo, "app.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("writing app.go: %v", err)
	}
	e2eRunGit(t, aliRepo, "add", "-A")
	e2eRunGit(t, aliRepo, "commit", "-q", "-m", "initial")
	headSHA := e2eRunGit(t, aliRepo, "rev-parse", "HEAD")
	treeSHA := e2eRunGit(t, aliRepo, "rev-parse", "HEAD^{tree}")
	e2eRunGit(t, aliRepo, "push", "-q", "origin", "HEAD:refs/heads/main")
	e2eRunGit(t, bareRemote, "symbolic-ref", "HEAD", "refs/heads/main")

	canonicalRemote, err := project.NormalizeRemote(remoteURL)
	if err != nil {
		t.Fatalf("NormalizeRemote(%q): %v", remoteURL, err)
	}

	// --- register a fresh project (ali becomes admin automatically) ---
	proj, err := client.RegisterProject(ctx, aliToken, "e2e-pull-test", canonicalRemote)
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}
	t.Cleanup(func() {
		e2ePsql(t, fmt.Sprintf("delete from sessions where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from project_members where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from projects where id = '%s';", proj.ID))
	})

	// sara needs resumer+ to pull — granted directly in the DB, the same
	// "query as the role in question" testing discipline as the rest of
	// this codebase's RLS/role tests.
	e2ePsql(t, fmt.Sprintf(
		"insert into project_members (project_id, user_id, role) values ('%s', '%s', 'resumer');",
		proj.ID, e2eSaraUserID))

	// --- ali uploads a real session through the real upload pipeline ---
	origSessionID, err := pull.NewSessionID()
	if err != nil {
		t.Fatalf("generating session id: %v", err)
	}
	transcriptPath := filepath.Join(root, origSessionID+".jsonl")
	fixtureLine := fmt.Sprintf(`{"sessionId":%q,"cwd":%q,"type":"user","message":{"text":"edited %s/app.go"}}`+"\n",
		origSessionID, aliRepo, aliRepo)
	if err := os.WriteFile(transcriptPath, []byte(fixtureLine), 0o600); err != nil {
		t.Fatalf("writing transcript fixture: %v", err)
	}

	uploader, err := upload.New(upload.Config{
		ClientSource: func() (*api.Client, error) { return client, nil },
		TokenSource:  func(context.Context) (string, error) { return aliToken, nil },
		StateDir:     filepath.Join(root, "upload-state"),
	})
	if err != nil {
		t.Fatalf("upload.New() error = %v", err)
	}
	if err := uploader.Upload(ctx, agent.Session{
		ID:             origSessionID,
		Cwd:            aliRepo,
		TranscriptPath: transcriptPath,
		ProjectID:      proj.ID,
		Final:          true,
	}); err != nil {
		t.Fatalf("real Upload() error = %v", err)
	}

	if err := client.RegisterSnapshot(ctx, aliToken, api.RegisterSnapshotInput{
		SessionID: origSessionID,
		Seq:       0,
		HeadSHA:   headSHA,
		WipRef:    "refs/heads/main",
		TreeSHA:   treeSHA,
	}); err != nil {
		t.Fatalf("RegisterSnapshot() error = %v", err)
	}

	// --- sara pulls, sandboxed under a fresh HOME ---
	saraHome := t.TempDir()
	t.Setenv("HOME", saraHome)
	t.Setenv("USERPROFILE", saraHome)

	saraClone := filepath.Join(root, "sara-clone")
	e2eRunGit(t, root, "clone", "-q", remoteURL, saraClone)

	record := filepath.Join(root, "fakeclaude-record.txt")
	fakeClaude := buildFakeClaudeForE2E(t, record)
	t.Setenv(claudeBinEnv, fakeClaude)

	cfg := &config.Config{
		APIURL:  e2eAPIURL,
		AnonKey: e2eAnonKey,
		Repos:   map[string]string{canonicalRemote: saraClone},
	}

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetIn(strings.NewReader("y\n")) // pre-answer any version-mismatch confirm prompt

	if err := pullWithClient(cmd, client, saraToken, cfg, origSessionID, pullFlags{yes: true}); err != nil {
		t.Fatalf("pullWithClient() error = %v\noutput:\n%s", err, stdout.String())
	}
	out := stdout.String()

	for _, want := range []string{"→ manifest:", "→ code:", "→ chunks:", "→ rewrite:", "→ place:", "→ launch:"} {
		if !strings.Contains(out, want) {
			t.Errorf("pull output missing step %q, got:\n%s", want, out)
		}
	}

	// --- the fake claude really got invoked with --resume/--fork-session ---
	recordBytes, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("reading fake claude record (claude was never invoked?): %v\noutput:\n%s", err, out)
	}
	recordStr := string(recordBytes)
	if !strings.Contains(recordStr, "--resume") || !strings.Contains(recordStr, "--fork-session") {
		t.Errorf("fake claude record = %q, want --resume/--fork-session", recordStr)
	}

	// The literal path string pull.Place/claudepath.Encode actually used
	// — parsed out of the "→ code: worktree ready at ..." step line,
	// not re-derived via filepath.EvalSymlinks on the fake claude's own
	// recorded cwd: on macOS, os.Getwd() inside that child process
	// returns the fully /private/var-resolved form, which encodes to a
	// DIFFERENT folder name than the unresolved /var/folders/... string
	// production code actually encoded — the exact same real symlink-
	// alias pitfall CO-206/CO-207 already had to guard against
	// elsewhere, caught here by comparing against the literal string
	// rather than assuming a resolved path has to match.
	const marker = "worktree ready at "
	idx := strings.Index(out, marker)
	if idx == -1 {
		t.Fatalf("pull output has no %q line:\n%s", marker, out)
	}
	rest := out[idx+len(marker):]
	worktreePath := rest[:strings.IndexAny(rest, "\n(")]

	// --- the placed file is at Claude Code's real path, forked correctly ---
	projectDir, err := claudepath.ProjectDir(worktreePath)
	if err != nil {
		t.Fatalf("claudepath.ProjectDir() error = %v", err)
	}
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		t.Fatalf("reading placed project dir %s: %v", projectDir, err)
	}
	var placedPath, newID string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			placedPath = filepath.Join(projectDir, e.Name())
			newID = strings.TrimSuffix(e.Name(), ".jsonl")
		}
	}
	if placedPath == "" {
		t.Fatalf("no placed .jsonl file found under %s", projectDir)
	}

	placedContent, err := os.ReadFile(placedPath)
	if err != nil {
		t.Fatalf("reading placed file: %v", err)
	}
	placed := string(placedContent)
	if strings.Contains(placed, origSessionID) {
		t.Errorf("placed file still contains the OLD session id %q:\n%s", origSessionID, placed)
	}
	if !strings.Contains(placed, newID) {
		t.Errorf("placed file doesn't contain the NEW session id %q:\n%s", newID, placed)
	}
	if strings.Contains(placed, aliRepo) {
		t.Errorf("placed file still contains ali's OLD path %q (path rewrite didn't run):\n%s", aliRepo, placed)
	}
	if !strings.Contains(placed, worktreePath) {
		t.Errorf("placed file doesn't contain sara's NEW worktree path %q:\n%s", worktreePath, placed)
	}
	if !strings.Contains(placed, "app.go") {
		t.Errorf("placed file lost the referenced file name entirely:\n%s", placed)
	}

	// --- register-fork really created the row, correctly parented ---
	var parentID, newOwner string
	e2eQueryRow(t, fmt.Sprintf(
		"select coalesce(parent_session_id::text, ''), user_id::text from sessions where id = '%s';", newID),
		&parentID, &newOwner)
	if parentID != origSessionID {
		t.Errorf("forked session's parent_session_id = %q, want %q", parentID, origSessionID)
	}
	if newOwner != e2eSaraUserID {
		t.Errorf("forked session's user_id = %q, want sara's %q", newOwner, e2eSaraUserID)
	}

	// --- export rebuilds the exact same bytes the server has ---
	exportOut := filepath.Join(root, "exported.jsonl")
	var exportStdout bytes.Buffer
	exportCmd := &cobra.Command{}
	exportCmd.SetContext(ctx)
	exportCmd.SetOut(&exportStdout)
	exportCmd.SetErr(&exportStdout)

	if err := exportWithClient(exportCmd, client, saraToken, origSessionID, exportOut); err != nil {
		t.Fatalf("exportWithClient() error = %v", err)
	}
	gotExport, err := os.ReadFile(exportOut)
	if err != nil {
		t.Fatalf("reading exported file: %v", err)
	}
	if string(gotExport) != fixtureLine {
		t.Errorf("export output = %q, want byte-identical to the uploaded fixture %q", gotExport, fixtureLine)
	}
}
