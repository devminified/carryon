package redact

import "regexp"

// Rule is one secret-detection pattern. Keywords, when non-empty, gate
// the (more expensive) regex check behind keywordMatcher's shared
// Aho-Corasick prefilter — gitleaks' own technique: nearly every rule
// here has a short, fixed literal prefix, so one combined scan finds
// every rule whose prefix is present, and only those rules' regexes
// ever run. Keywords must be given already lowercase — the matcher (and
// RedactLine) lowercases the subject once per string value and compares
// directly, never re-lowercasing per rule.
//
// An earlier version checked each rule's keywords with its own
// bytes.Contains call (45 separate scans per string value) instead of
// one shared automaton, as a deliberate simplification over building
// real Aho-Corasick — but that measurably couldn't clear the 20MB/s
// throughput requirement (internal/redact/throughput_test.go) once the
// *prefilter's own* overhead, not just the regexes it was gating,
// became the dominant cost. keywordMatcher (ahocorasick.go) replaced it
// once the simplification was shown not to hold up.
type Rule struct {
	ID      string
	Pattern *regexp.Regexp
	// Keywords are OR'd: any one present (found via the shared
	// Aho-Corasick scan) is enough to attempt the regex.
	Keywords []string
	// RequireAll are additional, cheap bytes.Contains checks (also
	// already-lowercase, also AND'd together) a rule can layer on top
	// of Keywords when a single literal isn't a tight enough gate on
	// its own — basic-auth-url's "://" alone matches almost any URL,
	// firing its full regex on a huge fraction of ordinary lines that
	// were never going to match (found via throughput_test.go's own
	// profiling); requiring "@" too (what the pattern itself actually
	// needs to match) cuts that down to only URLs that could plausibly
	// carry embedded credentials.
	RequireAll []string
}

// DefaultRules is Carryon's built-in rule set: the gitleaks v8 default
// ruleset's own regex *patterns*, hand-ported rather than depending on
// the gitleaks module itself (see this ticket's PR for why — importing
// github.com/zricethezav/gitleaks/v8/detect pulls in ~150 transitive
// packages built for its CLI, archive scanning included, none of which
// this laptop binary needs), plus the four rule families CO-301
// explicitly calls out: Supabase service keys, Anthropic keys,
// postgres connection strings, and private-key blocks.
//
// get-config (CO-301) can enable/disable rules by ID and extend the
// path denylist from the server without a new binary release — it
// deliberately cannot supply a new raw regex pattern to execute, so a
// compromised or careless server-side config change can narrow
// coverage but never inject code that runs against a developer's own
// transcript content.
func DefaultRules() []Rule {
	return []Rule{
		// --- Carryon-specific (CO-301's own required rules) ---
		{
			ID:       "supabase-service-key",
			Pattern:  regexp.MustCompile(`\bsb_secret_[A-Za-z0-9_-]{20,}`),
			Keywords: []string{"sb_secret_"},
		},
		{
			ID:       "anthropic-api-key",
			Pattern:  regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`),
			Keywords: []string{"sk-ant-"},
		},
		{
			ID:         "postgres-connection-string",
			Pattern:    regexp.MustCompile(`\bpostgres(?:ql)?://[^\s:/@"']+:[^\s/@"']+@[^\s/"']+`),
			Keywords:   []string{"postgres://", "postgresql://"},
			RequireAll: []string{"@"},
		},
		{
			ID:       "private-key-block",
			Pattern:  regexp.MustCompile(`(?s)-----BEGIN[ A-Z]*PRIVATE KEY-----.*?-----END[ A-Z]*PRIVATE KEY-----`),
			Keywords: []string{"-----begin"},
		},

		// --- generic credential-shaped URLs (gitleaks' "generic basic auth") ---
		{
			ID:         "basic-auth-url",
			Pattern:    regexp.MustCompile(`\bhttps?://[^\s:/@"']+:[^\s/@"']+@[^\s/"']+`),
			Keywords:   []string{"://"},
			RequireAll: []string{"@"},
		},

		// --- gitleaks default ruleset, hand-ported patterns ---
		{ID: "aws-access-key-id", Pattern: regexp.MustCompile(`\b(AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}\b`), Keywords: []string{"akia", "abia", "acca", "asia"}},
		{
			// The key name and value must be adjacent (an actual
			// assignment), not just both present somewhere in the same
			// string — a bare base64-ish 40-char run is otherwise far
			// too generic to flag on its own (hashes, encoded blobs).
			ID:       "aws-secret-access-key",
			Pattern:  regexp.MustCompile(`(?i)aws_secret_access_key\s*[:=]\s*['"]?([A-Za-z0-9/+=]{40})['"]?`),
			Keywords: []string{"aws_secret_access_key"},
		},
		{ID: "github-pat", Pattern: regexp.MustCompile(`\bghp_[0-9A-Za-z]{36}\b`), Keywords: []string{"ghp_"}},
		{ID: "github-oauth-token", Pattern: regexp.MustCompile(`\bgho_[0-9A-Za-z]{36}\b`), Keywords: []string{"gho_"}},
		{ID: "github-app-token", Pattern: regexp.MustCompile(`\b(ghu|ghs)_[0-9A-Za-z]{36}\b`), Keywords: []string{"ghu_", "ghs_"}},
		{ID: "github-refresh-token", Pattern: regexp.MustCompile(`\bghr_[0-9A-Za-z]{76}\b`), Keywords: []string{"ghr_"}},
		{ID: "github-fine-grained-pat", Pattern: regexp.MustCompile(`\bgithub_pat_[0-9A-Za-z_]{80,}\b`), Keywords: []string{"github_pat_"}},
		{ID: "gitlab-pat", Pattern: regexp.MustCompile(`\bglpat-[0-9A-Za-z_-]{20}\b`), Keywords: []string{"glpat-"}},
		{ID: "slack-bot-token", Pattern: regexp.MustCompile(`\bxoxb-[0-9]{10,13}-[0-9]{10,13}-[0-9A-Za-z]{24}\b`), Keywords: []string{"xoxb-"}},
		{ID: "slack-user-token", Pattern: regexp.MustCompile(`\bxoxp-[0-9]{10,13}-[0-9]{10,13}-[0-9]{10,13}-[0-9a-f]{32}\b`), Keywords: []string{"xoxp-"}},
		{ID: "slack-app-token", Pattern: regexp.MustCompile(`\bxapp-[0-9]-[A-Z0-9]+-[0-9]+-[a-f0-9]{64}\b`), Keywords: []string{"xapp-"}},
		{ID: "slack-webhook-url", Pattern: regexp.MustCompile(`\bhttps://hooks\.slack\.com/services/T[0-9A-Z]+/B[0-9A-Z]+/[0-9A-Za-z]+\b`), Keywords: []string{"hooks.slack.com"}},
		{ID: "stripe-secret-key", Pattern: regexp.MustCompile(`\bsk_(live|test)_[0-9A-Za-z]{24,}\b`), Keywords: []string{"sk_live_", "sk_test_"}},
		{ID: "stripe-restricted-key", Pattern: regexp.MustCompile(`\brk_(live|test)_[0-9A-Za-z]{24,}\b`), Keywords: []string{"rk_live_", "rk_test_"}},
		{ID: "openai-api-key", Pattern: regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9]{20,}\b`), Keywords: []string{"sk-"}},
		{ID: "google-api-key", Pattern: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`), Keywords: []string{"aiza"}},
		{ID: "google-oauth-client-secret", Pattern: regexp.MustCompile(`\bGOCSPX-[0-9A-Za-z_-]{28}\b`), Keywords: []string{"gocspx-"}},
		{ID: "twilio-api-key", Pattern: regexp.MustCompile(`\bSK[0-9a-fA-F]{32}\b`), Keywords: []string{"sk"}},
		{ID: "sendgrid-api-key", Pattern: regexp.MustCompile(`\bSG\.[0-9A-Za-z_-]{22}\.[0-9A-Za-z_-]{43}\b`), Keywords: []string{"sg."}},
		{ID: "mailgun-api-key", Pattern: regexp.MustCompile(`\bkey-[0-9a-f]{32}\b`), Keywords: []string{"key-"}},
		{ID: "mailchimp-api-key", Pattern: regexp.MustCompile(`\b[0-9a-f]{32}-us[0-9]{1,2}\b`), Keywords: []string{"-us"}},
		{ID: "npm-access-token", Pattern: regexp.MustCompile(`\bnpm_[0-9A-Za-z]{36}\b`), Keywords: []string{"npm_"}},
		{ID: "pypi-upload-token", Pattern: regexp.MustCompile(`\bpypi-AgEIcHlwaS5vcmc[0-9A-Za-z_-]{50,}\b`), Keywords: []string{"pypi-ageichlwas5vcmc"}},
		{ID: "jwt", Pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), Keywords: []string{"eyj"}},
		{ID: "digitalocean-pat", Pattern: regexp.MustCompile(`\bdop_v1_[0-9a-f]{64}\b`), Keywords: []string{"dop_v1_"}},
		{ID: "digitalocean-oauth-token", Pattern: regexp.MustCompile(`\bdoo_v1_[0-9a-f]{64}\b`), Keywords: []string{"doo_v1_"}},
		{ID: "digitalocean-refresh-token", Pattern: regexp.MustCompile(`\bdor_v1_[0-9a-f]{64}\b`), Keywords: []string{"dor_v1_"}},
		{ID: "shopify-access-token", Pattern: regexp.MustCompile(`\bshpat_[0-9a-f]{32}\b`), Keywords: []string{"shpat_"}},
		{ID: "shopify-shared-secret", Pattern: regexp.MustCompile(`\bshpss_[0-9a-f]{32}\b`), Keywords: []string{"shpss_"}},
		{ID: "square-access-token", Pattern: regexp.MustCompile(`\bsq0atp-[0-9A-Za-z_-]{22}\b`), Keywords: []string{"sq0atp-"}},
		{ID: "square-oauth-secret", Pattern: regexp.MustCompile(`\bsq0csp-[0-9A-Za-z_-]{43}\b`), Keywords: []string{"sq0csp-"}},
		{ID: "docker-hub-pat", Pattern: regexp.MustCompile(`\bdckr_pat_[0-9A-Za-z_-]{27}\b`), Keywords: []string{"dckr_pat_"}},
		// telegram-bot-token and discord-bot-token have no literal
		// prefix, only a colon/period at a fixed position — a loose
		// gate on that single character still matters: leaving these
		// two "ungated" (reasoning that 2 of 45 wouldn't move the
		// needle) was wrong in practice — they ran unconditionally on
		// every single string value with no other rule's regex
		// anywhere near that cost, found via throughput_test.go's own
		// profiling after the other 43 rules' gating was fixed.
		{ID: "telegram-bot-token", Pattern: regexp.MustCompile(`\b[0-9]{8,10}:[A-Za-z0-9_-]{35}\b`), Keywords: []string{":"}},
		{ID: "age-secret-key", Pattern: regexp.MustCompile(`\bAGE-SECRET-KEY-1[A-Z0-9]{58}\b`), Keywords: []string{"age-secret-key-1"}},
		{ID: "vault-service-token", Pattern: regexp.MustCompile(`\bhvs\.[A-Za-z0-9_-]{24,}\b`), Keywords: []string{"hvs."}},
		{ID: "planetscale-token", Pattern: regexp.MustCompile(`\bpscale_tkn_[A-Za-z0-9_]{32,}\b`), Keywords: []string{"pscale_tkn_"}},
		{ID: "planetscale-password", Pattern: regexp.MustCompile(`\bpscale_pw_[A-Za-z0-9_]{32,}\b`), Keywords: []string{"pscale_pw_"}},
		{ID: "huggingface-access-token", Pattern: regexp.MustCompile(`\bhf_[A-Za-z0-9]{34,}\b`), Keywords: []string{"hf_"}},
		{ID: "discord-bot-token", Pattern: regexp.MustCompile(`\b[MN][A-Za-z0-9_-]{23}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27}\b`), Keywords: []string{"."}},
		{
			// Same reasoning as aws-secret-access-key: the key name and
			// its value must appear as one adjacent assignment, not just
			// both somewhere in the same string.
			ID: "generic-api-key",
			Pattern: regexp.MustCompile(
				`(?i)(api[_-]?key|secret[_-]?key|access[_-]?token|access[_-]?key|client[_-]?secret)\s*[:=]\s*['"]?([A-Za-z0-9_-]{20,64})['"]?`,
			),
			Keywords: []string{"api_key", "api-key", "apikey", "secret_key", "secretkey", "access_token", "access_key", "client_secret"},
		},
	}
}
