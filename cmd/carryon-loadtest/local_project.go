package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/loadtest"
	"github.com/devminified/carryon/internal/project"
)

// provisionLocalProject registers a fresh, throwaway project against
// a local dev Supabase stack using creds[0] (which becomes its admin
// via register-project), grants every other credential resumer access
// directly in the DB — the same technique cmd/carryon's own
// e2e_test.go and internal/loadtest's own integration test already
// use for seed.sql's ali/sara — and returns the project ID plus a
// cleanup function that removes every row this created. Only ever
// called when -api-url is local (main.go's own isLocalAPIURL check) —
// this never runs against a real backend.
func provisionLocalProject(ctx context.Context, apiURL, anonKey, dbURL string, creds []loadtest.Credential) (projectID string, cleanup func(), err error) {
	if len(creds) == 0 {
		return "", nil, fmt.Errorf("no credentials to provision a project for")
	}
	httpClient := &http.Client{Timeout: 10 * time.Second}

	adminToken, err := passwordGrantForProvisioning(ctx, apiURL, anonKey, creds[0], httpClient)
	if err != nil {
		return "", nil, fmt.Errorf("signing in as %s: %w", creds[0].Email, err)
	}

	client := api.NewClientWithHTTP(apiURL, anonKey, httpClient)
	remote, err := project.NormalizeRemote(fmt.Sprintf("file://localhost/tmp/carryon-loadtest-%d.git", time.Now().UnixNano()))
	if err != nil {
		return "", nil, fmt.Errorf("normalizing a throwaway remote: %w", err)
	}
	proj, err := client.RegisterProject(ctx, adminToken, "loadtest-dry-run", remote)
	if err != nil {
		return "", nil, fmt.Errorf("registering a throwaway project: %w", err)
	}

	for _, c := range creds[1:] {
		userID, err := resolveUserID(dbURL, c.Email)
		if err != nil {
			_ = removeLocalProject(dbURL, proj.ID)
			return "", nil, fmt.Errorf("resolving user id for %s: %w", c.Email, err)
		}
		if err := psqlExec(dbURL, fmt.Sprintf(
			"insert into project_members (project_id, user_id, role) values ('%s', '%s', 'resumer') on conflict do nothing;",
			proj.ID, userID)); err != nil {
			_ = removeLocalProject(dbURL, proj.ID)
			return "", nil, fmt.Errorf("granting %s resumer access: %w", c.Email, err)
		}
	}

	cleanup = func() {
		_ = removeLocalProject(dbURL, proj.ID)
	}
	return proj.ID, cleanup, nil
}

func removeLocalProject(dbURL, projectID string) error {
	for _, sql := range []string{
		fmt.Sprintf("delete from sessions where project_id = '%s';", projectID),
		fmt.Sprintf("delete from project_members where project_id = '%s';", projectID),
		fmt.Sprintf("delete from projects where id = '%s';", projectID),
	} {
		if err := psqlExec(dbURL, sql); err != nil {
			return err
		}
	}
	return nil
}

func resolveUserID(dbURL, email string) (string, error) {
	out, err := exec.Command("psql", dbURL, "-t", "-A", "-v", "ON_ERROR_STOP=1", "-c", // #nosec G204 -- fixed local dev DB URL plus a flag-supplied email, test/dry-run tooling only
		fmt.Sprintf("select id from auth.users where email = '%s';", email)).Output()
	if err != nil {
		return "", err
	}
	id := trimTrailingNewline(out)
	if id == "" {
		return "", fmt.Errorf("no auth.users row for %s — is local Supabase seeded?", email)
	}
	return id, nil
}

func psqlExec(dbURL, sql string) error {
	out, err := exec.Command("psql", dbURL, "-v", "ON_ERROR_STOP=1", "-c", sql).CombinedOutput() // #nosec G204 -- fixed local dev DB URL, test/dry-run tooling only
	if err != nil {
		return fmt.Errorf("psql %q: %w\n%s", sql, err, out)
	}
	return nil
}

// passwordGrantForProvisioning is a small, self-contained copy of
// internal/loadtest's own (unexported) passwordGrant — duplicated
// rather than exported across a package boundary for this one
// bootstrap call, per this codebase's own "wait for the third
// occurrence" rule (go-conventions.md echoes CLAUDE.md's "no
// premature abstractions" here).
func passwordGrantForProvisioning(ctx context.Context, apiURL, anonKey string, cred loadtest.Credential, httpClient *http.Client) (string, error) {
	body, err := json.Marshal(map[string]string{"email": cred.Email, "password": cred.Password})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/auth/v1/token?grant_type=password", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("apikey", anonKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
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
		return "", fmt.Errorf("HTTP %d %s %s", resp.StatusCode, out.Error, out.Msg)
	}
	return out.AccessToken, nil
}

func trimTrailingNewline(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
