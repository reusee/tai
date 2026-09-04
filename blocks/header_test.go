package blocks

import (
	"testing"
)

func TestTokenizeHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		kind     string
		attrs    map[string]string
		ok       bool
		consumed int
	}{
		{
			name:     "bare kind",
			input:    `summary`,
			kind:     "summary",
			ok:       true,
			consumed: 7,
		},
		{
			name:     "bare kind with hyphen",
			input:    `go-test`,
			kind:     "go-test",
			ok:       true,
			consumed: 7,
		},
		{
			name:     "kind followed by paren is accepted with trailing content",
			input:    `summary(`,
			kind:     "summary",
			ok:       true,
			consumed: 7,
		},
		{
			name:     "kind followed by equals is accepted with trailing content",
			input:    `summary=`,
			kind:     "summary",
			ok:       true,
			consumed: 7,
		},
		{
			name:  "empty string",
			input: ``,
			ok:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, consumed, ok := TokenizeHeader(tc.input)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got ok=%v", tc.ok, ok)
			}
			if !ok {
				return
			}
			if token.Kind != tc.kind {
				t.Fatalf("expected kind %q, got %q", tc.kind, token.Kind)
			}
			if tc.consumed > 0 && consumed != tc.consumed {
				t.Fatalf("expected consumed=%d, got %d", tc.consumed, consumed)
			}
			for k, expectedV := range tc.attrs {
				gotV, exists := token.Attributes[k]
				if !exists {
					t.Fatalf("expected attribute %q, not found", k)
				}
				if gotV != expectedV {
					t.Fatalf("expected attribute %q=%q, got %q", k, expectedV, gotV)
				}
			}
		})
	}
}

// TestTokenizeHeaderURI covers the taught RFC 3986 URI header form: the
// scheme is the kind, the optional path precedes the query, and the query
// holds percent-decoded key=value pairs. See TheoryOfHeaderTokenizing.
func TestTokenizeHeaderURI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		kind     string
		path     string
		attrs    map[string]string
		ok       bool
		consumed int
	}{
		{
			name:     "scheme with colon",
			input:    `summary:`,
			kind:     "summary",
			ok:       true,
			consumed: len(`summary:`),
		},
		{
			name:     "scheme and query without path",
			input:    `change:?op=MODIFY&target=Foo`,
			kind:     "change",
			attrs:    map[string]string{"op": "MODIFY", "target": "Foo"},
			ok:       true,
			consumed: len(`change:?op=MODIFY&target=Foo`),
		},
		{
			name:     "path and query",
			input:    `change:/x.go?op=WRITE&file-path=%2Fhome%2Fx.go`,
			kind:     "change",
			path:     "/x.go",
			attrs:    map[string]string{"op": "WRITE", "file-path": "/home/x.go"},
			ok:       true,
			consumed: len(`change:/x.go?op=WRITE&file-path=%2Fhome%2Fx.go`),
		},
		{
			name:     "path only",
			input:    `change:/x.go`,
			kind:     "change",
			path:     "/x.go",
			attrs:    map[string]string{},
			ok:       true,
			consumed: len(`change:/x.go`),
		},
		{
			name:     "percent-decoded value",
			input:    `handoff:?note=hello%20world%22q%22%5C%09%0Aend`,
			kind:     "handoff",
			attrs:    map[string]string{"note": "hello world\"q\"\\\t\nend"},
			ok:       true,
			consumed: len(`handoff:?note=hello%20world%22q%22%5C%09%0Aend`),
		},
		{
			name:  "query without colon rejected",
			input: `summary?a=b`,
			ok:    false,
		},
		{
			name:  "pair without equals rejected",
			input: `change:?flag`,
			ok:    false,
		},
		{
			name:  "empty key rejected",
			input: `change:?=v`,
			ok:    false,
		},
		{
			name:  "malformed escape rejected",
			input: `change:?key=%zz`,
			ok:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, consumed, ok := TokenizeHeader(tc.input)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got ok=%v", tc.ok, ok)
			}
			if !ok {
				return
			}
			if token.Kind != tc.kind {
				t.Fatalf("expected kind %q, got %q", tc.kind, token.Kind)
			}
			if token.Path != tc.path {
				t.Fatalf("expected path %q, got %q", tc.path, token.Path)
			}
			if tc.consumed > 0 && consumed != tc.consumed {
				t.Fatalf("expected consumed=%d, got %d", tc.consumed, consumed)
			}
			for k, expectedV := range tc.attrs {
				gotV, exists := token.Attributes[k]
				if !exists {
					t.Fatalf("expected attribute %q, not found", k)
				}
				if gotV != expectedV {
					t.Fatalf("expected attribute %q=%q, got %q", k, expectedV, gotV)
				}
			}
		})
	}
}

func TestParseHeader(t *testing.T) {
	t.Run("ValidHeaderNoTrailing", func(t *testing.T) {
		kind, attrs, ok := parseHeader(`change:?op=MODIFY`)
		if !ok {
			t.Fatal("expected ok")
		}
		if kind != "change" {
			t.Fatalf("expected change, got %s", kind)
		}
		if attrs["op"] != "MODIFY" {
			t.Fatalf("expected op=MODIFY, got %v", attrs["op"])
		}
	})

	t.Run("SchemeOnly", func(t *testing.T) {
		kind, attrs, ok := parseHeader(`summary:`)
		if !ok {
			t.Fatal("expected ok")
		}
		if kind != "summary" || len(attrs) != 0 {
			t.Fatalf("expected summary with no attrs, got %s %v", kind, attrs)
		}
	})

	t.Run("HeaderWithTrailingProseRejected", func(t *testing.T) {
		_, _, ok := parseHeader(`change:?op=MODIFY trailing text`)
		if ok {
			t.Fatal("expected header with trailing text to be rejected")
		}
	})

	t.Run("FunctionCallFormRejected", func(t *testing.T) {
		_, _, ok := parseHeader(`change(op="MODIFY")`)
		if ok {
			t.Fatal("expected the deleted function-call form to be rejected")
		}
	})

	t.Run("AttributeOnlyFormRejected", func(t *testing.T) {
		_, _, ok := parseHeader(`kind="summary"`)
		if ok {
			t.Fatal("expected the deleted attribute-only form to be rejected")
		}
	})

	t.Run("FragmentRejected", func(t *testing.T) {
		_, _, ok := parseHeader(`change:/x.go#frag`)
		if ok {
			t.Fatal("expected a header carrying a fragment to be rejected")
		}
	})

	t.Run("BareKindWithTrailingProseRejected", func(t *testing.T) {
		_, _, ok := parseHeader(`summary some extra notes`)
		if ok {
			t.Fatal("expected bare kind with trailing text to be rejected")
		}
	})
}

func TestExtractKindName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`change:?op=MOD`, "change"},
		{`go-test`, "go-test"},
		{`  ingest:?pattern=v`, "ingest"},
		{`summary:`, "summary"},
		{`summary?a=b`, "summary"},
		{``, ""},
		{`(invalid)`, ""},
	}
	for _, tc := range tests {
		got := extractKindName(tc.input)
		if got != tc.want {
			t.Fatalf("extractKindName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
