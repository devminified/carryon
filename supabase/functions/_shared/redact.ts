// CO-306's own copy of internal/redact's rule *patterns* — the backstop
// scan `rescan` runs against bytes already in Storage, to catch a
// secret that slipped past the laptop's own redaction (I5) because the
// rule set has since grown, or a secret planted directly in Storage,
// bypassing the laptop entirely (this ticket's own testing point).
//
// This is a server-side port of internal/redact/rules.go's regex
// *patterns* only — same rule IDs, same patterns translated to
// JavaScript RegExp syntax (Go's `(?i)`/`(?s)` inline flags become the
// RegExp's own `i`/`s` flags; everything else is a direct
// character-for-character port). The laptop's Aho-Corasick keyword
// prefilter (ahocorasick.go) and the full JSON-structure-aware walk
// (walk.go, used for the laptop's denylist/tool_use correlation) are
// both deliberately NOT ported here:
//
//   - The prefilter exists only to clear the laptop's 20MB/s throughput
//     target on every keystroke's worth of new transcript bytes
//     (testing.md). rescan runs nightly over a bounded batch of
//     already-stored chunks — running every rule's regex unconditionally
//     is correctness-equivalent and not a real cost at this scale (no
//     speculative scale, CLAUDE.md).
//   - The denylist/tool_use-tool_result correlation exists to decide
//     whether to drop a whole tool_result BEFORE upload. By the time a
//     chunk reaches Storage, that decision already happened (or, per
//     this ticket's own testing point, was bypassed entirely) — there's
//     no tool_use/tool_result structure left to usefully re-derive
//     server-side, and nothing for a denylist to do to bytes that are
//     already stored. rescan's only job is rule matching.
//
// Matching runs directly against each raw stored line's bytes (not a
// JSON-string-aware walk): every pattern here only ever matches
// plain ASCII token characters (hex/base64/alnum runs, `://`, `@`,
// `-----BEGIN...-----END-----`) that never need JSON escaping, so a
// match's byte offsets within the raw JSON line are identical to its
// offsets within the decoded string value — verified against this
// file's own corpus test, which pastes every example into the same
// tool_result/text/command shapes internal/redact's own corpus_test.go
// uses.

export interface Rule {
  id: string;
  pattern: RegExp;
}

// defaultRules is a 1:1 port of internal/redact/rules.go's
// DefaultRules() — same IDs, same effective pattern. Keep these two
// lists in sync by hand whenever either changes; there's no shared
// source of truth across the Go/Deno boundary (no speculative
// code-generation pipeline for 44 regexes, CLAUDE.md's "no premature
// abstractions").
export function defaultRules(): Rule[] {
  return [
    { id: "supabase-service-key", pattern: /\bsb_secret_[A-Za-z0-9_-]{20,}/g },
    { id: "anthropic-api-key", pattern: /\bsk-ant-[A-Za-z0-9_-]{20,}/g },
    {
      id: "postgres-connection-string",
      pattern: /\bpostgres(?:ql)?:\/\/[^\s:/@"']+:[^\s/@"']+@[^\s/"']+/g,
    },
    {
      id: "private-key-block",
      pattern:
        /-----BEGIN[ A-Z]*PRIVATE KEY-----[\s\S]*?-----END[ A-Z]*PRIVATE KEY-----/g,
    },
    {
      id: "basic-auth-url",
      pattern: /\bhttps?:\/\/[^\s:/@"']+:[^\s/@"']+@[^\s/"']+/g,
    },
    {
      id: "aws-access-key-id",
      pattern: /\b(AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}\b/g,
    },
    {
      id: "aws-secret-access-key",
      pattern:
        /aws_secret_access_key\s*[:=]\s*['"]?([A-Za-z0-9/+=]{40})['"]?/gi,
    },
    { id: "github-pat", pattern: /\bghp_[0-9A-Za-z]{36}\b/g },
    { id: "github-oauth-token", pattern: /\bgho_[0-9A-Za-z]{36}\b/g },
    { id: "github-app-token", pattern: /\b(ghu|ghs)_[0-9A-Za-z]{36}\b/g },
    { id: "github-refresh-token", pattern: /\bghr_[0-9A-Za-z]{76}\b/g },
    {
      id: "github-fine-grained-pat",
      pattern: /\bgithub_pat_[0-9A-Za-z_]{80,}\b/g,
    },
    { id: "gitlab-pat", pattern: /\bglpat-[0-9A-Za-z_-]{20}\b/g },
    {
      id: "slack-bot-token",
      pattern: /\bxoxb-[0-9]{10,13}-[0-9]{10,13}-[0-9A-Za-z]{24}\b/g,
    },
    {
      id: "slack-user-token",
      pattern: /\bxoxp-[0-9]{10,13}-[0-9]{10,13}-[0-9]{10,13}-[0-9a-f]{32}\b/g,
    },
    {
      id: "slack-app-token",
      pattern: /\bxapp-[0-9]-[A-Z0-9]+-[0-9]+-[a-f0-9]{64}\b/g,
    },
    {
      id: "slack-webhook-url",
      pattern:
        /\bhttps:\/\/hooks\.slack\.com\/services\/T[0-9A-Z]+\/B[0-9A-Z]+\/[0-9A-Za-z]+\b/g,
    },
    {
      id: "stripe-secret-key",
      pattern: /\bsk_(live|test)_[0-9A-Za-z]{24,}\b/g,
    },
    {
      id: "stripe-restricted-key",
      pattern: /\brk_(live|test)_[0-9A-Za-z]{24,}\b/g,
    },
    { id: "openai-api-key", pattern: /\bsk-(proj-)?[A-Za-z0-9]{20,}\b/g },
    { id: "google-api-key", pattern: /\bAIza[0-9A-Za-z_-]{35}\b/g },
    {
      id: "google-oauth-client-secret",
      pattern: /\bGOCSPX-[0-9A-Za-z_-]{28}\b/g,
    },
    { id: "twilio-api-key", pattern: /\bSK[0-9a-fA-F]{32}\b/g },
    {
      id: "sendgrid-api-key",
      pattern: /\bSG\.[0-9A-Za-z_-]{22}\.[0-9A-Za-z_-]{43}\b/g,
    },
    { id: "mailgun-api-key", pattern: /\bkey-[0-9a-f]{32}\b/g },
    { id: "mailchimp-api-key", pattern: /\b[0-9a-f]{32}-us[0-9]{1,2}\b/g },
    { id: "npm-access-token", pattern: /\bnpm_[0-9A-Za-z]{36}\b/g },
    {
      id: "pypi-upload-token",
      pattern: /\bpypi-AgEIcHlwaS5vcmc[0-9A-Za-z_-]{50,}\b/g,
    },
    {
      id: "jwt",
      pattern:
        /\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/g,
    },
    { id: "digitalocean-pat", pattern: /\bdop_v1_[0-9a-f]{64}\b/g },
    { id: "digitalocean-oauth-token", pattern: /\bdoo_v1_[0-9a-f]{64}\b/g },
    { id: "digitalocean-refresh-token", pattern: /\bdor_v1_[0-9a-f]{64}\b/g },
    { id: "shopify-access-token", pattern: /\bshpat_[0-9a-f]{32}\b/g },
    { id: "shopify-shared-secret", pattern: /\bshpss_[0-9a-f]{32}\b/g },
    { id: "square-access-token", pattern: /\bsq0atp-[0-9A-Za-z_-]{22}\b/g },
    { id: "square-oauth-secret", pattern: /\bsq0csp-[0-9A-Za-z_-]{43}\b/g },
    { id: "docker-hub-pat", pattern: /\bdckr_pat_[0-9A-Za-z_-]{27}\b/g },
    { id: "telegram-bot-token", pattern: /\b[0-9]{8,10}:[A-Za-z0-9_-]{35}\b/g },
    { id: "age-secret-key", pattern: /\bAGE-SECRET-KEY-1[A-Z0-9]{58}\b/g },
    { id: "vault-service-token", pattern: /\bhvs\.[A-Za-z0-9_-]{24,}\b/g },
    { id: "planetscale-token", pattern: /\bpscale_tkn_[A-Za-z0-9_]{32,}\b/g },
    { id: "planetscale-password", pattern: /\bpscale_pw_[A-Za-z0-9_]{32,}\b/g },
    { id: "huggingface-access-token", pattern: /\bhf_[A-Za-z0-9]{34,}\b/g },
    {
      id: "discord-bot-token",
      pattern:
        /\b[MN][A-Za-z0-9_-]{23}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27}\b/g,
    },
    {
      id: "generic-api-key",
      pattern:
        /(api[_-]?key|secret[_-]?key|access[_-]?token|access[_-]?key|client[_-]?secret)\s*[:=]\s*['"]?([A-Za-z0-9_-]{20,64})['"]?/gi,
    },
  ];
}

// applyDisabledRuleIDs mirrors internal/redact.ApplyServerConfig's one
// relevant half for rescan's purpose (there's no denylist concept once
// bytes are already in Storage — see this file's own header comment):
// drop any rule whose ID the server's redaction_config has disabled, so
// rescan never flags something an admin deliberately exempted (the
// laptop wouldn't have flagged it either).
export function applyDisabledRuleIDs(
  rules: Rule[],
  disabledRuleIDs: string[],
): Rule[] {
  const disabled = new Set(disabledRuleIDs);
  return rules.filter((r) => !disabled.has(r.id));
}

interface ruleSpan {
  start: number;
  end: number;
  id: string;
}

// resolveOverlaps is internal/redact.resolveOverlaps, ported directly —
// same reasoning, same behavior: extend a kept span's end to cover any
// later span that starts before it ends, never drop the later span
// outright (the exact bug CO-301 found and fixed server-side too, so
// rescan can't reintroduce it independently).
function resolveOverlaps(spans: ruleSpan[]): ruleSpan[] {
  if (spans.length < 2) return spans;
  const sorted = [...spans].sort((
    a,
    b,
  ) => (a.start !== b.start ? a.start - b.start : b.end - a.end));
  const kept: ruleSpan[] = [sorted[0]];
  for (const s of sorted.slice(1)) {
    const last = kept[kept.length - 1];
    if (s.start < last.end) {
      if (s.end > last.end) last.end = s.end;
      continue;
    }
    kept.push(s);
  }
  return kept;
}

// redactLine runs every rule against one line's raw bytes (as a JS
// string — Deno strings are UTF-16, but every pattern here only ever
// matches single-byte ASCII runs, so indices stay consistent with the
// original line's own byte offsets for every match this ruleset can
// produce) and splices in `[REDACTED:<rule-id>]` at each resolved span,
// the same placeholder format internal/redact uses — a resumed laptop's
// own reader never has to special-case which side produced it.
export function redactLine(
  line: string,
  rules: Rule[],
): { out: string; hits: string[] } {
  const spans: ruleSpan[] = [];
  for (const rule of rules) {
    rule.pattern.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = rule.pattern.exec(line)) !== null) {
      spans.push({ start: m.index, end: m.index + m[0].length, id: rule.id });
      if (m[0].length === 0) rule.pattern.lastIndex++; // guard against a zero-width match looping forever
    }
  }
  if (spans.length === 0) return { out: line, hits: [] };

  const resolved = resolveOverlaps(spans);
  let out = "";
  let cursor = 0;
  const hits: string[] = [];
  for (const s of resolved) {
    out += line.slice(cursor, s.start) + `[REDACTED:${s.id}]`;
    hits.push(s.id);
    cursor = s.end;
  }
  out += line.slice(cursor);
  return { out, hits };
}

// redactJSONLine is this file's answer to internal/redact/walk.go's
// JSON-aware walk — much simpler here because JSON.parse already hands
// back fully *decoded* string values (a `\n` escape sequence becomes a
// real newline byte, a `\"` becomes a real quote), where Go's own
// walker has to track that decoding itself token by token. That
// decoding isn't cosmetic: a `\b`-anchored rule (most of them) fails to
// match right at a boundary between two escaped characters that are
// both "word" bytes in their *raw* two-character escape form (e.g. the
// literal bytes `\`, `n` right before a secret that starts with a
// letter) even though the *decoded* single newline byte correctly
// creates a word boundary there — found by this file's own corpus
// test failing on exactly that shape, the same shape
// internal/redact/corpus_test.go itself pastes a secret into.
//
// Unlike the laptop, this doesn't need byte-position-preserving splice
// logic to keep the rest of a line untouched — CO-306's own testing
// point is just "the rewritten chunk still rebuilds into valid JSON",
// not byte-identical formatting, so parsing the whole value and
// re-serializing with JSON.stringify is a deliberate, much simpler
// substitute for walk.go's explicit-stack token walker (no speculative
// scale, CLAUDE.md) — ok is false if line isn't valid JSON, the same
// signal internal/redact.RedactLine's viaJSON gives, with the same raw-
// bytes fallback.
function redactJSONLine(
  line: string,
  rules: Rule[],
): { out: string; hits: string[]; ok: boolean } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(line);
  } catch {
    return { out: line, hits: [], ok: false };
  }

  const hits: string[] = [];
  const walk = (value: unknown): unknown => {
    if (typeof value === "string") {
      const r = redactLine(value, rules);
      hits.push(...r.hits);
      return r.out;
    }
    if (Array.isArray(value)) return value.map(walk);
    if (value && typeof value === "object") {
      const out: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
        out[k] = walk(v);
      }
      return out;
    }
    return value;
  };

  return { out: JSON.stringify(walk(parsed)), hits, ok: true };
}

// redactText runs redactJSONLine over every line of a chunk's full
// decompressed text (falling back to raw-line matching for a line
// that isn't valid JSON — I5 still has to hold either way), preserving
// line count (split/joined on "\n" only — chunk bytes are always
// complete JSONL lines, per internal/reader's own contract the laptop
// already upholds).
export function redactText(
  text: string,
  rules: Rule[],
): { out: string; hits: string[] } {
  const lines = text.split("\n");
  const allHits: string[] = [];
  const outLines = lines.map((line) => {
    if (line === "") return line; // the trailing empty "line" after a final \n
    const r = redactJSONLine(line, rules);
    if (r.ok) {
      allHits.push(...r.hits);
      return r.out;
    }
    const raw = redactLine(line, rules);
    allHits.push(...raw.hits);
    return raw.out;
  });
  return { out: outLines.join("\n"), hits: allHits };
}
