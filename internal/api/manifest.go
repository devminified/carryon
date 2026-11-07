package api

import (
	"context"
	"time"
)

// SessionMeta is get-manifest's (CO-203) session metadata. ProjectName,
// GitRemote and LastActivityAt were added by CO-211, which is the
// first caller needing them — to locate or clone a local checkout
// before restoring a worktree, and to render the pre-launch header —
// rather than a second round trip through list-sessions just for these
// three columns.
type SessionMeta struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	ProjectName       string    `json:"projectName"`
	GitRemote         string    `json:"gitRemote"`
	OwnerLogin        string    `json:"ownerLogin"`
	OriginalCwd       string    `json:"originalCwd"`
	Branch            string    `json:"branch"`
	HeadSHA           string    `json:"headSha"`
	ClaudeCodeVersion string    `json:"claudeCodeVersion"`
	Status            string    `json:"status"`
	LastActivityAt    time.Time `json:"lastActivityAt"`
}

// ManifestChunk is one of get-manifest's ordered chunks. ObjectKey and
// DownloadToken build the actual GET URL via Client.StorageDownloadURL
// — never trust a signed URL an Edge Function might return directly.
type ManifestChunk struct {
	StreamID      string `json:"streamId"`
	Seq           int    `json:"seq"`
	ByteStart     int64  `json:"byteStart"`
	ByteEnd       int64  `json:"byteEnd"`
	SHA256        string `json:"sha256"`
	ObjectKey     string `json:"objectKey"`
	DownloadToken string `json:"downloadToken"`
}

// ManifestSnapshot is get-manifest's latest code_snapshots row: either a
// hidden git ref (WipRef set) or a bundle fallback (ObjectKey/
// DownloadToken set — CO-202).
type ManifestSnapshot struct {
	Seq           int    `json:"seq"`
	HeadSHA       string `json:"headSha"`
	TreeSHA       string `json:"treeSha"`
	WipRef        string `json:"wipRef,omitempty"`
	ObjectKey     string `json:"objectKey,omitempty"`
	DownloadToken string `json:"downloadToken,omitempty"`
}

// Manifest is get-manifest's full response (CO-203). Snapshot is nil
// when the session has no code snapshot yet.
type Manifest struct {
	Session  SessionMeta       `json:"session"`
	Chunks   []ManifestChunk   `json:"chunks"`
	Snapshot *ManifestSnapshot `json:"snapshot"`
}

// GetManifest calls the get-manifest Edge Function (CO-203). Returns an
// *Error satisfying IsForbidden if the caller isn't resumer+ on the
// session's project, IsNotFound if sessionID doesn't exist.
func (c *Client) GetManifest(ctx context.Context, accessToken, sessionID string) (*Manifest, error) {
	body := map[string]string{"sessionId": sessionID}
	var out Manifest
	if err := c.post(ctx, "get-manifest", accessToken, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
