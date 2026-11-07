package redact

import (
	"fmt"
	"strings"
	"testing"
)

// filler returns n characters built by cycling charset, varying with
// seed so five calls for the same rule produce five distinct (but
// still pattern-matching) fake values — this is a test fixture
// generator, not a source of real entropy.
func filler(charset string, n int, seed int) string {
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		b.WriteByte(charset[(i+seed*7+1)%len(charset)])
	}
	return b.String()
}

const (
	hexChars    = "0123456789abcdef"
	alnumChars  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	upperDigits = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// secretExamples returns 5 distinct fake values for every rule
// DefaultRules() defines, each one constructed to actually match that
// rule's own pattern (verified by the test below, not assumed) — the
// "200 known secrets across 40 types" corpus CO-301 calls for.
func secretExamples() map[string][]string {
	out := map[string][]string{}
	add := func(id string, gen func(seed int) string) {
		for i := 0; i < 5; i++ {
			out[id] = append(out[id], gen(i))
		}
	}

	add("supabase-service-key", func(s int) string { return "sb_secret_" + filler(alnumChars, 24, s) })
	add("anthropic-api-key", func(s int) string { return "sk-ant-" + filler(alnumChars, 30, s) })
	add("postgres-connection-string", func(s int) string {
		return fmt.Sprintf("postgres://user%d:pw0rd%s@db.example.com:5432/mydb", s, filler(alnumChars, 6, s))
	})
	add("private-key-block", func(s int) string {
		return "-----BEGIN RSA PRIVATE KEY-----\n" + filler(alnumChars, 40, s) + "\n-----END RSA PRIVATE KEY-----"
	})
	add("basic-auth-url", func(s int) string {
		return fmt.Sprintf("https://admin%d:p@ssw0rd%s@internal.example.com/api", s, filler(alnumChars, 4, s))
	})
	add("aws-access-key-id", func(s int) string { return "AKIA" + filler(upperDigits, 16, s) })
	add("aws-secret-access-key", func(s int) string {
		return "aws_secret_access_key: " + filler(alnumChars, 40, s)
	})
	add("github-pat", func(s int) string { return "ghp_" + filler(alnumChars, 36, s) })
	add("github-oauth-token", func(s int) string { return "gho_" + filler(alnumChars, 36, s) })
	add("github-app-token", func(s int) string { return "ghu_" + filler(alnumChars, 36, s) })
	add("github-refresh-token", func(s int) string { return "ghr_" + filler(alnumChars, 76, s) })
	add("github-fine-grained-pat", func(s int) string { return "github_pat_" + filler(alnumChars, 82, s) })
	add("gitlab-pat", func(s int) string { return "glpat-" + filler(alnumChars, 20, s) })
	add("slack-bot-token", func(s int) string {
		return fmt.Sprintf("xoxb-1234567890%d-1234567890%d-%s", s, s, filler(alnumChars, 24, s))
	})
	add("slack-user-token", func(s int) string {
		return fmt.Sprintf("xoxp-1234567890%d-1234567890%d-1234567890%d-%s", s, s, s, filler(hexChars, 32, s))
	})
	add("slack-app-token", func(s int) string {
		return fmt.Sprintf("xapp-1-%s-123456789%d-%s", filler(upperDigits, 10, s), s, filler(hexChars, 64, s))
	})
	add("slack-webhook-url", func(s int) string {
		return fmt.Sprintf("https://hooks.slack.com/services/T0%dXXXXX/B0%dYYYYY/%s", s, s, filler(alnumChars, 24, s))
	})
	add("stripe-secret-key", func(s int) string { return "sk_live_" + filler(alnumChars, 28, s) })
	add("stripe-restricted-key", func(s int) string { return "rk_live_" + filler(alnumChars, 28, s) })
	add("openai-api-key", func(s int) string { return "sk-" + filler(alnumChars, 48, s) })
	add("google-api-key", func(s int) string { return "AIza" + filler(alnumChars, 35, s) })
	add("google-oauth-client-secret", func(s int) string { return "GOCSPX-" + filler(alnumChars, 28, s) })
	add("twilio-api-key", func(s int) string { return "SK" + filler(hexChars, 32, s) })
	add("sendgrid-api-key", func(s int) string {
		return "SG." + filler(alnumChars, 22, s) + "." + filler(alnumChars, 43, s)
	})
	add("mailgun-api-key", func(s int) string { return "key-" + filler(hexChars, 32, s) })
	add("mailchimp-api-key", func(s int) string { return filler(hexChars, 32, s) + "-us" + fmt.Sprint(s+1) })
	add("npm-access-token", func(s int) string { return "npm_" + filler(alnumChars, 36, s) })
	add("pypi-upload-token", func(s int) string { return "pypi-AgEIcHlwaS5vcmc" + filler(alnumChars, 55, s) })
	add("jwt", func(s int) string {
		return "eyJ" + filler(alnumChars, 20, s) + ".eyJ" + filler(alnumChars, 20, s) + "." + filler(alnumChars, 20, s)
	})
	add("digitalocean-pat", func(s int) string { return "dop_v1_" + filler(hexChars, 64, s) })
	add("digitalocean-oauth-token", func(s int) string { return "doo_v1_" + filler(hexChars, 64, s) })
	add("digitalocean-refresh-token", func(s int) string { return "dor_v1_" + filler(hexChars, 64, s) })
	add("shopify-access-token", func(s int) string { return "shpat_" + filler(hexChars, 32, s) })
	add("shopify-shared-secret", func(s int) string { return "shpss_" + filler(hexChars, 32, s) })
	add("square-access-token", func(s int) string { return "sq0atp-" + filler(alnumChars, 22, s) })
	add("square-oauth-secret", func(s int) string { return "sq0csp-" + filler(alnumChars, 43, s) })
	add("docker-hub-pat", func(s int) string { return "dckr_pat_" + filler(alnumChars, 27, s) })
	add("telegram-bot-token", func(s int) string {
		return fmt.Sprintf("12345678%d:%s", s, filler(alnumChars, 35, s))
	})
	add("age-secret-key", func(s int) string { return "AGE-SECRET-KEY-1" + filler(upperDigits, 58, s) })
	add("vault-service-token", func(s int) string { return "hvs." + filler(alnumChars, 24, s) })
	add("planetscale-token", func(s int) string { return "pscale_tkn_" + filler(alnumChars, 32, s) })
	add("planetscale-password", func(s int) string { return "pscale_pw_" + filler(alnumChars, 32, s) })
	add("huggingface-access-token", func(s int) string { return "hf_" + filler(alnumChars, 34, s) })
	add("discord-bot-token", func(s int) string {
		return "M" + filler(alnumChars, 23, s) + "." + filler(alnumChars, 6, s) + "." + filler(alnumChars, 27, s)
	})
	add("generic-api-key", func(s int) string { return "api_key: " + filler(alnumChars, 32, s) })

	return out
}

// realisticLine wraps a raw secret value in one of several shapes a
// real transcript line might carry it in — a tool_result reading a
// config/env file, an assistant message quoting a value back, or a
// Bash command's output — so the corpus exercises the JSON-aware walk
// across different structural positions, not just one.
func realisticLine(secret string, shape int) string {
	switch shape % 4 {
	case 0:
		return fmt.Sprintf(`{"type":"user","message":{"content":[{"tool_use_id":"toolu_x","type":"tool_result","content":"export TOKEN=%s\nDONE"}]}}`, jsonEscape(secret))
	case 1:
		return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":"Found it: %s is the value configured."}]}}`, jsonEscape(secret))
	case 2:
		return fmt.Sprintf(`{"type":"user","message":{"content":[{"tool_use_id":"toolu_y","type":"tool_result","content":"$ echo $API_TOKEN\n%s"}]}}`, jsonEscape(secret))
	default:
		return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_z","name":"Bash","input":{"command":"curl -H 'Authorization: Bearer %s' https://api.example.com"}}]}}`, jsonEscape(secret))
	}
}

func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// TestCorpusEveryRuleExampleMatchesItsOwnPattern is a sanity check on
// the fixtures themselves, independent of the Redactor: every generated
// example must actually match the rule it's filed under, so a failure
// in the real detection test below can't be blamed on a bad fixture.
func TestCorpusEveryRuleExampleMatchesItsOwnPattern(t *testing.T) {
	rulesByID := map[string]Rule{}
	for _, rule := range DefaultRules() {
		rulesByID[rule.ID] = rule
	}
	examples := secretExamples()
	total := 0
	for id, values := range examples {
		rule, ok := rulesByID[id]
		if !ok {
			t.Fatalf("corpus has examples for unknown rule id %q", id)
		}
		for i, v := range values {
			total++
			if !rule.Pattern.MatchString(v) {
				t.Errorf("rule %q example[%d] = %q doesn't match its own pattern", id, i, v)
			}
		}
	}
	if total < 200 {
		t.Errorf("corpus has %d secret examples, want at least 200", total)
	}
	if len(examples) < 40 {
		t.Errorf("corpus covers %d rule types, want at least 40", len(examples))
	}
}

// TestCorpusCatchesEveryKnownSecret is CO-301's literal testing point:
// a corpus of known secrets across many types, 100% caught.
func TestCorpusCatchesEveryKnownSecret(t *testing.T) {
	examples := secretExamples()
	var missed []string

	for id, values := range examples {
		for i, secret := range values {
			r := NewRedactor(DefaultConfig())
			line := realisticLine(secret, i)
			out, n, ok := r.RedactLine([]byte(line))
			if !ok {
				missed = append(missed, fmt.Sprintf("%s[%d]: line wasn't valid JSON: %s", id, i, line))
				continue
			}
			if n == 0 {
				missed = append(missed, fmt.Sprintf("%s[%d]: no redaction applied", id, i))
				continue
			}
			if strings.Contains(string(out), secret) {
				missed = append(missed, fmt.Sprintf("%s[%d]: secret still present in output: %s", id, i, out))
			}
		}
	}

	if len(missed) > 0 {
		t.Errorf("%d/%d known secrets were not fully redacted:\n%s",
			len(missed), countExamples(examples), strings.Join(missed, "\n"))
	}
}

func countExamples(m map[string][]string) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}
