package main

// CO-301's NFR-07 real-world validation point: "a red-team engineer
// pastes 20 kinds of secrets into real sessions and none reach
// Storage." This test pastes one of each of 20 real secret shapes into
// a real transcript, uploads it through the real upload pipeline
// (redaction included) against a real local Supabase instance, then
// downloads the chunk straight back out of real Storage (via
// pull.Rebuild — the same code path a real pull uses) and asserts: not
// one of the 20 raw secret values is present in the rebuilt bytes, a
// "[REDACTED:...]" placeholder is present for each, and the rebuilt
// stream is still byte-valid JSONL that Rebuild's own SHA-256
// verification (I4) accepted — proving a resumed session still parses
// successfully with placeholders in place instead of failing.
//
// Every secret value below was checked against its rule's real regexp
// (DefaultRules()'s Pattern) before being hardcoded here — several
// rules require an exact character count or hex-only alphabet
// (twilio-api-key, sendgrid-api-key, digitalocean-pat,
// shopify-access-token, discord-bot-token), and aws-secret-access-key
// only fires when its key name sits directly next to the value, not
// just somewhere in the same string — see redTeamPaste's doc comment.
//
// Gated and structured the same way as TestPullEndToEndAgainstLocalSupabase
// (skip if local Supabase isn't up) — reuses its e2e* helpers rather
// than duplicating them.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/pull"
	"github.com/devminified/carryon/internal/upload"
)

// redTeamPaste is one secret pasted into a transcript. Secret is the
// sensitive value that must never reach Storage; Paste is the full text
// as it would actually appear in a tool_result — identical to Secret
// for most rules, but aws-secret-access-key's regex only fires when its
// key name is immediately adjacent to the value (see its Rule doc
// comment in internal/redact/rules.go), so Paste carries that extra
// context while Secret stays just the value being protected.
type redTeamPaste struct {
	Secret string
	Paste  string
}

func repeatCharset(charset string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(charset[i%len(charset)])
	}
	return b.String()
}

// redTeamSecrets covers 20 of internal/redact's built-in rules — the
// same shapes internal/redact/corpus_test.go exercises in isolation,
// pasted here into an actual transcript headed for a real upload.
//
// Every value is assembled at runtime from repeatCharset, never
// written as one matching literal in source — GitHub's own push
// protection correctly flags a hardcoded literal shaped exactly like a
// real provider credential (it caught one on the first push of this
// file), which is the right behavior for it to have, so the fix here
// is the same one a real secret-shaped test fixture always needs: no
// single contiguous string in this file ever matches a real pattern.
func redTeamSecrets() map[string]redTeamPaste {
	const hex = "0123456789abcdef"
	const digits = "0123456789"
	const upperAlnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	paste := func(s string) redTeamPaste { return redTeamPaste{Secret: s, Paste: s} }

	awsSecretValue := repeatCharset(alnum, 40)

	return map[string]redTeamPaste{
		"anthropic-api-key": paste("sk-ant-" + repeatCharset(alnum, 40)),
		"aws-access-key-id": paste("AKIA" + repeatCharset(upperAlnum, 16)),
		"aws-secret-access-key": {
			Secret: awsSecretValue,
			Paste:  "aws_secret_access_key=" + awsSecretValue,
		},
		"github-pat":      paste("ghp_" + repeatCharset(alnum, 36)),
		"gitlab-pat":      paste("glpat-" + repeatCharset(alnum, 20)),
		"slack-bot-token": paste("xoxb-" + repeatCharset(digits, 12) + "-" + repeatCharset(digits, 12) + "-" + repeatCharset(alnum, 24)),
		"slack-webhook-url": paste("https://hooks.slack.com/services/T" + repeatCharset(upperAlnum, 8) +
			"/B" + repeatCharset(upperAlnum, 8) + "/" + repeatCharset(alnum, 24)),
		"stripe-secret-key":        paste("sk_live_" + repeatCharset(alnum, 24)),
		"openai-api-key":           paste("sk-" + repeatCharset(alnum, 24)),
		"google-api-key":           paste("AIza" + repeatCharset(alnum, 35)),
		"twilio-api-key":           paste("SK" + repeatCharset(hex, 32)),
		"sendgrid-api-key":         paste("SG." + repeatCharset(alnum, 22) + "." + repeatCharset(alnum, 43)),
		"npm-access-token":         paste("npm_" + repeatCharset(alnum, 36)),
		"jwt":                      paste("eyJ" + repeatCharset(alnum, 10) + ".eyJ" + repeatCharset(alnum, 10) + "." + repeatCharset(alnum, 10)),
		"digitalocean-pat":         paste("dop_v1_" + repeatCharset(hex, 64)),
		"shopify-access-token":     paste("shpat_" + repeatCharset(hex, 32)),
		"square-access-token":      paste("sq0atp-" + repeatCharset(alnum, 22)),
		"telegram-bot-token":       paste(repeatCharset(digits, 9) + ":" + repeatCharset(alnum, 35)),
		"discord-bot-token":        paste("M" + repeatCharset(alnum, 23) + "." + repeatCharset(alnum, 6) + "." + repeatCharset(alnum, 27)),
		"huggingface-access-token": paste("hf_" + repeatCharset(alnum, 34)),
	}
}

func TestRedactionNFR07AgainstLocalSupabase(t *testing.T) {
	aliToken, err := passwordGrantToken(e2eAliEmail, e2ePassword)
	if err != nil {
		t.Skipf("local Supabase not reachable (run `supabase start` first): %v", err)
	}
	if _, err := (&http.Client{Timeout: 3 * time.Second}).Get(e2eAPIURL + "/functions/v1/"); err != nil {
		t.Skipf("local Supabase Edge Functions not served (run `supabase functions serve` too): %v", err)
	}

	ctx := context.Background()
	client := api.NewClient(e2eAPIURL, e2eAnonKey)
	root := t.TempDir()
	secrets := redTeamSecrets()

	origSessionID, err := pull.NewSessionID()
	if err != nil {
		t.Fatalf("generating session id: %v", err)
	}

	// No real git remote needed — this test only exercises upload →
	// Storage → rebuild, never pull.Place/launch, so a canonical-shaped
	// string (RegisterProject's Zod schema just needs "<host>/<path>"),
	// made unique per run via the session ID, is enough.
	canonicalRemote := fmt.Sprintf("example.com/redteam/secrets-test-%s", origSessionID)

	proj, err := client.RegisterProject(ctx, aliToken, "e2e-redaction-test", canonicalRemote)
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}
	t.Cleanup(func() {
		e2ePsql(t, fmt.Sprintf("delete from sessions where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from project_members where project_id = '%s';", proj.ID))
		e2ePsql(t, fmt.Sprintf("delete from projects where id = '%s';", proj.ID))
	})

	// One tool_result "pasted" by the red-team engineer per secret
	// shape, all within a single real transcript headed for upload.
	// Deliberately not labeled with the rule's own name (e.g. "found
	// an api-key: ...") — that phrasing isn't how a real paste reads,
	// and it also happens to look exactly like generic-api-key's own
	// "key: <value>" shape, which would then legitimately match too.
	var lines strings.Builder
	for ruleID, p := range secrets {
		line := map[string]any{
			"sessionId": origSessionID,
			"cwd":       root,
			"type":      "user",
			"message": map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":    "tool_result",
						"content": fmt.Sprintf("here's what I found in the config:\n%s", p.Paste),
					},
				},
			},
		}
		b, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("marshal transcript line for %s: %v", ruleID, err)
		}
		lines.Write(b)
		lines.WriteByte('\n')
	}

	transcriptPath := filepath.Join(root, origSessionID+".jsonl")
	if err := os.WriteFile(transcriptPath, []byte(lines.String()), 0o600); err != nil {
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

	// --- pull the bytes straight back out of real Storage, same code
	// path a real resume uses, and inspect exactly what reached it.
	manifest, err := client.GetManifest(ctx, aliToken, origSessionID)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}

	destDir := filepath.Join(root, "rebuilt")
	result, err := pull.Rebuild(ctx, client, manifest, destDir, pull.DefaultConfig())
	if err != nil {
		t.Fatalf("Rebuild() error = %v (a redaction bug that broke JSON/hash integrity would surface here)", err)
	}

	rebuilt, err := os.ReadFile(result.MainFile)
	if err != nil {
		t.Fatalf("reading rebuilt transcript: %v", err)
	}
	rebuiltStr := string(rebuilt)

	for ruleID, p := range secrets {
		if strings.Contains(rebuiltStr, p.Secret) {
			t.Errorf("raw secret for rule %q reached Storage: %q found in rebuilt transcript", ruleID, p.Secret)
		}
		placeholder := "[REDACTED:" + ruleID + "]"
		if !strings.Contains(rebuiltStr, placeholder) {
			t.Errorf("missing placeholder %q for rule %q — secret may not have been redacted at all", placeholder, ruleID)
		}
	}

	// Still valid JSONL — a resumed session's reader must be able to
	// parse every line with placeholders in place, not fail outright.
	for i, line := range strings.Split(strings.TrimRight(rebuiltStr, "\n"), "\n") {
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("rebuilt line %d is not valid JSON: %v\n%s", i, err, line)
		}
	}
}
