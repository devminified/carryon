package redact

import "testing"

// TestNeutralizeEscapesCoversEveryCase directly exercises
// neutralizeEscapes' every switch branch — it has no dedicated test
// elsewhere; the corpus tests only ever happen to exercise the \n case
// via the shape-2 template that originally caught the word-boundary
// bug (see corpus_test.go).
func TestNeutralizeEscapesCoversEveryCase(t *testing.T) {
	r := NewRedactor(DefaultConfig())

	cases := []struct {
		name string
		in   string
		want byte // the decoded byte every 2-width case fills with
	}{
		{"newline", `a\nb`, '\n'},
		{"tab", `a\tb`, '\t'},
		{"carriage-return", `a\rb`, '\r'},
		{"backspace", `a\bb`, '\b'},
		{"formfeed", `a\fb`, '\f'},
		{"quote", `a\"b`, '"'},
		{"backslash", `a\\b`, '\\'},
		{"slash", `a\/b`, '/'},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := r.neutralizeEscapes([]byte(c.in))
			if len(out) != len(c.in) {
				t.Fatalf("len(out) = %d, want %d (same-width fill)", len(out), len(c.in))
			}
			if out[1] != c.want || out[2] != c.want {
				t.Fatalf("out = %q, want both escape bytes filled with %q", out, c.want)
			}
		})
	}

	t.Run("unicode-escape-six-wide", func(t *testing.T) {
		in := "a" + string([]byte{'\\', 'u', '1', '2', '3', '4'}) + "b"
		out := r.neutralizeEscapes([]byte(in))
		if len(out) != len(in) {
			t.Fatalf("len(out) = %d, want %d", len(out), len(in))
		}
		for i := 1; i < 7; i++ {
			if out[i] != ' ' {
				t.Fatalf("out = %q, want bytes [1,7) filled with spaces", out)
			}
		}
		if out[7] != 'b' {
			t.Fatalf("out = %q, want trailing byte untouched", out)
		}
	})

	t.Run("unrecognized-escape-left-unchanged", func(t *testing.T) {
		in := `a\xb`
		out := r.neutralizeEscapes([]byte(in))
		if string(out) != in {
			t.Fatalf("out = %q, want unchanged %q (unrecognized escape falls to default: continue)", out, in)
		}
	})

	t.Run("no-backslash-returns-input-slice-unchanged", func(t *testing.T) {
		in := []byte("plain text, no escapes")
		out := r.neutralizeEscapes(in)
		if &out[0] != &in[0] {
			t.Fatal("expected the fast path to return the same backing array, not a copy")
		}
	})

	t.Run("trailing-backslash-with-nothing-after-is-left-alone", func(t *testing.T) {
		in := `ab\`
		out := r.neutralizeEscapes([]byte(in))
		if string(out) != in {
			t.Fatalf("out = %q, want unchanged %q (loop bound excludes a backslash with no following byte)", out, in)
		}
	})

	t.Run("scratch-buffer-reused-across-different-lengths", func(t *testing.T) {
		_ = r.neutralizeEscapes([]byte(`a long line with an escape\nright here and more text after it`))
		// Same-width fill, not compaction: `a\nb` is 4 raw bytes (a, \,
		// n, b), and both escape bytes become the real newline byte —
		// the output stays 4 bytes long, same as every other case here.
		out := r.neutralizeEscapes([]byte(`a\nb`))
		want := "a\n\nb"
		if string(out) != want {
			t.Fatalf("out = %q after reusing a larger scratch buffer, want %q", out, want)
		}
	})
}

// TestMatchRuleSpansEmptyStringValueProducesNoSpans covers the
// contentEnd<=contentStart guard directly: an empty JSON string value
// has nothing between its quotes to match against.
func TestMatchRuleSpansEmptyStringValueProducesNoSpans(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	out, n, ok := r.RedactLine([]byte(`{"content":""}`))
	if !ok {
		t.Fatal("ok = false, want true for valid JSON")
	}
	if n != 0 {
		t.Errorf("redactions = %d, want 0 for an empty string value", n)
	}
	if string(out) != `{"content":""}` {
		t.Errorf("out = %s, want the line unchanged", out)
	}
}

// TestRedactJSONHandlesEveryTopLevelTokenShape covers redactJSON's
// corner cases around a line that technically parses as JSON but isn't
// this schema's usual object-of-objects shape.
func TestRedactJSONHandlesEveryTopLevelTokenShape(t *testing.T) {
	r := NewRedactor(DefaultConfig())

	t.Run("bare-top-level-string", func(t *testing.T) {
		out, _, ok := r.RedactLine([]byte(`"just a string, no wrapper"`))
		if !ok {
			t.Fatal("ok = false, want true — a bare JSON string is still valid JSON")
		}
		if string(out) != `"just a string, no wrapper"` {
			t.Errorf("out = %s, want unchanged (nothing to correlate against with an empty stack)", out)
		}
	})

	t.Run("bare-top-level-number", func(t *testing.T) {
		out, n, ok := r.RedactLine([]byte(`12345`))
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if n != 0 || string(out) != `12345` {
			t.Errorf("out = %s n=%d, want unchanged with 0 redactions", out, n)
		}
	})

	t.Run("unterminated-object-is-rejected", func(t *testing.T) {
		in := `{"a":1`
		out, _, ok := r.RedactLine([]byte(in))
		if ok {
			t.Error("ok = true, want false for an object missing its closing brace")
		}
		if string(out) != in {
			t.Errorf("out = %s, want the line unchanged on rejection", out)
		}
	})
}

// TestMatchRuleSpansNoQuoteInWindowReturnsNil covers matchRuleSpans'
// defensive openRel<0 guard directly — unreachable through RedactLine
// itself (every real json.Decoder string token's window always
// contains its own closing quote), but worth keeping correct since it
// guards against ever indexing past a window that doesn't have one.
func TestMatchRuleSpansNoQuoteInWindowReturnsNil(t *testing.T) {
	r := NewRedactor(DefaultConfig())
	line := []byte("no quotes in this byte range at all")
	got := r.matchRuleSpans(line, 0, int64(len(line)))
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}
