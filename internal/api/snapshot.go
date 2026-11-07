package api

import "context"

// RegisterSnapshotInput is what register-snapshot (CO-201) needs after
// the agent pushes a working-tree snapshot to a hidden ref on the
// session's own GitHub remote — the push itself never goes through this
// client; this just records the pointer.
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
		"wipRef":    in.WipRef,
		"treeSha":   in.TreeSHA,
	}
	return c.post(ctx, "register-snapshot", accessToken, body, nil)
}
