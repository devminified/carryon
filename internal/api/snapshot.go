package api

import "context"

// RegisterSnapshotInput is what register-snapshot (CO-201) needs after
// the agent pushes a working-tree snapshot to a hidden ref on the
// session's own GitHub remote — the push itself never goes through this
// client; this just records the pointer.
//
// WipRef is left empty on the git-bundle fallback path (CO-202, when the
// ref push was rejected) — the server derives and verifies the bundle's
// object key itself from the session's own stored identity, the same
// way confirm-chunk never trusts a client-supplied chunk object key.
type RegisterSnapshotInput struct {
	SessionID string
	Seq       int
	HeadSHA   string
	WipRef    string
	TreeSHA   string
}

// RegisterSnapshot calls the register-snapshot Edge Function (CO-201).
// Idempotent on (session_id, seq) — a retried call for the same seq is a
// safe no-op (I3).
func (c *Client) RegisterSnapshot(ctx context.Context, accessToken string, in RegisterSnapshotInput) error {
	body := map[string]any{
		"sessionId": in.SessionID,
		"seq":       in.Seq,
		"headSha":   in.HeadSHA,
		"treeSha":   in.TreeSHA,
	}
	// Omitted rather than sent as "" on the bundle fallback path —
	// register-snapshot's Zod schema treats wipRef as optional, and an
	// omitted key is what tells it to use the bundle path instead of
	// rejecting an empty string as invalid.
	if in.WipRef != "" {
		body["wipRef"] = in.WipRef
	}
	return c.post(ctx, "register-snapshot", accessToken, body, nil)
}

// SignSnapshotBundleInput is what sign-snapshot-bundle (CO-202) needs to
// sign an upload URL for a git-bundle fallback snapshot.
type SignSnapshotBundleInput struct {
	SessionID string
	Seq       int
}

// SignSnapshotBundleResult is sign-snapshot-bundle's response: a signed
// Storage upload token (not a full URL — see RegisterChunkResult's doc
// comment for why: the Edge Function's own signed URL bakes in its
// internal SUPABASE_URL), the object key confirm-chunk-style
// verification will later check for, and the bucket (CO-305) the token
// was signed against.
type SignSnapshotBundleResult struct {
	UploadToken string `json:"uploadToken"`
	ObjectKey   string `json:"objectKey"`
	Bucket      string `json:"bucket"`
}

// SignSnapshotBundle calls the sign-snapshot-bundle Edge Function
// (CO-202). Returns an *Error satisfying IsForbidden if the caller isn't
// a member of the session's project.
func (c *Client) SignSnapshotBundle(ctx context.Context, accessToken string, in SignSnapshotBundleInput) (*SignSnapshotBundleResult, error) {
	body := map[string]any{
		"sessionId": in.SessionID,
		"seq":       in.Seq,
	}
	var out SignSnapshotBundleResult
	if err := c.post(ctx, "sign-snapshot-bundle", accessToken, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
