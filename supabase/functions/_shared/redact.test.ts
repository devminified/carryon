// CO-306's own parity check against internal/redact/corpus_test.go's
// corpus — same rule IDs, same example shapes (hand-copied, not
// imported across the Go/Deno boundary — see redact.ts's own header
// comment on why these two files are kept in sync by hand). If this
// test and internal/redact's ever drift on which rule IDs exist, that's
// the signal to update whichever file fell behind.

import { assertEquals, assertFalse } from "jsr:@std/assert@1.0.19";
import {
  applyDisabledRuleIDs,
  defaultRules,
  redactLine,
  redactText,
} from "./redact.ts";

function filler(charset: string, n: number, seed: number): string {
  let s = "";
  for (let i = 0; i < n; i++) {
    s += charset[(i + seed * 7 + 1) % charset.length];
  }
  return s;
}

const hexChars = "0123456789abcdef";
const alnumChars =
  "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
const upperDigits = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";

// secretExamples mirrors internal/redact/corpus_test.go's secretExamples
// — one generator per rule ID, 5 distinct fake values each.
function secretExamples(): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  const add = (id: string, gen: (seed: number) => string) => {
    out[id] = [0, 1, 2, 3, 4].map(gen);
  };

  add("supabase-service-key", (s) => "sb_secret_" + filler(alnumChars, 24, s));
  add("anthropic-api-key", (s) => "sk-ant-" + filler(alnumChars, 30, s));
  add(
    "postgres-connection-string",
    (s) =>
      `postgres://user${s}:pw0rd${
        filler(alnumChars, 6, s)
      }@db.example.com:5432/mydb`,
  );
  add(
    "private-key-block",
    (s) =>
      "-----BEGIN RSA PRIVATE KEY-----\\n" + filler(alnumChars, 40, s) +
      "\\n-----END RSA PRIVATE KEY-----",
  );
  add(
    "basic-auth-url",
    (s) =>
      `https://admin${s}:p@ssw0rd${
        filler(alnumChars, 4, s)
      }@internal.example.com/api`,
  );
  add("aws-access-key-id", (s) => "AKIA" + filler(upperDigits, 16, s));
  add(
    "aws-secret-access-key",
    (s) => "aws_secret_access_key: " + filler(alnumChars, 40, s),
  );
  add("github-pat", (s) => "ghp_" + filler(alnumChars, 36, s));
  add("github-oauth-token", (s) => "gho_" + filler(alnumChars, 36, s));
  add("github-app-token", (s) => "ghu_" + filler(alnumChars, 36, s));
  add("github-refresh-token", (s) => "ghr_" + filler(alnumChars, 76, s));
  add(
    "github-fine-grained-pat",
    (s) => "github_pat_" + filler(alnumChars, 82, s),
  );
  add("gitlab-pat", (s) => "glpat-" + filler(alnumChars, 20, s));
  add(
    "slack-bot-token",
    (s) => `xoxb-1234567890${s}-1234567890${s}-${filler(alnumChars, 24, s)}`,
  );
  add(
    "slack-user-token",
    (s) =>
      `xoxp-1234567890${s}-1234567890${s}-1234567890${s}-${
        filler(hexChars, 32, s)
      }`,
  );
  add(
    "slack-app-token",
    (s) =>
      `xapp-1-${filler(upperDigits, 10, s)}-123456789${s}-${
        filler(hexChars, 64, s)
      }`,
  );
  add(
    "slack-webhook-url",
    (s) =>
      `https://hooks.slack.com/services/T0${s}XXXXX/B0${s}YYYYY/${
        filler(alnumChars, 24, s)
      }`,
  );
  add("stripe-secret-key", (s) => "sk_live_" + filler(alnumChars, 28, s));
  add("stripe-restricted-key", (s) => "rk_live_" + filler(alnumChars, 28, s));
  add("openai-api-key", (s) => "sk-" + filler(alnumChars, 48, s));
  add("google-api-key", (s) => "AIza" + filler(alnumChars, 35, s));
  add(
    "google-oauth-client-secret",
    (s) => "GOCSPX-" + filler(alnumChars, 28, s),
  );
  add("twilio-api-key", (s) => "SK" + filler(hexChars, 32, s));
  add(
    "sendgrid-api-key",
    (s) => "SG." + filler(alnumChars, 22, s) + "." + filler(alnumChars, 43, s),
  );
  add("mailgun-api-key", (s) => "key-" + filler(hexChars, 32, s));
  add("mailchimp-api-key", (s) => filler(hexChars, 32, s) + "-us" + (s + 1));
  add("npm-access-token", (s) => "npm_" + filler(alnumChars, 36, s));
  add(
    "pypi-upload-token",
    (s) => "pypi-AgEIcHlwaS5vcmc" + filler(alnumChars, 55, s),
  );
  add(
    "jwt",
    (s) =>
      "eyJ" + filler(alnumChars, 20, s) + ".eyJ" + filler(alnumChars, 20, s) +
      "." + filler(alnumChars, 20, s),
  );
  add("digitalocean-pat", (s) => "dop_v1_" + filler(hexChars, 64, s));
  add("digitalocean-oauth-token", (s) => "doo_v1_" + filler(hexChars, 64, s));
  add("digitalocean-refresh-token", (s) => "dor_v1_" + filler(hexChars, 64, s));
  add("shopify-access-token", (s) => "shpat_" + filler(hexChars, 32, s));
  add("shopify-shared-secret", (s) => "shpss_" + filler(hexChars, 32, s));
  add("square-access-token", (s) => "sq0atp-" + filler(alnumChars, 22, s));
  add("square-oauth-secret", (s) => "sq0csp-" + filler(alnumChars, 43, s));
  add("docker-hub-pat", (s) => "dckr_pat_" + filler(alnumChars, 27, s));
  add("telegram-bot-token", (s) => `12345678${s}:${filler(alnumChars, 35, s)}`);
  add("age-secret-key", (s) => "AGE-SECRET-KEY-1" + filler(upperDigits, 58, s));
  add("vault-service-token", (s) => "hvs." + filler(alnumChars, 24, s));
  add("planetscale-token", (s) => "pscale_tkn_" + filler(alnumChars, 32, s));
  add("planetscale-password", (s) => "pscale_pw_" + filler(alnumChars, 32, s));
  add("huggingface-access-token", (s) => "hf_" + filler(alnumChars, 34, s));
  add(
    "discord-bot-token",
    (s) =>
      "M" + filler(alnumChars, 23, s) + "." + filler(alnumChars, 6, s) + "." +
      filler(alnumChars, 27, s),
  );
  add("generic-api-key", (s) => "api_key: " + filler(alnumChars, 32, s));

  return out;
}

Deno.test("every rule has at least one example, and every example matches its own pattern", () => {
  const rulesByID = new Map(defaultRules().map((r) => [r.id, r]));
  const examples = secretExamples();
  let total = 0;
  for (const [id, values] of Object.entries(examples)) {
    const rule = rulesByID.get(id);
    if (!rule) throw new Error(`corpus has examples for unknown rule id ${id}`);
    for (const v of values) {
      total++;
      rule.pattern.lastIndex = 0;
      if (!rule.pattern.test(v)) {
        throw new Error(
          `rule ${id} example ${
            JSON.stringify(v)
          } doesn't match its own pattern`,
        );
      }
    }
  }
  if (total < 200) {
    throw new Error(`corpus has ${total} examples, want at least 200`);
  }
  if (Object.keys(examples).length < 40) {
    throw new Error(
      `corpus covers ${
        Object.keys(examples).length
      } rule types, want at least 40`,
    );
  }
});

Deno.test("every rule type in internal/redact's own rule set is covered here too", () => {
  // internal/redact/rules.go's own 44 rule IDs — kept as a literal list
  // (not parsed out of the Go source) so a rule added to one side
  // without the other fails loudly here, not just silently drifts.
  const goRuleIDs = [
    "supabase-service-key",
    "anthropic-api-key",
    "postgres-connection-string",
    "private-key-block",
    "basic-auth-url",
    "aws-access-key-id",
    "aws-secret-access-key",
    "github-pat",
    "github-oauth-token",
    "github-app-token",
    "github-refresh-token",
    "github-fine-grained-pat",
    "gitlab-pat",
    "slack-bot-token",
    "slack-user-token",
    "slack-app-token",
    "slack-webhook-url",
    "stripe-secret-key",
    "stripe-restricted-key",
    "openai-api-key",
    "google-api-key",
    "google-oauth-client-secret",
    "twilio-api-key",
    "sendgrid-api-key",
    "mailgun-api-key",
    "mailchimp-api-key",
    "npm-access-token",
    "pypi-upload-token",
    "jwt",
    "digitalocean-pat",
    "digitalocean-oauth-token",
    "digitalocean-refresh-token",
    "shopify-access-token",
    "shopify-shared-secret",
    "square-access-token",
    "square-oauth-secret",
    "docker-hub-pat",
    "telegram-bot-token",
    "age-secret-key",
    "vault-service-token",
    "planetscale-token",
    "planetscale-password",
    "huggingface-access-token",
    "discord-bot-token",
    "generic-api-key",
  ];
  const tsRuleIDs = new Set(defaultRules().map((r) => r.id));
  for (const id of goRuleIDs) {
    if (!tsRuleIDs.has(id)) {
      throw new Error(`rule ${id} exists in internal/redact but not here`);
    }
  }
  assertEquals(
    tsRuleIDs.size,
    goRuleIDs.length,
    "rule count mismatch between redact.ts and internal/redact",
  );
});

// realisticLine mirrors internal/redact/corpus_test.go's own shapes —
// a secret pasted into a tool_result, assistant text, or a command —
// pasted here as a raw line the same way a stored chunk line looks.
function realisticLine(secret: string, shape: number): string {
  switch (shape % 4) {
    case 0:
      return `{"type":"user","message":{"content":[{"tool_use_id":"toolu_x","type":"tool_result","content":"export TOKEN=${secret}\\nDONE"}]}}`;
    case 1:
      return `{"type":"assistant","message":{"content":[{"type":"text","text":"Found it: ${secret} is the value configured."}]}}`;
    case 2:
      return `{"type":"user","message":{"content":[{"tool_use_id":"toolu_y","type":"tool_result","content":"$ echo $API_TOKEN\\n${secret}"}]}}`;
    default:
      return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_z","name":"Bash","input":{"command":"curl -H 'Authorization: Bearer ${secret}' https://api.example.com"}}]}}`;
  }
}

Deno.test("redactText catches every known secret in the corpus, fully, via the JSON-aware path", () => {
  const rules = defaultRules();
  const examples = secretExamples();
  const missed: string[] = [];

  for (const [id, values] of Object.entries(examples)) {
    values.forEach((secret, i) => {
      const line = realisticLine(secret, i);
      const { out, hits } = redactText(line, rules);
      if (hits.length === 0) {
        missed.push(`${id}[${i}]: no redaction applied`);
        return;
      }
      if (out.includes(secret)) {
        missed.push(`${id}[${i}]: secret still present in output: ${out}`);
      }
      JSON.parse(out); // still valid JSON after redaction — CO-306's own testing point
    });
  }

  if (missed.length > 0) {
    throw new Error(
      `${missed.length} known secrets were not fully redacted:\n${
        missed.join("\n")
      }`,
    );
  }
});

Deno.test("redactText preserves line count and redacts across multiple lines", () => {
  const rules = defaultRules();
  const text = [
    `{"a":1}`,
    `{"secret":"sb_secret_${"x".repeat(24)}"}`,
    `{"b":2}`,
  ].join("\n");
  const { out, hits } = redactText(text, rules);
  assertEquals(out.split("\n").length, 3);
  assertEquals(hits, ["supabase-service-key"]);
  assertFalse(out.includes("sb_secret_"));
});

Deno.test("applyDisabledRuleIDs drops exactly the named rules", () => {
  const rules = defaultRules();
  const filtered = applyDisabledRuleIDs(rules, ["jwt", "github-pat"]);
  const ids = new Set(filtered.map((r) => r.id));
  assertFalse(ids.has("jwt"));
  assertFalse(ids.has("github-pat"));
  assertEquals(filtered.length, rules.length - 2);
});

Deno.test("resolveOverlaps extends coverage instead of dropping a longer overlapping match (CO-301 parity)", () => {
  // generic-api-key's own "label-key: value" shape, overlapping a
  // longer run that's part of the real value — same scenario
  // internal/redact's own resolveOverlaps test exercises.
  const rules = defaultRules();
  const line = `{"t":"api_key: ${"A".repeat(40)}"}`;
  const { out, hits } = redactLine(line, rules);
  if (out.includes("A".repeat(20))) {
    throw new Error(`secret value survived redaction: ${out}`);
  }
  if (hits.length === 0) throw new Error("expected at least one hit");
});
