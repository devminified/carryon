package api

import "context"

// Project is what register-project and match-project return.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GitRemote string `json:"gitRemote"`
}

// RegisterProject calls the register-project Edge Function (CO-104). The
// caller becomes the project's first member with role "admin". gitRemote
// must already be in canonical form (project.NormalizeRemote) — the
// function re-normalizes server-side too, but passing an already-
// canonical value avoids a pointless round trip through two different
// implementations of the same logic disagreeing.
func (c *Client) RegisterProject(ctx context.Context, accessToken, name, gitRemote string) (*Project, error) {
	body := map[string]string{"name": name, "gitRemote": gitRemote}
	var out Project
	if err := c.post(ctx, "register-project", accessToken, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MatchProjectResult is what match-project returns on success.
type MatchProjectResult struct {
	ProjectID string `json:"projectId"`
	Role      string `json:"role"`
}

// MatchProject calls the match-project Edge Function (CO-104, used by the
// capture agent from CO-108 onward). Returns an *Error satisfying
// IsNotFound if gitRemote isn't registered, or IsForbidden if it's
// registered but the caller isn't a member.
func (c *Client) MatchProject(ctx context.Context, accessToken, gitRemote string) (*MatchProjectResult, error) {
	body := map[string]string{"gitRemote": gitRemote}
	var out MatchProjectResult
	if err := c.post(ctx, "match-project", accessToken, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
