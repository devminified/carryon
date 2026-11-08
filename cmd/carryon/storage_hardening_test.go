package main

// CO-305's own real-world testing points, verified against a real local
// Supabase instance (gated and structured the same way as
// TestPullEndToEndAgainstLocalSupabase — skip if local Supabase isn't
// up, never run against anything but 127.0.0.1):
//
//   - "An object in a client bucket is unreachable through a project in
//     another bucket" — a project with its own dedicated bucket really
//     lands its chunk's bytes there, not in the shared default bucket,
//     and a real pull through that project genuinely works.
//   - "A signed URL used after 301 seconds fails" — verified as the real
//     expiry MECHANISM (Supabase Storage's own JWT "exp" check) with a
//     short TTL, not by literally waiting 300+ seconds in CI.
//   - "An upload over the size limit is refused" — a real PUT past the
//     10MiB file_size_limit against a real signed upload URL.
//   - "The bucket is private, with no public URLs" — the real
//     /storage/v1/object/public/... route rejects a private bucket.
//
// Creating a dedicated bucket is a manual, one-time ops action in this
// system (see the CO-305 migration's own doc comment) — there's no
// Edge Function for it, so these tests do it directly against the
// Storage REST API with the local dev service-role key, the same key
// `supabase status` prints for every fresh `carryon-dev` checkout (not
// a secret specific to this deployment).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/pull"
	"github.com/devminified/carryon/internal/upload"
)

// mustJSON marshals v for a raw admin HTTP request body — any marshal
// error here means the literal map above this call is malformed, a
// programmer error worth failing loudly on rather than threading an
// error return through every call site.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling request body: %v", err)
	}
	return b
}

// e2eServiceRoleKey is the well-known local-dev service-role JWT
// `supabase status` prints for any freshly-initialized project using
// Supabase's stock local JWT secret — not a real credential, and not
// reachable from outside 127.0.0.1 in this test's own gating. Needed
// here because bucket creation/deletion and a custom-TTL signed URL
// are service-role-only operations with no Edge Function in front of
// them (CO-305's bucket creation is a manual ops action, not an API).
const e2eServiceRoleKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
	"eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0." +
	"EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU"

// e2eStorageAdmin fires one service-role request against the local
// Storage REST API and returns the decoded JSON body plus HTTP status.
func e2eStorageAdmin(t *testing.T, method, path string, body io.Reader) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, e2eAPIURL+path, body) // #nosec G107 -- fixed local dev URL, test-only
	if err != nil {
		t.Fatalf("building storage admin request %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+e2eServiceRoleKey)
	req.Header.Set("apikey", e2eServiceRoleKey)
	if body != nil {
		// Storage's DELETE route 400s on an empty body if this header is
		// set at all — found by a real DELETE failing silently (this
		// helper didn't check status) and leaving a cleanup's object
		// behind for the next run to collide with.
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("storage admin request %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestDedicatedBucketIsolation(t *testing.T) {
	aliToken, err := passwordGrantToken(e2eAliEmail, e2ePassword)
	if err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}
	if _, err := (&http.Client{Timeout: 3 * time.Second}).Get(e2eAPIURL + "/functions/v1/"); err != nil {
		t.Skipf("local Supabase Edge Functions not served (run `supabase functions serve` too): %v", err)
	}

	ctx := context.Background()
	client := api.NewClient(e2eAPIURL, e2eAnonKey)

	// --- a real dedicated bucket, created the same manual way an
	// operator would for a client contract that requires physical
	// separation (CO-305 migration's own doc comment: bucket creation
	// is not automated by any Edge Function).
	const dedicatedBucket = "co305-dedicated-isolation-test"
	status, body := e2eStorageAdmin(t, http.MethodPost, "/storage/v1/bucket", bytes.NewReader(mustJSON(t, map[string]any{
		"id": dedicatedBucket, "name": dedicatedBucket, "public": false, "file_size_limit": 10 * 1024 * 1024,
	})))
	if status != http.StatusOK {
		t.Fatalf("creating dedicated bucket: HTTP %d %v", status, body)
	}
	t.Cleanup(func() {
		e2eStorageAdmin(t, http.MethodPost, "/storage/v1/bucket/"+dedicatedBucket+"/empty", bytes.NewReader([]byte("{}")))
		e2eStorageAdmin(t, http.MethodDelete, "/storage/v1/bucket/"+dedicatedBucket, nil)
	})

	proj, err := client.RegisterProject(ctx, aliToken, "e2e-dedicated-bucket-test", "github.com/devminified/e2e-dedicated-bucket-test")
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}
	t.Cleanup(func() {
		e2ePsql(t, fmt.Sprintf("delete from sessions where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from project_members where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from projects where id = '%s';", proj.ID))
	})

	// Pointing a project at its own bucket is likewise a direct,
	// one-time DB action (the column has no Edge Function write path —
	// see database-rls.md's projects.bucket note), not something this
	// test should fake by any other means.
	e2ePsql(t, fmt.Sprintf("update projects set bucket = '%s' where id = '%s';", dedicatedBucket, proj.ID))

	root := t.TempDir()
	origSessionID, err := pull.NewSessionID()
	if err != nil {
		t.Fatalf("generating session id: %v", err)
	}
	transcriptPath := filepath.Join(root, origSessionID+".jsonl")
	fixtureLine := fmt.Sprintf(`{"sessionId":%q,"cwd":%q,"type":"user","message":{"text":"hello from the dedicated bucket test"}}`+"\n",
		origSessionID, root)
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
		Cwd:            root,
		TranscriptPath: transcriptPath,
		ProjectID:      proj.ID,
		Final:          true,
	}); err != nil {
		t.Fatalf("real Upload() error = %v", err)
	}

	manifest, err := client.GetManifest(ctx, aliToken, origSessionID)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	if len(manifest.Chunks) == 0 {
		t.Fatalf("manifest has no chunks")
	}
	chunk := manifest.Chunks[0]
	if chunk.Bucket != dedicatedBucket {
		t.Errorf("manifest chunk Bucket = %q, want the project's dedicated bucket %q", chunk.Bucket, dedicatedBucket)
	}

	// --- the real object landed in the dedicated bucket, not the
	// shared default one.
	var actualBucket string
	e2eQueryRow(t, fmt.Sprintf("select bucket_id from storage.objects where name = '%s';", chunk.ObjectKey), &actualBucket)
	if actualBucket != dedicatedBucket {
		t.Errorf("storage.objects.bucket_id = %q, want %q (the object leaked into the wrong bucket)", actualBucket, dedicatedBucket)
	}

	var inDefaultBucket string
	e2eQueryRow(t, fmt.Sprintf(
		"select count(*)::text from storage.objects where name = '%s' and bucket_id = 'carryon-sessions';", chunk.ObjectKey),
		&inDefaultBucket)
	if inDefaultBucket != "0" {
		t.Errorf("object %q also exists in the shared default bucket — isolation broken", chunk.ObjectKey)
	}

	// --- a real pull, through the dedicated bucket, rebuilds the exact
	// same bytes — proving the Go client's StorageDownloadURL actually
	// reaches the bucket the server signed against, not a hardcoded one.
	destDir := filepath.Join(root, "rebuilt")
	result, err := pull.Rebuild(ctx, client, manifest, destDir, pull.DefaultConfig())
	if err != nil {
		t.Fatalf("Rebuild() against the dedicated bucket error = %v", err)
	}
	rebuilt, err := os.ReadFile(result.MainFile)
	if err != nil {
		t.Fatalf("reading rebuilt transcript: %v", err)
	}
	if string(rebuilt) != fixtureLine {
		t.Errorf("rebuilt transcript = %q, want byte-identical to %q", rebuilt, fixtureLine)
	}
}

// TestSignedDownloadURLGenuinelyExpires verifies the real expiry
// mechanism get-manifest's 5-minute download tokens rely on — Supabase
// Storage's own JWT "exp" claim check — using a short TTL instead of
// waiting 300+ seconds. A signed URL that works immediately and is
// rejected once its "exp" has passed is the same code path a production
// 300s token hits, just compressed in time for CI.
func TestSignedDownloadURLGenuinelyExpires(t *testing.T) {
	if _, err := passwordGrantToken(e2eAliEmail, e2ePassword); err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}

	const objectKey = "co305-expiry-test.txt"
	status, body := e2eStorageAdmin(t, http.MethodPost, "/storage/v1/object/carryon-sessions/"+objectKey,
		bytes.NewReader([]byte("expiry test object")))
	if status != http.StatusOK {
		t.Fatalf("PUTting expiry test object: HTTP %d %v", status, body)
	}
	t.Cleanup(func() {
		e2eStorageAdmin(t, http.MethodDelete, "/storage/v1/object/carryon-sessions/"+objectKey, nil)
	})

	status, signBody := e2eStorageAdmin(t, http.MethodPost, "/storage/v1/object/sign/carryon-sessions/"+objectKey,
		bytes.NewReader(mustJSON(t, map[string]any{"expiresIn": 1})))
	if status != http.StatusOK {
		t.Fatalf("signing short-TTL download URL: HTTP %d %v", status, signBody)
	}
	signedPath, _ := signBody["signedURL"].(string)
	if signedPath == "" {
		t.Fatalf("sign response had no signedURL: %v", signBody)
	}

	// Immediately: the token is valid, the GET succeeds.
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(e2eAPIURL + "/storage/v1" + signedPath)
	if err != nil {
		t.Fatalf("GET signed URL (fresh): %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET signed URL (fresh) = HTTP %d, want 200", resp.StatusCode)
	}

	// Past its 1-second TTL: Storage's own "exp" check must now reject
	// it — the exact mechanism a real 300s download token relies on.
	time.Sleep(2 * time.Second)
	resp2, err := (&http.Client{Timeout: 5 * time.Second}).Get(e2eAPIURL + "/storage/v1" + signedPath)
	if err != nil {
		t.Fatalf("GET signed URL (expired): %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode == http.StatusOK {
		t.Errorf("GET signed URL (expired) = HTTP 200, want a rejection — the signed URL did not actually expire")
	}
}

// TestUploadOverSizeLimitIsRefused proves the 10MiB file_size_limit
// CO-305's migration set on the shared bucket is enforced by real
// Storage, not just documented — a real signed upload URL (obtained the
// same way the agent's own upload pipeline does, via register-chunk)
// rejects a real PUT past the limit.
func TestUploadOverSizeLimitIsRefused(t *testing.T) {
	aliToken, err := passwordGrantToken(e2eAliEmail, e2ePassword)
	if err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}
	if _, err := (&http.Client{Timeout: 3 * time.Second}).Get(e2eAPIURL + "/functions/v1/"); err != nil {
		t.Skipf("local Supabase Edge Functions not served (run `supabase functions serve` too): %v", err)
	}

	ctx := context.Background()
	client := api.NewClient(e2eAPIURL, e2eAnonKey)

	proj, err := client.RegisterProject(ctx, aliToken, "e2e-oversize-upload-test", "github.com/devminified/e2e-oversize-upload-test")
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}
	t.Cleanup(func() {
		e2ePsql(t, fmt.Sprintf("delete from sessions where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from project_members where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from projects where id = '%s';", proj.ID))
	})

	sessionID, err := pull.NewSessionID()
	if err != nil {
		t.Fatalf("generating session id: %v", err)
	}

	const oversize = 12 * 1024 * 1024 // past the 10MiB limit
	reg, err := client.RegisterChunk(ctx, aliToken, api.RegisterChunkInput{
		SessionID:   sessionID,
		ProjectID:   proj.ID,
		OriginalCwd: "/tmp/e2e-oversize",
		StreamID:    "main",
		Seq:         0,
		ByteStart:   0,
		ByteEnd:     oversize,
		SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		Size:        oversize,
	})
	if err != nil {
		t.Fatalf("RegisterChunk() error = %v", err)
	}
	t.Cleanup(func() {
		e2eStorageAdmin(t, http.MethodDelete, "/storage/v1/object/"+reg.Bucket+"/"+reg.ObjectKey, nil)
	})

	uploadURL := client.StorageUploadURL(reg.Bucket, reg.ObjectKey, reg.UploadToken)
	req, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(make([]byte, oversize)))
	if err != nil {
		t.Fatalf("building oversized PUT: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("oversized PUT: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Errorf("oversized PUT succeeded with HTTP %d, want a rejection (body: %s)", resp.StatusCode, respBody)
	}
}

// TestPrivateBucketHasNoPublicURL confirms the shared bucket really is
// private — Storage's /object/public/... route, which serves public
// buckets with no signature at all, must not serve anything from it.
func TestPrivateBucketHasNoPublicURL(t *testing.T) {
	if _, err := passwordGrantToken(e2eAliEmail, e2ePassword); err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(e2eAPIURL + "/storage/v1/object/public/carryon-sessions/anything.txt")
	if err != nil {
		t.Fatalf("GET public URL on private bucket: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("GET public URL on carryon-sessions = HTTP 200, want a rejection — the bucket is not private")
	}
}
