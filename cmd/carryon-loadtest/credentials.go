package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/devminified/carryon/internal/loadtest"
)

// loadCredentials reads a JSON array of {"email":...,"password":...}
// — real accounts an operator provisions ahead of time on the target
// backend (docs/co-505-load-test.md's own manual setup step, same
// shape as this codebase's other one-time external setup steps —
// auth.md's GitHub OAuth App, GitHub App). This tool never creates an
// account itself.
func loadCredentials(path string) ([]loadtest.Credential, error) {
	// #nosec G304 -- an operator-supplied path to their own credentials file, not attacker input
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading -credentials-file %s: %w", path, err)
	}
	var creds []loadtest.Credential
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, fmt.Errorf("parsing -credentials-file %s: %w", path, err)
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("-credentials-file %s contains no credentials", path)
	}
	for i, c := range creds {
		if c.Email == "" || c.Password == "" {
			return nil, fmt.Errorf("-credentials-file %s: entry %d is missing email or password", path, i)
		}
	}
	return creds, nil
}
