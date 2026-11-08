package main

// CO-307's own real end-to-end testing points, verified against a real
// local Supabase instance — gated and structured the same way as
// TestPullEndToEndAgainstLocalSupabase (skip if local Supabase isn't
// up), reusing its e2e* helpers:
//
//   - "Deleting a parent session keeps its forks, with
//     parent_session_id set to null."
//   - "A deletion without a reason is refused."
//   - Admin-only: a resumer (non-admin) is refused.
//   - The deleted session's chunk object is actually gone from real
//     Storage, not just the DB row.
//
// purge-expired-sessions (the daily retention sweep) has no Go client
// at all — it's cron-only, called with a shared secret, never by the
// laptop — so its own real-world verification was done the same way
// CO-306's rescan was: a direct, cleaned-up manual verification
// against local Supabase rather than a Go test here.

import (
	"context"
	"fmt"
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

func TestAdminDeleteSessionEndToEnd(t *testing.T) {
	aliToken, err := passwordGrantToken(e2eAliEmail, e2ePassword)
	if err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}
	saraToken, err := passwordGrantToken(e2eSaraEmail, e2ePassword)
	if err != nil {
		t.Fatalf("password grant for sara failed (ali's succeeded, so the stack is up): %v", err)
	}
	if _, err := (&http.Client{Timeout: 3 * time.Second}).Get(e2eAPIURL + "/functions/v1/"); err != nil {
		t.Skipf("local Supabase Edge Functions not served (run `supabase functions serve` too): %v", err)
	}

	ctx := context.Background()
	client := api.NewClient(e2eAPIURL, e2eAnonKey)

	proj, err := client.RegisterProject(ctx, aliToken, "e2e-admin-delete-test", "github.com/devminified/e2e-admin-delete-test")
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}
	t.Cleanup(func() {
		e2ePsql(t, fmt.Sprintf("delete from sessions where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from project_members where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from audit_log where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from projects where id = '%s';", proj.ID))
	})

	// sara is a resumer — enough to pull/fork, not enough to delete.
	e2ePsql(t, fmt.Sprintf(
		"insert into project_members (project_id, user_id, role) values ('%s', '%s', 'resumer');",
		proj.ID, e2eSaraUserID))

	root := t.TempDir()
	parentID, err := pull.NewSessionID()
	if err != nil {
		t.Fatalf("generating session id: %v", err)
	}
	transcriptPath := filepath.Join(root, parentID+".jsonl")
	fixtureLine := fmt.Sprintf(`{"sessionId":%q,"cwd":%q,"type":"user","message":{"text":"hello from the delete test"}}`+"\n",
		parentID, root)
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
		ID:             parentID,
		Cwd:            root,
		TranscriptPath: transcriptPath,
		ProjectID:      proj.ID,
		Final:          true,
	}); err != nil {
		t.Fatalf("real Upload() error = %v", err)
	}

	manifest, err := client.GetManifest(ctx, aliToken, parentID)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	if len(manifest.Chunks) == 0 {
		t.Fatalf("manifest has no chunks")
	}
	chunkObjectKey, chunkBucket := manifest.Chunks[0].ObjectKey, manifest.Chunks[0].Bucket

	// sara forks the parent (a real fork, via the real register-fork
	// Edge Function — the same one `carryon pull` calls).
	forkID, err := pull.NewSessionID()
	if err != nil {
		t.Fatalf("generating fork session id: %v", err)
	}
	if _, err := client.RegisterFork(ctx, saraToken, api.RegisterForkInput{ParentSessionID: parentID, NewSessionID: forkID}); err != nil {
		t.Fatalf("RegisterFork() error = %v", err)
	}

	// --- a deletion without a reason is refused ---
	if err := client.DeleteSession(ctx, aliToken, parentID, ""); err == nil {
		t.Errorf("DeleteSession() with an empty reason = nil error, want a validation error")
	}
	if err := client.DeleteSession(ctx, aliToken, parentID, "   "); err == nil {
		t.Errorf("DeleteSession() with a whitespace-only reason = nil error, want a validation error")
	}

	// --- a resumer (sara) is refused — admin-only ---
	if err := client.DeleteSession(ctx, saraToken, parentID, "sara trying to delete"); !api.IsForbidden(err) {
		t.Errorf("DeleteSession() as a resumer = %v, want IsForbidden", err)
	}

	// --- ali (admin) deletes it for real, with a real reason ---
	const wantReason = "e2e test: cleaning up after itself"
	if err := client.DeleteSession(ctx, aliToken, parentID, wantReason); err != nil {
		t.Fatalf("DeleteSession() as admin error = %v", err)
	}

	// --- the session row is really gone ---
	var sessionCount string
	e2eQueryRow(t, fmt.Sprintf("select count(*)::text from sessions where id = '%s';", parentID), &sessionCount)
	if sessionCount != "0" {
		t.Errorf("sessions row for %s still exists after delete", parentID)
	}

	// --- its chunk object is really gone from Storage, not just the row ---
	infoReq, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/storage/v1/object/info/%s/%s", e2eAPIURL, chunkBucket, chunkObjectKey), nil)
	if err != nil {
		t.Fatalf("building storage info request: %v", err)
	}
	infoReq.Header.Set("Authorization", "Bearer "+e2eServiceRoleKey)
	infoReq.Header.Set("apikey", e2eServiceRoleKey)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(infoReq)
	if err != nil {
		t.Fatalf("checking storage object: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("chunk object %s/%s still exists in Storage after delete", chunkBucket, chunkObjectKey)
	}

	// --- the fork survives, reparented to no parent ---
	var forkExists, forkParent string
	e2eQueryRow(t, fmt.Sprintf("select count(*)::text from sessions where id = '%s';", forkID), &forkExists)
	if forkExists != "1" {
		t.Fatalf("fork session %s was deleted along with its parent — forks must survive", forkID)
	}
	e2eQueryRow(t, fmt.Sprintf("select coalesce(parent_session_id::text, 'NULL') from sessions where id = '%s';", forkID), &forkParent)
	if forkParent != "NULL" {
		t.Errorf("fork's parent_session_id = %q, want NULL after the parent was deleted", forkParent)
	}

	// --- the audit log has the reason, as the only remaining record ---
	var auditAction, auditReason string
	e2eQueryRow(t, fmt.Sprintf(
		"select action, reason from audit_log where session_id = '%s' and action = 'delete_session';", parentID),
		&auditAction, &auditReason)
	if auditAction != "delete_session" || auditReason != wantReason {
		t.Errorf("audit_log row = (%q, %q), want (delete_session, %q)", auditAction, auditReason, wantReason)
	}

	t.Cleanup(func() {
		e2ePsql(t, fmt.Sprintf("delete from sessions where id = '%s';", forkID))
	})
}
