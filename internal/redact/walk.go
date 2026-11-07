package redact

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
)

// jsonFrame tracks one currently-open container while redactJSON walks
// a line's token stream (the same "explicit stack instead of
// recursion" shape as internal/pull's rewriter, SDD algorithm #6).
// Beyond that shared shape, this frame also accumulates just enough of
// its own direct children's values to recognize — once it closes —
// whether it was a tool_use or tool_result block, by its sibling keys
// alone, regardless of how deep in the overall line it's nested.
type jsonFrame struct {
	isArray   bool
	expectKey bool
	lastKey   string // the key whose value comes next (object frames only)

	typeVal   string // this object's own "type" string value, if any
	nameVal   string // this object's own "name" string value, if any (tool_use's tool name)
	toolUseID string // "id" (tool_use) or "tool_use_id" (tool_result)

	filePathValue string // bubbled up from a child "input" object's own "file_path"
	hasFilePath   bool

	contentSpan [2]int64 // this object's own "content" value's byte range (string, or a bubbled-up container)
	hasContent  bool

	// startedAsContent/selfStart let a container value (content can be
	// a plain string OR an array of content blocks in the real schema)
	// report its own span up to its parent once it closes, the same way
	// "input" bubbles up file_path.
	startedAsContent bool
	selfStart        int64
}

// foundToolUse and foundToolResult are what redactJSON collects as
// frames close, for the denylist correlation step once the whole line
// has been walked (frames themselves are discarded as the stack pops).
type foundToolUse struct {
	id, name, filePath string
	hasFilePath        bool
}

type foundToolResult struct {
	toolUseID  string
	span       [2]int64
	hasContent bool
}

// redactJSON walks line as JSON, collecting every rule match in every
// string value plus every tool_use/tool_result pair, then splices in
// the replacements (rule matches, and any denylisted tool_result's
// entire content, which supersedes any finer-grained match inside it).
// ok is false if line isn't valid JSON or nests too deep — the caller
// falls back to raw-byte scanning in that case (see RedactLine).
func (r *Redactor) redactJSON(line []byte) (out []byte, redactions int, ok bool) {
	var ruleSpans []ruleSpan
	var toolUses []foundToolUse
	var toolResults []foundToolResult
	var stack []jsonFrame

	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()

	for {
		startOffset := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return line, 0, false
		}
		endOffset := dec.InputOffset()

		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				if len(stack) >= maxJSONDepth {
					return line, 0, false
				}
				frame := jsonFrame{isArray: t == '[', expectKey: t == '{'}
				if len(stack) > 0 {
					parent := &stack[len(stack)-1]
					if !parent.isArray && parent.lastKey == "content" {
						frame.startedAsContent = true
						// startOffset is InputOffset() from BEFORE this
						// token was read — i.e. right after the
						// preceding "content" key token, not at this
						// delimiter itself, so it still includes the
						// ':' (and any whitespace) between them. The
						// delimiter byte can only be preceded by
						// whitespace/':' here (JSON grammar), so the
						// first occurrence of it from startOffset is
						// always this token's own real position.
						frame.selfStart = startOffset + int64(bytes.IndexByte(line[startOffset:], byte(t))) // #nosec G115 -- t is always one of the ASCII delimiters '{','[','}',']'
					}
				}
				stack = append(stack, frame)

			default: // '}' or ']'
				if len(stack) == 0 {
					return line, 0, false
				}
				popped := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				if popped.typeVal == "tool_use" {
					toolUses = append(toolUses, foundToolUse{
						id: popped.toolUseID, name: popped.nameVal,
						filePath: popped.filePathValue, hasFilePath: popped.hasFilePath,
					})
				}
				if popped.typeVal == "tool_result" {
					span := popped.contentSpan
					hasContent := popped.hasContent
					toolResults = append(toolResults, foundToolResult{
						toolUseID: popped.toolUseID, span: span, hasContent: hasContent,
					})
				}

				if len(stack) > 0 {
					newTop := &stack[len(stack)-1]
					if popped.startedAsContent {
						newTop.contentSpan = [2]int64{popped.selfStart, endOffset}
						newTop.hasContent = true
					}
					if !newTop.isArray && newTop.lastKey == "input" && popped.hasFilePath {
						newTop.filePathValue = popped.filePathValue
						newTop.hasFilePath = true
					}
					markValueConsumed(newTop)
				}
			}

		case string:
			if len(stack) == 0 {
				continue // a bare top-level JSON string (not this schema, but not an error either)
			}
			top := &stack[len(stack)-1]
			isKey := !top.isArray && top.expectKey
			if isKey {
				top.lastKey = t
				top.expectKey = false
				continue
			}

			switch top.lastKey {
			case "type":
				top.typeVal = t
			case "name":
				top.nameVal = t
			case "id", "tool_use_id":
				top.toolUseID = t
			case "file_path":
				top.filePathValue = t
				top.hasFilePath = true
			case "content":
				// Same startOffset caveat as the container case above:
				// find this string token's own opening quote rather
				// than using startOffset directly, which still
				// includes the preceding ':' (and any whitespace).
				quoteStart := startOffset + int64(bytes.IndexByte(line[startOffset:], '"'))
				top.contentSpan = [2]int64{quoteStart, endOffset}
				top.hasContent = true
			}

			ruleSpans = append(ruleSpans, r.matchRuleSpans(line, startOffset, endOffset)...)
			markValueConsumed(top)

		default: // number, bool, nil
			if len(stack) > 0 {
				markValueConsumed(&stack[len(stack)-1])
			}
		}
	}
	if len(stack) != 0 {
		return line, 0, false
	}

	denylistSpans := r.resolveToolCalls(toolUses, toolResults)
	return spliceSpans(line, ruleSpans, denylistSpans)
}

func markValueConsumed(top *jsonFrame) {
	if !top.isArray {
		top.expectKey = true
	}
}

// matchRuleSpans runs every rule against the decoded string value found
// in line[startOffset:endOffset] (a JSON string token, quotes included)
// and returns each match's byte range *within line*, by locating the
// value's content between its opening and closing quotes the same way
// internal/pull's rewriter does: the first '"' found scanning forward
// from startOffset is always the opening quote, since nothing before it
// in that window can itself be an unescaped quote (JSON grammar).
func (r *Redactor) matchRuleSpans(line []byte, startOffset, endOffset int64) []ruleSpan {
	window := line[startOffset:endOffset]
	openRel := bytes.IndexByte(window, '"')
	if openRel < 0 {
		return nil
	}
	contentStart := startOffset + int64(openRel) + 1
	contentEnd := endOffset - 1 // the closing quote itself
	if contentEnd <= contentStart {
		return nil
	}
	raw := line[contentStart:contentEnd] // still JSON-escaped, e.g. \n is backslash+n, not a real newline
	matchable := r.neutralizeEscapes(raw)
	lower := r.lowerInto(matchable)
	matched := r.matchKeywords(lower)

	var spans []ruleSpan
	for i, rule := range r.cfg.Rules {
		if !ruleGatePassed(rule, matched[i], lower) {
			continue
		}
		for _, m := range rule.Pattern.FindAllIndex(matchable, -1) {
			spans = append(spans, ruleSpan{
				start: int(contentStart) + m[0],
				end:   int(contentStart) + m[1],
				id:    rule.ID,
			})
		}
	}
	return spans
}

// resolveToolCalls matches each tool_use found in this line against
// the Redactor's persisted pending map, each tool_result against both
// this line's own tool_uses and the pending map, and returns the
// full-content replacement spans for every denylisted tool_result
// resolved — updating r.pending in place (the caller persists it via
// Pending() after processing a batch of lines).
func (r *Redactor) resolveToolCalls(toolUses []foundToolUse, toolResults []foundToolResult) []ruleSpan {
	for _, tu := range toolUses {
		if tu.hasFilePath && isDenylisted(tu.filePath, r.cfg.Denylist) {
			r.pending[tu.id] = filepath.Base(tu.filePath)
		}
	}

	var spans []ruleSpan
	for _, tr := range toolResults {
		name, ok := r.pending[tr.toolUseID]
		if !ok || !tr.hasContent {
			continue
		}
		delete(r.pending, tr.toolUseID)
		spans = append(spans, ruleSpan{
			start: int(tr.span[0]), end: int(tr.span[1]),
			id: "denylisted-file " + name,
		})
	}
	return spans
}

// spliceSpans applies ruleSpans (individual secret matches) and
// denylistSpans (whole-content replacements, which supersede any
// rule match they fully contain) to line, returning the result and the
// total number of redactions actually made.
func spliceSpans(line []byte, ruleSpans, denylistSpans []ruleSpan) ([]byte, int, bool) {
	all := make([]ruleSpan, 0, len(ruleSpans)+len(denylistSpans))
	// Denylist spans are typically much wider than any rule match they
	// contain; sorting by (start asc, end desc) before resolveOverlaps
	// makes the wider span win, exactly like two overlapping rule
	// matches already do.
	all = append(all, denylistSpans...)
	all = append(all, ruleSpans...)
	if len(all) == 0 {
		return line, 0, true
	}
	all = resolveOverlaps(all)

	var buf bytes.Buffer
	buf.Grow(len(line))
	cursor := 0
	for _, s := range all {
		buf.Write(line[cursor:s.start])
		if name, ok := strings.CutPrefix(s.id, "denylisted-file "); ok {
			// A denylist span covers the *whole* original value,
			// delimiters included (the opening/closing quotes for a
			// string content, or the [ ] for an array of content
			// blocks) — so the replacement must be a complete,
			// self-quoted JSON string, unlike a rule match's
			// replacement text, which is spliced *inside* quotes that
			// stay in place either side of it.
			buf.Write(mustMarshalJSON("[REDACTED:denylisted-file " + name + "]"))
		} else {
			buf.WriteString("[REDACTED:" + s.id + "]")
		}
		cursor = s.end
	}
	buf.Write(line[cursor:])
	return buf.Bytes(), len(all), true
}

// mustMarshalJSON encodes s as a JSON string. json.Marshal only fails
// for types it can't represent at all; a string always succeeds.
func mustMarshalJSON(s string) []byte {
	encoded, _ := json.Marshal(s)
	return encoded
}

// neutralizeEscapes returns a copy of raw (a JSON string's still-
// escaped content) the exact same length, with each escape sequence's
// bytes replaced by its real decoded byte repeated to fill the same
// width — found the hard way: matching rule patterns directly against
// still-escaped bytes lets a one-character escape like \n (literal
// backslash + 'n') defeat a \b word-boundary check, since 'n' is a
// word character that would otherwise sit directly against a secret
// starting right after it (e.g. a secret on its own line in captured
// tool output, immediately preceded by \n) — \bAKIA... never matches
// "...\nAKIA..." because regexp sees one unbroken word run from 'n'
// through the secret, even though the *real*, decoded text has an
// actual newline there, which is correctly non-word. Replacing \n with
// two real newline bytes (not a placeholder) fixes the boundary while
// keeping every byte offset valid for splicing the original raw bytes,
// and keeps multi-line patterns (private-key-block's dotall match)
// working the same as they would against the real decoded text.
func (r *Redactor) neutralizeEscapes(raw []byte) []byte {
	if bytes.IndexByte(raw, '\\') < 0 {
		return raw // common case: nothing escaped, no copy needed at all
	}
	if cap(r.escapeScratch) < len(raw) {
		r.escapeScratch = make([]byte, len(raw))
	}
	out := r.escapeScratch[:len(raw)]
	copy(out, raw)
	for i := 0; i < len(out)-1; i++ {
		if out[i] != '\\' {
			continue
		}
		var real byte
		width := 2
		switch out[i+1] {
		case 'n':
			real = '\n'
		case 't':
			real = '\t'
		case 'r':
			real = '\r'
		case 'b':
			real = '\b'
		case 'f':
			real = '\f'
		case '"':
			real = '"'
		case '\\':
			real = '\\'
		case '/':
			real = '/'
		case 'u':
			// \uXXXX: 6 raw bytes. The decoded code point can be a
			// word character (rare for a real secret's surroundings,
			// unlike \n/\t/\r), so this is a best-effort fill with a
			// clearly non-word placeholder rather than a full UTF-16
			// decode — good enough to avoid spuriously *joining* two
			// otherwise-separate matches, which is this function's
			// actual job; it was never relied on to decode content.
			real = ' '
			width = 6
		default:
			continue
		}
		for j := 0; j < width && i+j < len(out); j++ {
			out[i+j] = real
		}
		i += width - 1
	}
	return out
}

// isDenylisted reports whether path's basename matches any of the
// configured denylist globs.
func isDenylisted(path string, denylist []string) bool {
	base := filepath.Base(path)
	for _, pattern := range denylist {
		if ok, _ := filepath.Match(pattern, base); ok {
			return true
		}
	}
	return false
}
