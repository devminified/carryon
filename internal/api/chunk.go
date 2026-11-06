package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// RegisterChunkInput is what register-chunk (CO-110) needs before the
// agent uploads one compressed chunk. ByteStart/ByteEnd/SHA256/Size
// describe the *uncompressed* slice. StreamID is "main" for a session's
// own transcript, or a subagent's agent_id for one of its child streams
// (CO-109) — without it, a subagent's chunk numbering could collide with
// the main stream's.
//
// Machine/Branch/HeadSHA/ClaudeCodeVersion are CO-111's session-
// identifying fields — the server can't exec git or read the laptop's
// own hostname, so the agent gathers them and sends them along. They're
// only ever used by register-chunk's insert-if-missing on a session's
// first chunk; harmless to send on every call.
type RegisterChunkInput struct {
	SessionID         string
	ProjectID         string
	OriginalCwd       string
	StreamID          string
	Machine           string
	Branch            string
	HeadSHA           string
	ClaudeCodeVersion string
	Seq               int
	ByteStart         int64
	ByteEnd           int64
	SHA256            string
	Size              int64
}

// RegisterChunkResult is register-chunk's response: a signed Storage
// upload token (expires in a fixed 2 hours — see
// .claude/rules/redaction.md — not the usual 5 minutes) and the object
// key confirm-chunk will later verify. UploadToken, not a full URL — the
// Edge Function's own signed URL bakes in its internal SUPABASE_URL
// (unreachable from outside Docker in local dev); the token itself
// carries no host claim, so build the actual upload URL with
// Client.StorageUploadURL instead (see redaction.md).
type RegisterChunkResult struct {
	UploadToken string `json:"uploadToken"`
	ObjectKey   string `json:"objectKey"`
}

// RegisterChunk calls the register-chunk Edge Function (CO-110). Returns
// an *Error satisfying IsForbidden if the caller isn't a member of
// ProjectID.
func (c *Client) RegisterChunk(ctx context.Context, accessToken string, in RegisterChunkInput) (*RegisterChunkResult, error) {
	body := map[string]any{
		"sessionId":   in.SessionID,
		"projectId":   in.ProjectID,
		"originalCwd": in.OriginalCwd,
		"streamId":    in.StreamID,
		"seq":         in.Seq,
		"byteStart":   in.ByteStart,
		"byteEnd":     in.ByteEnd,
		"sha256":      in.SHA256,
		"size":        in.Size,
	}
	// Omitted rather than sent as "" when gathering failed — register-chunk's
	// Zod schema treats these as optional ("unknown"), not "known to be
	// empty", and an omitted key leaves the column null instead of storing
	// a misleading empty string.
	if in.Machine != "" {
		body["machine"] = in.Machine
	}
	if in.Branch != "" {
		body["branch"] = in.Branch
	}
	if in.HeadSHA != "" {
		body["headSha"] = in.HeadSHA
	}
	if in.ClaudeCodeVersion != "" {
		body["claudeCodeVersion"] = in.ClaudeCodeVersion
	}
	var out RegisterChunkResult
	if err := c.post(ctx, "register-chunk", accessToken, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConfirmChunkInput is what confirm-chunk (CO-110) needs to verify an
// uploaded chunk and record it. The object key is deliberately not part
// of this input — confirm-chunk derives it itself from the session's own
// stored project/user, so a caller can't point it at an unrelated object.
type ConfirmChunkInput struct {
	SessionID string
	StreamID  string
	Seq       int
	ByteStart int64
	ByteEnd   int64
	SHA256    string
}

// ConfirmChunk calls the confirm-chunk Edge Function (CO-110), which
// verifies the chunk's object actually exists in Storage before recording
// it. Returns an *Error satisfying IsConflict if the object is missing
// (the upload may have failed) — the caller should re-upload, not just
// retry confirm-chunk.
func (c *Client) ConfirmChunk(ctx context.Context, accessToken string, in ConfirmChunkInput) error {
	body := map[string]any{
		"sessionId": in.SessionID,
		"streamId":  in.StreamID,
		"seq":       in.Seq,
		"byteStart": in.ByteStart,
		"byteEnd":   in.ByteEnd,
		"sha256":    in.SHA256,
	}
	return c.post(ctx, "confirm-chunk", accessToken, body, nil)
}

// IsConflict reports whether err is a 409 from an Edge Function (e.g.
// confirm-chunk's "chunk object not found in storage").
func IsConflict(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// chunkBucket is the private Storage bucket chunks live in — created by
// CO-102's migration, named identically in supabase/functions/*/index.ts's
// own BUCKET constant.
const chunkBucket = "carryon-sessions"

// StorageUploadURL builds the actual PUT URL for a chunk's signed upload
// token from the client's own base URL — see RegisterChunkResult's doc
// comment for why this must never be built from an Edge Function's own
// signed-URL string instead.
func (c *Client) StorageUploadURL(objectKey, token string) string {
	return fmt.Sprintf("%s/storage/v1/object/upload/sign/%s/%s?token=%s", c.baseURL, chunkBucket, objectKey, url.QueryEscape(token))
}
