// CO-207: rewrite every occurrence of a pulled session's original
// working-tree path to its restored location, inside the JSON string
// values of a rebuilt session.jsonl — never via raw text substitution,
// which can corrupt JSON escaping or, worse, silently rewrite the
// wrong thing ("/acme-api" inside "/acme-api-old"). Session-ID rewrite
// is CO-208, not this file.
package pull

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxJSONDepth matches the SDD's "iterative depth-first traversal with
// an explicit stack, depth limit 512" (algorithm #6) — a crafted,
// deeply nested line is rejected (kept unchanged, counted) rather than
// risking an unbounded walk.
const maxJSONDepth = 512

var winDriveRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// rootMapping is one registered "old path -> new path" rewrite.
type rootMapping struct {
	// oldNormalized and new are both compared/emitted with forward
	// slashes internally; RewriteLine converts a match's surrounding
	// text to the target's own separator style as it splices (see
	// rewriteString).
	oldNormalized   string
	new             string
	caseInsensitive bool
}

// PathRewriter maps one or more old working-tree root paths to their
// restored location, and rewrites every genuine occurrence (never a
// mere shared prefix — CO-207's "/acme-api would also corrupt
// /acme-api-old") found in a JSONL transcript's string values.
type PathRewriter struct {
	roots []rootMapping
}

// NewPathRewriter creates a PathRewriter mapping oldRoot's every
// occurrence to newRoot. oldRoot is treated as a Windows path (and so
// matched case-insensitively, per CO-207's "drive letters and
// case-insensitive matching") when it starts with a drive letter
// (C:\ or C:/) — Windows filesystems are case-insensitive, POSIX ones
// are not, so folding case for a POSIX root would itself introduce
// false matches.
func NewPathRewriter(oldRoot, newRoot string) *PathRewriter {
	r := &PathRewriter{}
	r.AddAlias(oldRoot, newRoot)
	return r
}

// AddAlias registers an additional old root that also maps to newRoot.
// CO-207 also calls for "rewrit[ing] paths under the developer's home
// folder that point into the repo through symlinks, when resolvable" —
// resolving such an alias requires information this package doesn't
// have (the *original* machine's symlink topology isn't available to
// the puller at all; only the caller, with broader context, could ever
// discover one). AddAlias is that extension point: a caller that finds
// one passes it here. This package deliberately doesn't guess at
// symlink aliases on its own.
func (r *PathRewriter) AddAlias(oldRoot, newRoot string) {
	r.roots = append(r.roots, rootMapping{
		oldNormalized:   toForwardSlash(oldRoot),
		new:             newRoot,
		caseInsensitive: winDriveRe.MatchString(oldRoot),
	})
}

// toForwardSlash converts '\' to '/', always — unlike filepath.ToSlash,
// which only does this when GOOS is windows. The text being normalized
// here belongs to whichever OS originally captured the session, not to
// the OS this binary happens to be running on right now (CO-207's own
// "macOS to Linux, Linux to Windows, Windows to macOS" cross-platform
// fixtures only make sense if normalization never depends on runtime
// GOOS — found by running this against a real cross-platform case and
// seeing filepath.ToSlash silently do nothing on a non-Windows build).
func toForwardSlash(s string) string {
	return strings.ReplaceAll(s, `\`, "/")
}

// RewriteResult summarizes one RewriteFile call.
type RewriteResult struct {
	Lines int
	// LinesRewritten is how many lines had at least one replacement.
	LinesRewritten int
	// LinesUnparseable is how many lines were copied through unchanged
	// because they weren't valid JSON, or nested deeper than
	// maxJSONDepth — never silently dropped, always counted.
	LinesUnparseable int
}

// RewriteFile streams srcPath line by line, applying every registered
// root to every line's JSON string values, and writes the result to
// dstPath via a temp file + fsync + atomic rename (I2/I8). Line count
// and order are always preserved exactly.
func (r *PathRewriter) RewriteFile(srcPath, dstPath string) (*RewriteResult, error) {
	src, err := os.Open(srcPath) // #nosec G304 -- srcPath is the caller's own rebuilt session file
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating directory for %s: %w", dstPath, err)
	}
	tmp := dstPath + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- tmp is derived from the caller's own dstPath
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", tmp, err)
	}

	result := &RewriteResult{}
	w := bufio.NewWriter(dst)
	reader := bufio.NewReader(src)

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr != nil {
			break
		}

		hadNewline := bytes.HasSuffix(line, []byte("\n"))
		trimmed := line
		if hadNewline {
			trimmed = line[:len(line)-1]
		}

		result.Lines++
		out, ok := r.RewriteLine(trimmed)
		if !ok {
			result.LinesUnparseable++
		} else if !bytes.Equal(out, trimmed) {
			result.LinesRewritten++
		}

		if _, err := w.Write(out); err != nil {
			_ = dst.Close()
			_ = os.Remove(tmp)
			return nil, fmt.Errorf("writing %s: %w", tmp, err)
		}
		if hadNewline {
			if err := w.WriteByte('\n'); err != nil {
				_ = dst.Close()
				_ = os.Remove(tmp)
				return nil, fmt.Errorf("writing %s: %w", tmp, err)
			}
		}

		if readErr != nil {
			if readErr != io.EOF {
				_ = dst.Close()
				_ = os.Remove(tmp)
				return nil, fmt.Errorf("reading %s: %w", srcPath, readErr)
			}
			break
		}
	}

	if err := w.Flush(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("flushing %s: %w", tmp, err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("fsyncing %s: %w", tmp, err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dstPath); err != nil {
		return nil, fmt.Errorf("renaming %s to %s: %w", tmp, dstPath, err)
	}
	return result, nil
}

// jsonFrame tracks one currently-open container while RewriteLine
// walks a line's token stream — the "explicit stack" the SDD calls for
// instead of recursing with the JSON structure (algorithm #6).
type jsonFrame struct {
	isArray   bool
	expectKey bool // only meaningful for an object frame
}

// RewriteLine rewrites one JSON line (no trailing newline) in place,
// touching only string VALUES (never object keys) at any nesting
// depth, up to maxJSONDepth. ok is false — out is line, byte for
// byte — if the line isn't valid JSON or nests too deep; the caller
// counts that rather than dropping or corrupting the line.
func (r *PathRewriter) RewriteLine(line []byte) (out []byte, ok bool) {
	type replacement struct {
		start, end int64
		value      string
	}
	var replacements []replacement
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
				continue
			}
			if newVal, changed := r.rewriteString(t); changed {
				encoded, err := json.Marshal(newVal)
				if err != nil {
					return line, false
				}
				window := line[startOffset:endOffset]
				openQuoteRel := bytes.IndexByte(window, '"')
				if openQuoteRel < 0 {
					return line, false
				}
				replacements = append(replacements, replacement{
					start: startOffset + int64(openQuoteRel),
					end:   endOffset,
					value: string(encoded),
				})
			}
			markValueConsumed(stack)

		default: // number, bool, nil
			markValueConsumed(stack)
		}
	}
	if len(stack) != 0 {
		return line, false
	}

	if len(replacements) == 0 {
		return line, true
	}
	var buf bytes.Buffer
	buf.Grow(len(line))
	var cursor int64
	for _, rep := range replacements {
		buf.Write(line[cursor:rep.start])
		buf.WriteString(rep.value)
		cursor = rep.end
	}
	buf.Write(line[cursor:])
	return buf.Bytes(), true
}

// markValueConsumed records that the current frame just received one
// complete value (a scalar, or a container that just closed) — for an
// object frame, the next token is a key again; for an array frame,
// there's nothing to toggle, every token is another element.
func markValueConsumed(stack []jsonFrame) {
	if len(stack) == 0 {
		return
	}
	top := &stack[len(stack)-1]
	if !top.isArray {
		top.expectKey = true
	}
}

// isSegmentContinuationByte reports whether b can appear inside a
// path/filename segment — used at both ends of a candidate match to
// require a genuine segment boundary, not just a shared prefix (CO-207:
// "/acme-api would also corrupt /acme-api-old", and symmetrically,
// corrupt ".../backup/acme-api" by matching its suffix instead of a
// genuine standalone path).
func isSegmentContinuationByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_' || b == '-' || b == '.':
		return true
	}
	return false
}

// rewriteString finds every genuine occurrence of a registered root in
// s and replaces it (and any immediately-following path segments) with
// the new root, converting that whole replaced span's separators to
// the new root's own style — the matching happens on a slash-normalized
// view of s so an old root written with '\' still matches a line that
// spells it with '/' (or vice versa), the mixed-separator case CO-207's
// cross-platform fixtures exercise.
func (r *PathRewriter) rewriteString(s string) (string, bool) {
	if len(r.roots) == 0 || s == "" {
		return s, false
	}
	norm := toForwardSlash(s) // same byte length as s: backslash -> slash is 1-for-1

	type span struct {
		start, end int
		new        string
	}
	var matches []span

	i := 0
	for i < len(norm) {
		bestRoot := -1
		bestLen := 0
		for ri, root := range r.roots {
			ol := len(root.oldNormalized)
			if ol == 0 || i+ol > len(norm) {
				continue
			}
			candidate := norm[i : i+ol]
			if root.caseInsensitive {
				if !strings.EqualFold(candidate, root.oldNormalized) {
					continue
				}
			} else if candidate != root.oldNormalized {
				continue
			}
			if i > 0 && isSegmentContinuationByte(norm[i-1]) {
				continue
			}
			if i+ol < len(norm) && isSegmentContinuationByte(norm[i+ol]) {
				continue
			}
			if ol > bestLen {
				bestRoot, bestLen = ri, ol
			}
		}
		if bestRoot < 0 {
			i++
			continue
		}

		tailEnd := i + bestLen
		for tailEnd < len(norm) && norm[tailEnd] == '/' {
			j := tailEnd + 1
			for j < len(norm) && isSegmentContinuationByte(norm[j]) {
				j++
			}
			tailEnd = j
		}

		newSep := "/"
		if strings.Contains(r.roots[bestRoot].new, `\`) && !strings.Contains(r.roots[bestRoot].new, "/") {
			newSep = `\`
		}
		tail := norm[i+bestLen : tailEnd]
		if newSep != "/" {
			tail = strings.ReplaceAll(tail, "/", newSep)
		}
		matches = append(matches, span{start: i, end: tailEnd, new: r.roots[bestRoot].new + tail})
		i = tailEnd
	}

	if len(matches) == 0 {
		return s, false
	}
	var b strings.Builder
	cursor := 0
	for _, m := range matches {
		b.WriteString(s[cursor:m.start])
		b.WriteString(m.new)
		cursor = m.end
	}
	b.WriteString(s[cursor:])
	return b.String(), true
}
