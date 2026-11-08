package api

import "context"

// RedactionConfig is get-config's (CO-301) response: which built-in
// internal/redact rules are disabled, and which extra path-denylist
// globs apply, on top of internal/redact.DefaultConfig. There's no
// field for a raw regex pattern — get-config can narrow or extend
// coverage, never supply code this binary would execute. See
// internal/redact's Rule doc comment.
//
// PinnedVersion (CO-308) is otherwise unrelated to redaction — see
// get-config/index.ts's own doc comment for why it lives on this same
// response anyway (the ticket's own instruction). "" means no pin.
type RedactionConfig struct {
	DisabledRuleIDs   []string `json:"disabledRuleIds"`
	DenylistAdditions []string `json:"denylistAdditions"`
	PinnedVersion     string   `json:"pinnedVersion"`
}

// GetConfig calls the get-config Edge Function (CO-301). Every
// authenticated user gets the same global config — there's no
// per-project redaction config in the current build plan, so this
// takes no input beyond the caller's own token.
func (c *Client) GetConfig(ctx context.Context, accessToken string) (*RedactionConfig, error) {
	var out RedactionConfig
	if err := c.post(ctx, "get-config", accessToken, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
