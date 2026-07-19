package blocks

import (
	"testing"
)

func TestTokenizeXMLOpeningTag(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		kind      string
		attrs     map[string]string
		ok        bool
		isClosing bool
	}{
		{
			name:  "simple opening tag",
			input: `<change>`,
			kind:  "change",
			attrs: map[string]string{},
			ok:    true,
		},
		{
			name:  "opening tag with attributes",
			input: `<change op="MODIFY" target="Foo" file-path="/test.go">`,
			kind:  "change",
			attrs: map[string]string{
				"op":        "MODIFY",
				"target":    "Foo",
				"file-path": "/test.go",
			},
			ok: true,
		},
		{
			name:  "opening tag with single-quoted attributes",
			input: `<change op='MODIFY' target='Foo'>`,
			kind:  "change",
			attrs: map[string]string{
				"op":     "MODIFY",
				"target": "Foo",
			},
			ok: true,
		},
		{
			name:  "self-closing tag",
			input: `<change op="DELETE" />`,
			kind:  "change",
			attrs: map[string]string{
				"op": "DELETE",
			},
			ok: true,
		},
		{
			name:  "gt inside attribute value",
			input: `<change op="MODIFY" target="a>b" file-path="/x.go">`,
			kind:  "change",
			attrs: map[string]string{
				"op":        "MODIFY",
				"target":    "a>b",
				"file-path": "/x.go",
			},
			ok: true,
		},
		{
			name:  "gt inside single-quoted value",
			input: `<change op='a>b' target='c>d'>`,
			kind:  "change",
			attrs: map[string]string{
				"op":     "a>b",
				"target": "c>d",
			},
			ok: true,
		},
		{
			name:  "incomplete tag no closing gt",
			input: `<change op="MODIFY"`,
			ok:    false,
		},
		{
			name:  "incomplete tag unclosed quote",
			input: `<change op="MODIFY`,
			ok:    false,
		},
		{
			name:  "incomplete tag gt inside quote only",
			input: `<change op="a>b"`,
			ok:    false,
		},
		{
			name:      "closing tag parsed as closing not opening",
			input:     `</change>`,
			kind:      "change",
			attrs:     map[string]string{},
			ok:        true,
			isClosing: true,
		},
		{
			name:  "not a tag",
			input: `not a tag`,
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
			token, _, ok := TokenizeXMLTag(tc.input)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got ok=%v", tc.ok, ok)
			}
			if !ok {
				return
			}
			if token.IsClosing() != tc.isClosing {
				if tc.isClosing {
					t.Fatal("expected closing tag")
				}
				t.Fatal("expected non-closing tag")
			}
			if token.Kind != tc.kind {
				t.Fatalf("expected kind %q, got %q", tc.kind, token.Kind)
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

func TestTokenizeXMLClosingTag(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		kind      string
		ok        bool
		isClosing bool
	}{
		{"simple closing tag", `</change>`, "change", true, true},
		{"closing tag with whitespace", `</change >`, "change", true, true},
		{"closing tag with leading whitespace", `  </change>`, "change", true, true},
		{"opening tag is not closing", `<change>`, "change", true, false},
		{"self-closing tag is not closing", `<change />`, "change", true, false},
		{"incomplete closing tag", `</change`, "", false, false},
		{"not a tag", `not a tag`, "", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, _, ok := TokenizeXMLTag(tc.input)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got ok=%v", tc.ok, ok)
			}
			if !ok {
				return
			}
			if token.IsClosing() != tc.isClosing {
				if tc.isClosing {
					t.Fatal("expected closing tag")
				}
				t.Fatal("expected non-closing tag")
			}
			if token.Kind != tc.kind {
				t.Fatalf("expected kind %q, got %q", tc.kind, token.Kind)
			}
		})
	}
}

func TestTokenizeXMLTagEntities(t *testing.T) {
	tests := []struct {
		name  string
		input string
		attrs map[string]string
	}{
		{
			name:  "amp entity",
			input: `<change op="a&amp;b">`,
			attrs: map[string]string{"op": "a&b"},
		},
		{
			name:  "lt entity",
			input: `<change op="a&lt;b">`,
			attrs: map[string]string{"op": "a<b"},
		},
		{
			name:  "gt entity",
			input: `<change op="a&gt;b">`,
			attrs: map[string]string{"op": "a>b"},
		},
		{
			name:  "quot entity",
			input: `<change op="a&quot;b">`,
			attrs: map[string]string{"op": `a"b`},
		},
		{
			name:  "apos entity",
			input: `<change op="a&apos;b">`,
			attrs: map[string]string{"op": "a'b"},
		},
		{
			name:  "no entities",
			input: `<change op="MODIFY">`,
			attrs: map[string]string{"op": "MODIFY"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, _, ok := TokenizeXMLTag(tc.input)
			if !ok {
				t.Fatal("expected ok")
			}
			for k, expectedV := range tc.attrs {
				gotV := token.Attributes[k]
				if gotV != expectedV {
					t.Fatalf("expected %q=%q, got %q", k, expectedV, gotV)
				}
			}
		})
	}
}

func TestTokenizeXMLTagConsumed(t *testing.T) {
	// Verify that consumed includes the terminating '>'.
	input := `<change op="MODIFY">`
	_, consumed, ok := TokenizeXMLTag(input)
	if !ok {
		t.Fatal("expected ok")
	}
	if consumed != len(input) {
		t.Fatalf("expected consumed=%d, got %d", len(input), consumed)
	}

	// Verify that consumed includes the '/>' for self-closing tags.
	input2 := `<change op="DELETE" />`
	_, consumed2, ok2 := TokenizeXMLTag(input2)
	if !ok2 {
		t.Fatal("expected ok")
	}
	if consumed2 != len(input2) {
		t.Fatalf("expected consumed=%d, got %d", len(input2), consumed2)
	}

	// Verify that trailing content after '>' is not consumed.
	input3 := `<change>extra`
	_, consumed3, ok3 := TokenizeXMLTag(input3)
	if !ok3 {
		t.Fatal("expected ok")
	}
	if consumed3 != len("<change>") {
		t.Fatalf("expected consumed=%d, got %d", len("<change>"), consumed3)
	}
}

func TestTokenizeXMLTagStreaming(t *testing.T) {
	// Simulate streaming input: feed partial tags and verify they are
	// rejected until complete.
	parts := []string{
		`<change`,
		` op="`,
		`MODIFY"`,
		` target="Fo`,
		`o"`,
		` file-path="/test.go"`,
		`>`,
	}
	var accumulated string
	for i, part := range parts {
		accumulated += part
		_, _, ok := TokenizeXMLTag(accumulated)
		if i < len(parts)-1 {
			if ok {
				t.Fatalf("expected incomplete at part %d (accumulated=%q)", i, accumulated)
			}
		} else {
			if !ok {
				t.Fatal("expected complete at final part")
			}
		}
	}
}

func TestParseXMLOpeningTagWrapper(t *testing.T) {
	// Verify the wrapper maintains backward compatibility.
	kind, attrs, ok := parseXMLOpeningTag(`<change op="MODIFY" target="Foo" file-path="/test.go">`)
	if !ok {
		t.Fatal("expected ok")
	}
	if kind != "change" {
		t.Fatalf("expected kind change, got %s", kind)
	}
	if attrs["op"] != "MODIFY" {
		t.Fatalf("expected op=MODIFY, got %s", attrs["op"])
	}
	if attrs["target"] != "Foo" {
		t.Fatalf("expected target=Foo, got %s", attrs["target"])
	}
	if attrs["file-path"] != "/test.go" {
		t.Fatalf("expected file-path=/test.go, got %s", attrs["file-path"])
	}

	// Closing tag is rejected.
	_, _, ok = parseXMLOpeningTag(`</change>`)
	if ok {
		t.Fatal("expected closing tag to be rejected")
	}

	// Incomplete tag is rejected.
	_, _, ok = parseXMLOpeningTag(`<change op="MODIFY"`)
	if ok {
		t.Fatal("expected incomplete tag to be rejected")
	}
}

func TestParseXMLClosingTagWrapper(t *testing.T) {
	// Verify the wrapper maintains backward compatibility.
	kind, ok := parseXMLClosingTag(`</change>`)
	if !ok {
		t.Fatal("expected ok")
	}
	if kind != "change" {
		t.Fatalf("expected kind change, got %s", kind)
	}

	// Opening tag is rejected.
	_, ok = parseXMLClosingTag(`<change>`)
	if ok {
		t.Fatal("expected opening tag to be rejected")
	}

	// Incomplete closing tag is rejected.
	_, ok = parseXMLClosingTag(`</change`)
	if ok {
		t.Fatal("expected incomplete closing tag to be rejected")
	}
}
