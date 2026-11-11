package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// passwordGrant signs in against Supabase Auth's own password grant —
// the same real endpoint internal/auth's PKCE flow ultimately hits,
// just driven directly the way cmd/carryon's own e2e_test.go does for
// its seeded local/CI users. For a real staging run, these are real
// accounts an operator provisions ahead of time (docs/co-505-load-test.md)
// — this package never creates one.
func passwordGrant(ctx context.Context, apiURL, anonKey, email, password string, httpClient *http.Client) (string, error) {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
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
		return "", fmt.Errorf("password grant for %s: HTTP %d %s %s", email, resp.StatusCode, out.Error, out.Msg)
	}
	return out.AccessToken, nil
}
