// Package redact implements CO-301: scanning every new transcript line
// for known secret shapes before it's compressed and uploaded (I5 — no
// secret leaves the laptop unredacted, for every session, every time),
// plus a path denylist that replaces a denylisted file's entire tool
// result rather than trying to redact its content piecemeal.
//
// Unlike internal/pull's path/session-ID rewriters (CO-207/CO-208),
// which leave a line byte-for-byte unchanged if it isn't valid JSON or
// nests too deep — a correctness/UX concern, not a security one — this
// package never does that: I5 is a hard invariant, so a line this
// package can't parse as JSON still gets every rule's regex run
// directly against its raw bytes. The JSON-aware walk is what finds
// secrets inside normally-escaped string values *and* what finds a
// denylisted file's tool_result (which needs real structure — matching
// a tool_use's id to a later tool_result's tool_use_id); the raw-bytes
// fallback is the backstop for a line that structure can't help with.
package redact

import (
	"bytes"
	"sort"
)

// maxJSONDepth matches internal/pull's own limit (the SDD's "iterative
// depth-first traversal with an explicit stack, depth limit 512").
const maxJSONDepth = 512

// Config is a Redactor's rule set and path denylist. DefaultConfig's
// values are used until get-config (CO-301) supplies an override.
type Config struct {
	Rules []Rule
	// Denylist is a set of filepath.Match glob patterns checked against
	// a Read tool call's file_path basename — a match replaces that
	// call's entire tool_result, never just part of it.
	Denylist []string
}

// DefaultConfig returns Carryon's built-in rules and path denylist.
func DefaultConfig() Config {
	return Config{
		Rules:    DefaultRules(),
		Denylist: []string{".env*", "*.pem", "*.key", "id_rsa*", "*.p12", "credentials*", "secrets.*"},
	}
}

// ApplyServerConfig narrows or extends base per get-config's (CO-301)
// response: dropping any rule whose ID is in disabledRuleIDs, and
// appending denylistAdditions' extra globs. There's deliberately no way
// to reach this from a raw server-supplied regex — see Rule's doc
// comment for why that boundary matters — so this can only ever shrink
// rule coverage or grow the denylist, never inject new matching logic.
func ApplyServerConfig(base Config, disabledRuleIDs, denylistAdditions []string) Config {
	disabled := make(map[string]bool, len(disabledRuleIDs))
	for _, id := range disabledRuleIDs {
		disabled[id] = true
	}

	out := Config{
		Rules:    make([]Rule, 0, len(base.Rules)),
		Denylist: make([]string, 0, len(base.Denylist)+len(denylistAdditions)),
	}
	for _, rule := range base.Rules {
		if !disabled[rule.ID] {
			out.Rules = append(out.Rules, rule)
		}
	}
	out.Denylist = append(out.Denylist, base.Denylist...)
	out.Denylist = append(out.Denylist, denylistAdditions...)
	return out
}

// Redactor applies a Config to a session's transcript lines, in order.
// Not safe for concurrent use — callers needing the correlation between
// a tool_use and its later tool_result must feed lines through one
// Redactor sequentially, same as internal/reader's own sequential
// tailing.
type Redactor struct {
	cfg Config
	// pending maps an in-flight tool_use's id to the denylisted file's
	// basename it read, from the moment that tool_use is seen until its
	// tool_result is (a tool call can finish well after the agent's own
	// poll tick that uploaded the tool_use line, so this must survive
	// across RedactLine calls — see Pending/SetPending for how a caller
	// persists it across a restart, the same shape as internal/state's
	// own per-stream pointer).
	pending map[string]string
	// matcher is built once from cfg.Rules' keywords, not per line or
	// per string value — see keywordMatcher's own doc comment for why
	// that once-per-call cost mattered enough to measure.
	matcher *keywordMatcher
	// matchedScratch is reused across every matchRuleSpans/redactRaw/
	// countRawMatches call (reset to all-false first) instead of
	// allocating a fresh []bool per string value scanned — Redactor is
	// already documented as sequential-only, so reuse is safe.
	matchedScratch []bool
	// lowerScratch is the same idea applied to the keyword gate's
	// lowercased copy of each string value — bytes.ToLower always
	// allocates a fresh slice; lowerInto (below) fills this reused one
	// instead, regrowing only when a longer value needs more room.
	lowerScratch []byte
	// escapeScratch is neutralizeEscapes' own reused buffer, same
	// reasoning as lowerScratch.
	escapeScratch []byte
}

// NewRedactor creates a Redactor for cfg, with no in-flight tool_use
// pending yet.
func NewRedactor(cfg Config) *Redactor {
	return &Redactor{
		cfg: cfg, pending: map[string]string{},
		matcher:        newKeywordMatcher(cfg.Rules),
		matchedScratch: make([]bool, len(cfg.Rules)),
	}
}

// Pending returns a copy of the current in-flight tool_use -> denylisted
// basename map, for a caller to persist (see internal/state's pointer
// persistence for the pattern this mirrors).
func (r *Redactor) Pending() map[string]string {
	out := make(map[string]string, len(r.pending))
	for k, v := range r.pending {
		out[k] = v
	}
	return out
}

// SetPending restores a previously-persisted pending map (e.g. after an
// agent restart) before processing more lines.
func (r *Redactor) SetPending(pending map[string]string) {
	r.pending = make(map[string]string, len(pending))
	for k, v := range pending {
		r.pending[k] = v
	}
}

// lowerInto returns src lowercased, reusing r.lowerScratch instead of
// allocating — the slice returned is only valid until the next call.
func (r *Redactor) lowerInto(src []byte) []byte {
	if cap(r.lowerScratch) < len(src) {
		r.lowerScratch = make([]byte, len(src))
	}
	dst := r.lowerScratch[:len(src)]
	for i, b := range src {
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		dst[i] = b
	}
	return dst
}

// matchKeywords resets r.matchedScratch and runs the shared
// Aho-Corasick scan over lower (already lowercased by the caller),
// returning the reused buffer — valid only until the next call.
func (r *Redactor) matchKeywords(lower []byte) []bool {
	for i := range r.matchedScratch {
		r.matchedScratch[i] = false
	}
	r.matcher.matchedRules(lower, r.matchedScratch)
	return r.matchedScratch
}

// RedactBytes redacts data in place in memory, line by line — the only
// shape actually needed: internal/upload reads a stream's new bytes via
// internal/reader before they're ever written to a temp file, so
// redaction has to happen on exactly that in-memory slice, before the
// 8MB-chunk split, since reader.Read's own contract guarantees "no line
// split across two reads" but says nothing about where a later
// byte-range split falls. There's deliberately no file-to-file
// counterpart — nothing in the real pipeline ever has a transcript file
// to redact in place; don't add one speculatively. data must consist of
// complete lines (reader.Read's own contract) — a trailing partial line
// is kept unchanged rather than mis-redacted.
func (r *Redactor) RedactBytes(data []byte) (out []byte, redactions int) {
	var buf bytes.Buffer
	buf.Grow(len(data))

	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		var line []byte
		if nl < 0 {
			line = data
			data = nil
		} else {
			line = data[:nl]
			data = data[nl+1:]
		}

		redacted, n, _ := r.RedactLine(line)
		redactions += n
		buf.Write(redacted)
		if nl >= 0 {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), redactions
}

// RedactLine redacts one JSON line (no trailing newline). viaJSON is
// true if the line was valid JSON and the structure-aware walk ran
// (finding secrets in string values, and resolving any denylisted
// tool_result); false means the raw-bytes rule-matching fallback ran
// instead (I5 still holds either way — see the package doc comment).
func (r *Redactor) RedactLine(line []byte) (out []byte, redactions int, viaJSON bool) {
	out, redactions, ok := r.redactJSON(line)
	if ok {
		return out, redactions, true
	}
	return r.redactRaw(line), r.countRawMatches(line), false
}

func (r *Redactor) countRawMatches(line []byte) int {
	lower := r.lowerInto(line)
	matched := r.matchKeywords(lower)
	n := 0
	for i, rule := range r.cfg.Rules {
		if !ruleGatePassed(rule, matched[i], lower) {
			continue
		}
		n += len(rule.Pattern.FindAllIndex(line, -1))
	}
	return n
}

// ruleGatePassed decides whether rule's regex is worth attempting:
// Keywords gate via the shared Aho-Corasick scan (matched, computed
// once for all rules — see keywordMatcher), and RequireAll layers on
// cheap additional bytes.Contains checks for a rule whose Keywords
// alone aren't a tight enough filter (see basic-auth-url).
func ruleGatePassed(rule Rule, matched bool, lower []byte) bool {
	if len(rule.Keywords) > 0 && !matched {
		return false
	}
	for _, kw := range rule.RequireAll {
		if !bytes.Contains(lower, []byte(kw)) {
			return false
		}
	}
	return true
}

// ruleSpan is one rule match's byte range within a line, plus the rule
// ID to splice in as its replacement.
type ruleSpan struct {
	start, end int
	id         string
}

// resolveOverlaps sorts spans by start and merges any span that
// overlaps an already-kept one into that kept span, extending its end
// rather than dropping the overlapping span outright — two rules can
// legitimately match overlapping text (e.g. generic-api-key's
// "key: <label>" match ending right where a more specific rule's own
// match, for the real value past the label, begins), and the earlier
// match ending before the real secret does is exactly the case where
// simply discarding the later, longer match would leave that secret's
// bytes outside every kept span and so never get redacted at all (I5).
// Extending coverage instead guarantees every byte any rule matched
// ends up inside exactly one kept span, whichever rule's ID a given
// kept span is reported under — correctness here is about coverage,
// not about which rule's name ends up in the placeholder.
func resolveOverlaps(spans []ruleSpan) []ruleSpan {
	if len(spans) < 2 {
		return spans
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end // longer match first at the same start
	})
	kept := spans[:1]
	for _, s := range spans[1:] {
		last := &kept[len(kept)-1]
		if s.start < last.end {
			if s.end > last.end {
				last.end = s.end // extend coverage — never shrink it back down
			}
			continue
		}
		kept = append(kept, s)
	}
	return kept
}

// redactRaw applies every rule directly against line's raw bytes,
// splicing in [REDACTED:<rule-id>] at each match — used only when line
// isn't valid JSON, so there's no string-value structure to preserve.
func (r *Redactor) redactRaw(line []byte) []byte {
	lower := r.lowerInto(line)
	matched := r.matchKeywords(lower)
	var spans []ruleSpan
	for i, rule := range r.cfg.Rules {
		if !ruleGatePassed(rule, matched[i], lower) {
			continue
		}
		for _, m := range rule.Pattern.FindAllIndex(line, -1) {
			spans = append(spans, ruleSpan{start: m[0], end: m[1], id: rule.ID})
		}
	}
	if len(spans) == 0 {
		return line
	}
	spans = resolveOverlaps(spans)

	var buf bytes.Buffer
	buf.Grow(len(line))
	cursor := 0
	for _, s := range spans {
		buf.Write(line[cursor:s.start])
		buf.WriteString("[REDACTED:" + s.id + "]")
		cursor = s.end
	}
	buf.Write(line[cursor:])
	return buf.Bytes()
}

