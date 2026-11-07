// CO-208: rewrite every "sessionId" field in a rebuilt session.jsonl to
// a freshly generated UUIDv4, and register the resulting fork
// server-side so the puller's own agent has a row to upload into once
// they resume (internal/api.RegisterFork). Path rewrite (CO-207) walks
// a line's JSON token stream the same way this file does, but matches
// a genuinely different thing (arbitrary string content vs. one exact
// key name) — not merged into one abstraction, per this project's
// "wait for the third occurrence" rule.
package pull

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// NewSessionID generates a UUIDv4 (RFC 4122) for a fork — this
// project's one other UUID need (register-chunk, get-manifest, …) has
// always taken an ID the caller already had, so there's no existing
// generator to reuse; crypto/rand plus RFC 4122's two bit tweaks is ~10
// lines and avoids a dependency for something this small.
func NewSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	hexed := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32]), nil
}

// RewriteSessionIDResult mirrors RewriteResult's shape for session-ID
// rewriting.
type RewriteSessionIDResult struct {
	Lines int
	// LinesRewritten is how many lines had a "sessionId" field replaced.
	LinesRewritten int
	// LinesUnparseable is how many lines were copied through unchanged
	// because they weren't valid JSON, or nested deeper than
	// maxJSONDepth — never dropped, always counted.
	LinesUnparseable int
}

// RewriteSessionIDFile streams srcPath line by line, replacing every
// "sessionId" field's value with newSessionID, and writes the result
// to dstPath via a temp file + fsync + atomic rename (I2/I8). Line
// count and order are always preserved exactly.
func RewriteSessionIDFile(srcPath, dstPath, newSessionID string) (*RewriteSessionIDResult, error) {
	result := &RewriteSessionIDResult{}
	err := streamRewriteFile(srcPath, dstPath, func(line []byte) ([]byte, bool) {
		return rewriteSessionIDLine(line, newSessionID)
	}, &result.Lines, &result.LinesRewritten, &result.LinesUnparseable)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RewriteSessionIDLine rewrites one line's "sessionId" field(s) (there
// should only ever be one per line, but every occurrence at any depth
// is replaced — never just the first). ok is false — out is line,
// byte for byte — if the line isn't valid JSON or nests too deep.
func RewriteSessionIDLine(line []byte, newSessionID string) (out []byte, ok bool) {
	return rewriteSessionIDLine(line, newSessionID)
}

func rewriteSessionIDLine(line []byte, newSessionID string) ([]byte, bool) {
	type replacement struct {
		start, end int64
	}
	var replacements []replacement
	var stack []jsonFrame
	pendingSessionIDValue := false

	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()

	for {
		startOffset := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return line, false
		}

		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				if len(stack) >= maxJSONDepth {
					return line, false
				}
				stack = append(stack, jsonFrame{expectKey: true})
			case '[':
				if len(stack) >= maxJSONDepth {
					return line, false
				}
				stack = append(stack, jsonFrame{isArray: true})
			default: // '}' or ']'
				if len(stack) == 0 {
					return line, false
				}
				stack = stack[:len(stack)-1]
				markValueConsumed(stack)
			}

		case string:
			endOffset := dec.InputOffset()
			isKey := len(stack) > 0 && !stack[len(stack)-1].isArray && stack[len(stack)-1].expectKey
			if isKey {
				stack[len(stack)-1].expectKey = false
				pendingSessionIDValue = t == "sessionId"
				continue
			}
			if pendingSessionIDValue {
				window := line[startOffset:endOffset]
				openQuoteRel := bytes.IndexByte(window, '"')
				if openQuoteRel < 0 {
					return line, false
				}
				replacements = append(replacements, replacement{
					start: startOffset + int64(openQuoteRel),
					end:   endOffset,
				})
				pendingSessionIDValue = false
			}
			markValueConsumed(stack)

		default: // number, bool, nil
			pendingSessionIDValue = false
			markValueConsumed(stack)
		}
	}
	if len(stack) != 0 {
		return line, false
	}

	if len(replacements) == 0 {
		return line, true
	}
	encoded, err := json.Marshal(newSessionID)
	if err != nil {
		return line, false
	}
	var buf bytes.Buffer
	buf.Grow(len(line))
	var cursor int64
	for _, rep := range replacements {
		buf.Write(line[cursor:rep.start])
		buf.Write(encoded)
		cursor = rep.end
	}
	buf.Write(line[cursor:])
	return buf.Bytes(), true
}
