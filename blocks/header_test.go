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
			attrs:    nil,
			ok:       true,
			consumed: 7,
		},
		{
			name:     "bare kind with hyphen",
			input:    `go-test`,
			kind:     "go-test",
			attrs:    nil,
			ok:       true,
			consumed: 7,
		},
		{
			name:     "kind with empty parens",
			input:    `summary()`,
			kind:     "summary",
			attrs:    map[string]string{},
			ok:       true,
			consumed: 9,
		},
		{
			name:  "kind with double-quoted parameters",
			input: `change(op="MODIFY", target="Foo", file-path="/test.go")`,
			kind:  "change",
			attrs: map[string]string{
				"op":        "MODIFY",
				"target":    "Foo",
				"file-path": "/test.go",
			},
			ok:       true,
			consumed: len(`change(op="MODIFY", target="Foo", file-path="/test.go")`),
		},
		{
			name:  "kind with single-quoted parameters",
			input: `change(op='MODIFY', target='Foo')`,
			kind:  "change",
			attrs: map[string]string{
				"op":     "MODIFY",
				"target": "Foo",
			},
			ok:       true,
			consumed: len(`change(op='MODIFY', target='Foo')`),
		},
		{
			name:  "space-separated parameters without commas",
			input: `change(op="MODIFY" target="Foo")`,
			kind:  "change",
			attrs: map[string]string{
				"op":     "MODIFY",
				"target": "Foo",
			},
			ok:       true,
			consumed: len(`change(op="MODIFY" target="Foo")`),
		},
		{
			name:  "unquoted values",
			input: `change(op=MODIFY, target=Foo)`,
			kind:  "change",
			attrs: map[string]string{
				"op":     "MODIFY",
				"target": "Foo",
			},
			ok:       true,
			consumed: len(`change(op=MODIFY, target=Foo)`),
		},
		{
			name:  "escaped quotes in value",
			input: `change(find="foo\"bar")`,
			kind:  "change",
			attrs: map[string]string{
				"find": `foo"bar`,
			},
			ok:       true,
			consumed: len(`change(find="foo\"bar")`),
		},
		{
			name:  "escaped newlines and tabs",
			input: `change(find="line1\n\tline2")`,
			kind:  "change",
			attrs: map[string]string{
				"find": "line1\n\tline2",
			},
			ok:       true,
			consumed: len(`change(find="line1\n\tline2")`),
		},
		{
			name:  "incomplete header unclosed paren",
			input: `change(op="MODIFY"`,
			ok:    false,
		},
		{
			name:  "incomplete header unclosed quote",
			input: `change(op="MODIFY)`,
			ok:    false,
		},
		{
			name:  "missing equals sign",
			input: `change(op)`,
			ok:    false,
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

func TestTokenizeHeaderAttributeOnly(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		kind     string
		attrs    map[string]string
		ok       bool
		consumed int
	}{
		{
			name:     "double-quoted kind",
			input:    `kind="summary"`,
			kind:     "summary",
			attrs:    map[string]string{"kind": "summary"},
			ok:       true,
			consumed: len(`kind="summary"`),
		},
		{
			name:     "single-quoted kind",
			input:    `kind='go-test'`,
			kind:     "go-test",
			attrs:    map[string]string{"kind": "go-test"},
			ok:       true,
			consumed: len(`kind='go-test'`),
		},
		{
			name:     "unquoted kind",
			input:    `kind=summary`,
			kind:     "summary",
			attrs:    map[string]string{"kind": "summary"},
			ok:       true,
			consumed: len(`kind=summary`),
		},
		{
			name:     "spaces around equals",
			input:    `kind = "summary"`,
			kind:     "summary",
			attrs:    map[string]string{"kind": "summary"},
			ok:       true,
			consumed: len(`kind = "summary"`),
		},
		{
			name:  "multiple parameters comma-separated",
			input: `kind="change", op="MODIFY", target="Foo"`,
			kind:  "change",
			attrs: map[string]string{
				"kind":   "change",
				"op":     "MODIFY",
				"target": "Foo",
			},
			ok:       true,
			consumed: len(`kind="change", op="MODIFY", target="Foo"`),
		},
		{
			name:  "multiple parameters space-separated",
			input: `op="WRITE" kind="go-test" file-path="/x.go"`,
			kind:  "go-test",
			attrs: map[string]string{
				"op":        "WRITE",
				"kind":      "go-test",
				"file-path": "/x.go",
			},
			ok:       true,
			consumed: len(`op="WRITE" kind="go-test" file-path="/x.go"`),
		},
		{
			name:     "trailing space",
			input:    `kind="summary" `,
			kind:     "summary",
			attrs:    map[string]string{"kind": "summary"},
			ok:       true,
			consumed: len(`kind="summary" `),
		},
		{
			name:  "missing kind attribute",
			input: `op="MODIFY" target="Foo"`,
			ok:    false,
		},
		{
			name:  "empty kind value",
			input: `kind=""`,
			ok:    false,
		},
		{
			name:  "unclosed quote",
			input: `kind="summary`,
			ok:    false,
		},
		{
			name:  "trailing junk without equals",
			input: `kind="summary" junk`,
			ok:    false,
		},
		{
			name:  "stray closing paren",
			input: `kind="summary")`,
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

func TestParseHeader(t *testing.T) {
	t.Run("ValidHeaderNoTrailing", func(t *testing.T) {
		kind, attrs, ok := parseHeader(`change(op="MODIFY")`)
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

	t.Run("HeaderWithTrailingProseRejected", func(t *testing.T) {
		_, _, ok := parseHeader(`change(op="MODIFY") trailing text`)
		if ok {
			t.Fatal("expected header with trailing text to be rejected")
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
		{`change(op="MODIFY"`, "change"},
		{`go-test`, "go-test"},
		{`  ingest(param="v"`, "ingest"},
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
