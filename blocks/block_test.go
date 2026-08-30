package blocks

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseFirstBlockRejectsNonHanDelimiter(t *testing.T) {
	content := []byte("<<DELIM1 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\nDELIM1\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for non-Han delimiter")
	}

	content2 := []byte("<<DELIM1 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\nDELIM1\n<<龘靐 change(op=\"MODIFY\", target=\"Bar\", file-path=\"/test.go\")\nfunc Bar() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block with Han delimiter after skipping non-Han delimiter")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
}

func TestParseFirstBlockRejectsThreeCharDelimiter(t *testing.T) {
	content := []byte("<<徕珑龘 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n徕珑龘\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for three-character Han delimiter")
	}

	content2 := []byte("<<徕珑龘 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n徕珑龘\n<<龘靐 change(op=\"MODIFY\", target=\"Bar\", file-path=\"/test.go\")\nfunc Bar() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block with two-character delimiter after skipping three-character delimiter")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
}

func TestBoundaryBlockLineStart(t *testing.T) {
	content1 := []byte("some text <<龘靐 change(op=\"MODIFY\", target=\"x\", file-path=\"/x.go\")\nbody\n龘靐\n")
	_, _, _, ok, err := ParseFirstBlock(content1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for mid-line start marker")
	}

	content2 := []byte("<<龘靐 change(op=\"MODIFY\", target=\"x\", file-path=\"/x.go\")\nbody text龘靐\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for unclosed block with embedded delimiter")
	}
	if ok {
		t.Fatal("expected no block for embedded delimiter")
	}

	content3 := []byte("<<龘靐 change(op=\"MODIFY\", target=\"x\", file-path=\"/x.go\")\nbody\n龘靐\n")
	_, _, _, ok, err = ParseFirstBlock(content3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block for properly placed markers")
	}
}

func TestParseFirstBlockSkipMalformed(t *testing.T) {
	content := []byte("some text <<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/f.go\")\ninvalid body\n龘靐\n\n<<齉爩 change(op=\"MODIFY\", target=\"Bar\", file-path=\"/b.go\")\nfunc Bar() {}\n齉爩\n")
	block, start, end, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected a valid block to be found")
	}
	if block.Kind != "change" {
		t.Fatalf("expected kind change, got %s", block.Kind)
	}
	if block.Boundary != "齉爩" {
		t.Fatalf("expected boundary 齉爩, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Bar() {}") {
		t.Fatalf("expected body to contain 'func Bar() {}': %s", block.Body)
	}
	if start < len("some text ") {
		t.Fatalf("expected first valid block to start after malformed one, start=%d", start)
	}
	if end != len(content) {
		t.Fatalf("expected block to consume entire remaining valid content, end=%d", end)
	}
}

func TestParseFirstBlockUnclosed(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/f.go\")\nfunc Foo() {}\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block with no end marker")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.BlockKind != "change" || e.Boundary != "龘靐" {
		t.Fatalf("expected unclosed block kind=change boundary=龘靐, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}

	content2 := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/f.go\")\nbody\n齉爩\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for unclosed block with non-matching end marker")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
	e, isParseErr = err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.BlockKind != "change" || e.Boundary != "龘靐" {
		t.Fatalf("expected unclosed block kind=change boundary=龘靐, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}
}

func TestParseFirstBlockUnclosedReturnsPositions(t *testing.T) {
	content := []byte("prose\n<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/f.go\")\nfunc Foo() {}\n")
	_, start, end, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
	if start == 0 {
		t.Fatal("expected non-zero start for unclosed block")
	}
	if end == 0 {
		t.Fatal("expected non-zero end for unclosed block")
	}
	if end <= start {
		t.Fatalf("expected end > start, got start=%d end=%d", start, end)
	}
	remaining := content[end:]
	if !strings.Contains(string(remaining), "func Foo() {}") {
		t.Fatalf("remaining content after skip should contain body text, got %q", remaining)
	}
}

func TestParseFirstBlockUnclosedIncludesContent(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/f.go\")\nfunc Foo() {\n\treturn\n}\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.Content == "" {
		t.Fatal("expected non-empty Content in BlockParseError")
	}
	if !strings.Contains(e.Content, "<<龘靐") {
		t.Fatalf("Content should include the opening marker: %q", e.Content)
	}
	if !strings.Contains(e.Content, "func Foo()") {
		t.Fatalf("Content should include the partial body: %q", e.Content)
	}
	if !strings.Contains(err.Error(), "func Foo()") {
		t.Fatalf("error message should include the partial content: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "Content parsed so far") {
		t.Fatalf("error message should include the 'Content parsed so far' label: %s", err.Error())
	}
}

func TestBlockParseErrorLineNumber(t *testing.T) {
	content := []byte("prose line\n<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/f.go\")\nfunc Foo() {}\n")
	_, _, _, _, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.Line != 2 {
		t.Fatalf("expected line 2, got %d", e.Line)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error message should include the line number, got: %s", err.Error())
	}
}

func TestParseFirstBlockUnclosedOpeningLineAtEOF(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for truncated opening line at EOF")
	}
	if ok {
		t.Fatal("expected ok to be false for truncated opening line at EOF")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.BlockKind != "change" || e.Boundary != "龘靐" {
		t.Fatalf("expected unclosed block kind=change boundary=龘靐, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}

	content2 := []byte("<<龘靐")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for truncated kindless opening at EOF")
	}
	if ok {
		t.Fatal("expected ok to be false for truncated kindless opening at EOF")
	}

	content3 := []byte("<<some text")
	_, _, _, ok, err = ParseFirstBlock(content3)
	if err != nil {
		t.Fatalf("unexpected error for non-block text at EOF: %v", err)
	}
	if ok {
		t.Fatal("expected no block for non-block text at EOF")
	}
}

func TestParseFirstBlockMalformedOpeningTag(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\"\nfunc Foo() {}\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for malformed opening header")
	}
	if ok {
		t.Fatal("expected ok to be false for malformed opening header")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %q", e.Boundary)
	}
	if e.BlockKind != "change" {
		t.Fatalf("expected block kind change, got %q", e.BlockKind)
	}
	if e.Line != 1 {
		t.Fatalf("expected line 1, got %d", e.Line)
	}
	if e.Reason == "" {
		t.Fatal("expected reason for malformed opening header")
	}
	if !strings.Contains(err.Error(), "malformed block") {
		t.Fatalf("expected 'malformed block' in error, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "function-call header") {
		t.Fatalf("expected 'function-call header' in error, got: %s", err.Error())
	}
}

func TestBlockParseErrorCollisionHints(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐 extra\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block with malformed closing line")
	}
	if ok {
		t.Fatal("expected ok to be false")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if len(e.Hints) != 1 {
		t.Fatalf("expected 1 hint, got %d: %v", len(e.Hints), e.Hints)
	}
	if !strings.Contains(e.Hints[0], "龘靐 extra") {
		t.Fatalf("hint should contain the malformed line: %q", e.Hints[0])
	}
	if !strings.Contains(err.Error(), "hint:") {
		t.Fatalf("error should include the hint section: %s", err.Error())
	}

	content2 := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\nend 龘靐\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for unclosed block with malformed closing line")
	}
	e, isParseErr = err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if len(e.Hints) != 1 {
		t.Fatalf("expected 1 hint, got %d: %v", len(e.Hints), e.Hints)
	}

	content3 := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")
	_, _, _, ok, err = ParseFirstBlock(content3)
	if err == nil {
		t.Fatal("expected error for unclosed block")
	}
	e, isParseErr = err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if len(e.Hints) != 0 {
		t.Fatalf("expected 0 hints, got %d: %v", len(e.Hints), e.Hints)
	}
}

func TestTruncateParseErrorContent(t *testing.T) {
	t.Run("WithinLimit", func(t *testing.T) {
		content := strings.Repeat("a", maxParseErrorContentLength)
		if got := truncateParseErrorContent(content); got != content {
			t.Fatal("content within the limit should be unchanged")
		}
	})

	t.Run("OverLimit", func(t *testing.T) {
		middleMarker := "MARKER_IN_MIDDLE"
		content := strings.Repeat("a", 3000) + middleMarker + strings.Repeat("b", 3000)
		got := truncateParseErrorContent(content)
		if strings.Contains(got, middleMarker) {
			t.Fatal("middle content should be omitted")
		}
		if !strings.Contains(got, "bytes omitted") {
			t.Fatal("truncation note should be present")
		}
		if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
			t.Fatal("head should be preserved")
		}
		if !strings.HasSuffix(got, strings.Repeat("b", 10)) {
			t.Fatal("tail should be preserved")
		}
		if len(got) > maxParseErrorContentLength+200 {
			t.Fatalf("truncated output should stay bounded, got %d bytes", len(got))
		}
	})

	t.Run("DoesNotSplitRunes", func(t *testing.T) {
		// 12000 bytes of 3-byte runes: the head and tail cuts fall inside
		// runes and must be adjusted to rune boundaries.
		content := strings.Repeat("世", 4000)
		got := truncateParseErrorContent(content)
		if !utf8.ValidString(got) {
			t.Fatal("truncated content must be valid UTF-8")
		}
	})
}

func TestBlockParseErrorTruncatesLargeContent(t *testing.T) {
	middleMarker := "MIDDLE_CONTENT_MARKER"
	largeBody := strings.Repeat("x", 5000) + middleMarker + strings.Repeat("y", 5000)
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/f.go\")\n" + largeBody)
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
	if strings.Contains(err.Error(), middleMarker) {
		t.Fatal("error message should not include the middle of a large block")
	}
	if !strings.Contains(err.Error(), "bytes omitted") {
		t.Fatalf("error message should indicate truncation: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "<<龘靐") {
		t.Fatalf("error message should preserve the opening marker: %s", err.Error())
	}
	if !strings.Contains(err.Error(), strings.Repeat("y", 10)) {
		t.Fatalf("error message should preserve the tail: %s", err.Error())
	}
}

func TestParseFirstBlockNonMatchingEndIsBodyContent(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nbody line 1\n齉爩\nbody line 2\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "change" || block.Boundary != "龘靐" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", block.Kind, block.Boundary)
	}
	if !strings.Contains(block.Body, "齉爩") {
		t.Fatalf("body should contain non-matching delimiter line as content: %q", block.Body)
	}
	if !strings.Contains(block.Body, "body line 1") || !strings.Contains(block.Body, "body line 2") {
		t.Fatalf("body should contain both body lines: %q", block.Body)
	}
}

func TestExtractDelimiter(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"龘靐", "龘靐"},
		{"龘靐 extra", "龘靐"},
		{" 龘靐 ", "龘靐"},
		{"龘靐\trest", "龘靐"},
		{"龘靐(change", "龘靐"},
		{"齉爩", "齉爩"},
		{"齉爩 change(op=\"MODIFY\")", "齉爩"},
		{"麤黿", "麤黿"},
		{"", ""},
		{"徕珑龘", ""},
		{"徕珑龘 extra", ""},
		{" 徕珑龘 ", ""},
		{"徕珑龘\trest", ""},
		{"徕珑龘(change", ""},
		{"徕珑龘靐", ""},
		{" 徕珑龘靐 ", ""},
		{"BLOCK1", ""},
		{"BLOCK1 extra", ""},
		{" DELIM ", ""},
		{"DELIM_X9K2", ""},
		{"TEST-END", ""},
		{"abc", ""},
	}
	for _, tc := range tests {
		got := extractDelimiter(tc.input)
		if got != tc.expected {
			t.Errorf("extractDelimiter(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestParseFirstBlockTrailingContent(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "change" {
		t.Fatalf("expected kind change, got %s", block.Kind)
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %q", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
}

func TestParseFirstBlockLenientAfterFullStop(t *testing.T) {
	// A marker glued directly after a Chinese full stop parses as a
	// block when the closing delimiter follows. See
	// TheoryOfLenientOpeningMarkers.
	content := []byte("Here is prose。<<龃龉 change(op=\"MODIFY\")\nbody\n龃龉\n")
	block, start, end, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected the lenient opening to parse as a block")
	}
	if block.Kind != "change" {
		t.Fatalf("kind = %q", block.Kind)
	}
	if block.Attributes["op"] != "MODIFY" {
		t.Fatalf("op = %q", block.Attributes["op"])
	}
	if block.Body != "body" {
		t.Fatalf("body = %q", block.Body)
	}
	if string(content[start:start+2]) != "<<" {
		t.Fatalf("start = %d, expected the << position", start)
	}
	if end != len(content) {
		t.Fatalf("end = %d, want %d", end, len(content))
	}

	// Without a closing delimiter the lenient opening is not a block and
	// not an error.
	block, _, _, ok, err = ParseFirstBlock([]byte("prose。<<龃龉 change(op=\"MODIFY\")\nno closing line\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("unexpected block: %+v", block)
	}

	// A malformed header after the full stop is skipped silently too.
	block, _, _, ok, err = ParseFirstBlock([]byte("prose。<<龃龉 change(bad\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("unexpected block: %+v", block)
	}

	// A marker preceded by other text (not a full stop) is still regular
	// content.
	block, _, _, ok, err = ParseFirstBlock([]byte("prose. <<龃龉 change(op=\"MODIFY\")\nbody\n龃龉\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("unexpected block: %+v", block)
	}

	// A lenient opening that never closes does not hide a later block.
	block, _, _, ok, err = ParseFirstBlock([]byte("prose。<<龃龉 change(op=\"MODIFY\")\nno close\n<<彳亍 summary\nok\n彳亍\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || block.Kind != "summary" {
		t.Fatalf("block = %+v, ok = %v", block, ok)
	}
	if block.Body != "ok" {
		t.Fatalf("body = %q", block.Body)
	}
}

func TestParseFirstBlockLenientAfterPunctuation(t *testing.T) {
	// A marker glued directly after any common Chinese or English
	// punctuation mark parses as a block when the closing delimiter
	// follows. See TheoryOfLenientOpeningMarkers.
	for _, mark := range lenientPunctuationMarks {
		content := []byte("prose" + mark + "<<龃龉 change(op=\"MODIFY\")\nbody\n龃龉\n")
		block, start, end, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("mark %q: %v", mark, err)
		}
		if !ok {
			t.Fatalf("mark %q: expected the lenient opening to parse as a block", mark)
		}
		if block.Kind != "change" || block.Attributes["op"] != "MODIFY" || block.Body != "body" {
			t.Fatalf("mark %q: block = %+v", mark, block)
		}
		if string(content[start:start+2]) != "<<" {
			t.Fatalf("mark %q: start = %d, expected the << position", mark, start)
		}
		if end != len(content) {
			t.Fatalf("mark %q: end = %d, want %d", mark, end, len(content))
		}
	}

	// A marker preceded by a space, a letter, or a digit stays regular
	// content: only punctuation admits the lenient form.
	for _, prefix := range []string{"prose ", "prose", "42"} {
		block, _, _, ok, err := ParseFirstBlock([]byte(prefix + "<<龃龉 change(op=\"MODIFY\")\nbody\n龃龉\n"))
		if err != nil {
			t.Fatalf("prefix %q: unexpected error: %v", prefix, err)
		}
		if ok {
			t.Fatalf("prefix %q: unexpected block: %+v", prefix, block)
		}
	}

	// A lenient opening after a non-full-stop mark that never closes is
	// not a block and not an error, and does not hide a later block.
	block, _, _, ok, err := ParseFirstBlock([]byte("prose.<<龃龉 change(op=\"MODIFY\")\nno close\n<<彳亍 summary\nok\n彳亍\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || block.Kind != "summary" || block.Body != "ok" {
		t.Fatalf("block = %+v, ok = %v", block, ok)
	}

	// A malformed header after the punctuation mark is skipped silently.
	block, _, _, ok, err = ParseFirstBlock([]byte("prose，<<龃龉 change(bad\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("unexpected block: %+v", block)
	}
}

func TestParseFirstBlockLeadingWhitespaceAfterMarker(t *testing.T) {
	content := []byte("<< 龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found with leading whitespace after <<")
	}
	if block.Kind != "change" {
		t.Fatalf("expected kind change, got %s", block.Kind)
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %q", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}

	content2 := []byte("<<   齉爩 summary\n- done\n齉爩\n")
	block2, _, _, ok2, err2 := ParseFirstBlock(content2)
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if !ok2 {
		t.Fatal("expected block to be found with multiple leading spaces")
	}
	if block2.Kind != "summary" {
		t.Fatalf("expected kind summary, got %s", block2.Kind)
	}
	if block2.Boundary != "齉爩" {
		t.Fatalf("expected boundary 齉爩, got %q", block2.Boundary)
	}
	if block2.Body != "- done" {
		t.Fatalf("expected body '- done', got %q", block2.Body)
	}

	content3 := []byte("<< 麤黿\nbody text\n麤黿\n")
	block3, _, _, ok3, err3 := ParseFirstBlock(content3)
	if err3 != nil {
		t.Fatalf("unexpected error: %v", err3)
	}
	if !ok3 {
		t.Fatal("expected kindless block to be found with leading whitespace")
	}
	if block3.Kind != "" {
		t.Fatalf("expected empty kind, got %q", block3.Kind)
	}
	if block3.Boundary != "麤黿" {
		t.Fatalf("expected boundary 麤黿, got %q", block3.Boundary)
	}
	if block3.Body != "body text" {
		t.Fatalf("expected body 'body text', got %q", block3.Body)
	}
}

func TestParseFirstBlockClosingLineWithTrailingContent(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐 extra\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block with trailing content on closing line")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
}

func TestParseFirstBlockLenientClosingMarker(t *testing.T) {
	open := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n"

	// The lenient closing form — the delimiter followed by ">>",
	// optionally separated by whitespace and surrounded by line
	// whitespace, with or without a trailing newline — closes the block
	// like the delimiter-alone line. See TheoryOfLenientClosingMarkers.
	for _, closing := range []string{"龘靐>>\n", "龘靐 >>\n", "  龘靐>>  \n", "龘靐>>"} {
		content := []byte(open + closing)
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("closing %q: unexpected error: %v", closing, err)
		}
		if !ok {
			t.Fatalf("closing %q: expected the block to parse", closing)
		}
		if block.Body != "func Foo() {}" {
			t.Fatalf("closing %q: unexpected body %q", closing, block.Body)
		}
	}

	// A single trailing ">" does not close the block: an incomplete
	// lenient form keeps the block unclosed until the line completes,
	// preserving the streaming guarantee. See TheoryOfLenientClosingMarkers.
	_, _, _, ok, err := ParseFirstBlock([]byte(open + "龘靐>"))
	if err == nil {
		t.Fatal("expected an unclosed-block error for a single trailing '>'")
	}
	if ok {
		t.Fatal("expected ok to be false for a single trailing '>'")
	}

	// The lenient form tolerates only the bare ">>": any further
	// trailing text still leaves the block unclosed.
	_, _, _, ok, err = ParseFirstBlock([]byte(open + "龘靐>> extra\n"))
	if err == nil {
		t.Fatal("expected an unclosed-block error for trailing text after '>>'")
	}
	if ok {
		t.Fatal("expected ok to be false for trailing text after '>>'")
	}
}

func TestParseFirstBlockEndMarkerNoTrailingNewline(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐")
	block, _, end, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "change" {
		t.Fatalf("expected kind change, got %s", block.Kind)
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
	if strings.Contains(block.Body, "龘靐") {
		t.Fatalf("body should not contain the end marker: %q", block.Body)
	}
	if end != len(content) {
		t.Fatalf("expected end %d, got %d", len(content), end)
	}
}

func TestParseFirstBlockWithoutXMLHeader(t *testing.T) {
	content := []byte("<<龘靐\nsummary body\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "" {
		t.Fatalf("expected empty kind, got %q", block.Kind)
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %q", block.Boundary)
	}
	if block.Body != "summary body" {
		t.Fatalf("expected body 'summary body', got %q", block.Body)
	}
}

func TestParseFirstBlockBareKind(t *testing.T) {
	t.Run("Change", func(t *testing.T) {
		content := []byte("<<龘靐 change\nfunc Foo() {}\n龘靐\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected block to be found")
		}
		if block.Kind != "change" {
			t.Fatalf("expected kind change, got %q", block.Kind)
		}
		if len(block.Attributes) != 0 {
			t.Fatalf("expected no attributes for a bare kind, got %v", block.Attributes)
		}
		if block.Body != "func Foo() {}" {
			t.Fatalf("expected body 'func Foo() {}', got %q", block.Body)
		}
	})

	t.Run("Summary", func(t *testing.T) {
		content := []byte("<<齉爩 summary\n- done\n齉爩\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected block to be found")
		}
		if block.Kind != "summary" {
			t.Fatalf("expected kind summary, got %q", block.Kind)
		}
		if block.Body != "- done" {
			t.Fatalf("expected body '- done', got %q", block.Body)
		}
	})

	t.Run("HyphenatedKind", func(t *testing.T) {
		content := []byte("<<爨虋 go-test\n-run\nTestFoo\n爨虋\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected block to be found")
		}
		if block.Kind != "go-test" {
			t.Fatalf("expected kind go-test, got %q", block.Kind)
		}
		if block.Body != "-run\nTestFoo" {
			t.Fatalf("unexpected body: %q", block.Body)
		}
	})

	t.Run("ParseBlocks", func(t *testing.T) {
		content := []byte("<<龘靐 summary\nfirst\n龘靐\n<<齉爩 change\nfunc Bar() {}\n齉爩\n")
		blocks, err := ParseBlocks(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(blocks))
		}
		if blocks[0].Kind != "summary" || blocks[0].Body != "first" {
			t.Fatalf("unexpected first block: %+v", blocks[0])
		}
		if blocks[1].Kind != "change" || blocks[1].Body != "func Bar() {}" {
			t.Fatalf("unexpected second block: %+v", blocks[1])
		}
	})
}

func TestParseFirstBlockAttributeOnly(t *testing.T) {
	t.Run("KindAttributeOnly", func(t *testing.T) {
		content := []byte("<<齉爩 kind=\"summary\"\n- done\n齉爩\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected block to be found")
		}
		if block.Kind != "summary" {
			t.Fatalf("expected kind summary, got %q", block.Kind)
		}
		if block.Attributes["kind"] != "summary" {
			t.Fatalf("expected kind attribute summary, got %q", block.Attributes["kind"])
		}
		if block.Body != "- done" {
			t.Fatalf("expected body '- done', got %q", block.Body)
		}
	})

	t.Run("KindAttributeWithParameters", func(t *testing.T) {
		content := []byte("<<龘靐 kind=\"change\" op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\"\nfunc Foo() {}\n龘靐\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected block to be found")
		}
		if block.Kind != "change" {
			t.Fatalf("expected kind change, got %q", block.Kind)
		}
		wantAttrs := map[string]string{
			"kind":      "change",
			"op":        "MODIFY",
			"target":    "Foo",
			"file-path": "/test.go",
		}
		for k, want := range wantAttrs {
			if block.Attributes[k] != want {
				t.Fatalf("expected attribute %q=%q, got %q", k, want, block.Attributes[k])
			}
		}
		if block.Body != "func Foo() {}" {
			t.Fatalf("expected body 'func Foo() {}', got %q", block.Body)
		}
	})

	t.Run("MissingKindAttributeIsMalformed", func(t *testing.T) {
		content := []byte("<<爨虋 op=\"MODIFY\"\nfunc Foo() {}\n爨虋\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err == nil {
			t.Fatal("expected a parse error for a parameter list without a kind attribute")
		}
		if ok {
			t.Fatalf("expected no block, got %+v", block)
		}
		parseErr, valid := err.(*BlockParseError)
		if !valid {
			t.Fatalf("expected *BlockParseError, got %T", err)
		}
		if parseErr.BlockKind != "op" {
			t.Fatalf("expected block kind op, got %q", parseErr.BlockKind)
		}
	})

	t.Run("NestedSameDelimiter", func(t *testing.T) {
		content := []byte("<<齉爩 kind=\"change\"\nouter start\n<<齉爩 kind=\"summary\"\ninner\n齉爩\nouter end\n齉爩\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected block to be found")
		}
		if block.Kind != "change" {
			t.Fatalf("expected kind change, got %q", block.Kind)
		}
		if block.Body != "outer start\n<<齉爩 kind=\"summary\"\ninner\n齉爩\nouter end" {
			t.Fatalf("unexpected body: %q", block.Body)
		}
	})

	t.Run("ParseBlocks", func(t *testing.T) {
		content := []byte("<<齉爩 kind=\"summary\"\nfirst\n齉爩\n<<龘靐 change(op=\"WRITE\")\nsecond\n龘靐\n")
		blocks, err := ParseBlocks(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(blocks))
		}
		if blocks[0].Kind != "summary" || blocks[0].Body != "first" {
			t.Fatalf("unexpected first block: %+v", blocks[0])
		}
		if blocks[1].Kind != "change" || blocks[1].Body != "second" {
			t.Fatalf("unexpected second block: %+v", blocks[1])
		}
	})
}

func TestParseFirstBlockMultipleBlocksWithNoTrailingNewline(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n<<齉爩 change(op=\"MODIFY\", target=\"Bar\", file-path=\"/test.go\")\nfunc Bar() {}\n齉爩")

	block, _, end, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error for first block: %v", err)
	}
	if !ok {
		t.Fatal("expected first block to be found")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected first boundary 龘靐, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("first body should contain code: %q", block.Body)
	}

	remaining := content[end:]
	block2, _, end2, ok2, err2 := ParseFirstBlock(remaining)
	if err2 != nil {
		t.Fatalf("unexpected error for second block: %v", err2)
	}
	if !ok2 {
		t.Fatal("expected second block to be found")
	}
	if block2.Boundary != "齉爩" {
		t.Fatalf("expected second boundary 齉爩, got %s", block2.Boundary)
	}
	if !strings.Contains(block2.Body, "func Bar() {}") {
		t.Fatalf("second body should contain code: %q", block2.Body)
	}
	if strings.Contains(block2.Body, "齉爩") {
		t.Fatalf("second body should not contain end marker: %q", block2.Body)
	}
	if end2 != len(remaining) {
		t.Fatalf("expected second end %d, got %d", len(remaining), end2)
	}
}

func TestParseBlocks(t *testing.T) {
	content := []byte("<<龘靐 summary\nfirst\n龘靐\n<<齉爩\nsecond\n齉爩\n")
	blocks, err := ParseBlocks(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Kind != "summary" || blocks[0].Body != "first" {
		t.Fatalf("unexpected first block: %+v", blocks[0])
	}
	if blocks[1].Kind != "" || blocks[1].Body != "second" {
		t.Fatalf("unexpected second block: %+v", blocks[1])
	}
}

func TestParseFirstBlockNonMatchingEndNoTrailingNewline(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nbody\n齉爩")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block with non-matching end marker at EOF")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.BlockKind != "change" || e.Boundary != "龘靐" {
		t.Fatalf("expected unclosed block kind=change boundary=龘靐, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}
}

func TestParseBlocksSkipsUnclosed(t *testing.T) {
	content := []byte("<<龘靐 summary\nunclosed\n<<齉爩\nclosed body\n齉爩\n")
	blocks, err := ParseBlocks(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "" || blocks[0].Body != "closed body" {
		t.Fatalf("unexpected block: %+v", blocks[0])
	}
}

func TestParseFirstBlockNestedSameDelimiter(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Outer\", file-path=\"/outer.go\")\n<<龘靐 change(op=\"MODIFY\", target=\"Inner\", file-path=\"/inner.go\")\nfunc Inner() {}\n龘靐\nfunc Outer() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Inner() {}") {
		t.Fatalf("body should contain inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain outer block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<龘靐") {
		t.Fatalf("body should contain inner block opening marker: %q", block.Body)
	}
}

func TestParseFirstBlockNestedSameDelimiterBareKind(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Outer\", file-path=\"/outer.go\")\n<<龘靐 summary\n- inner\n龘靐\nfunc Outer() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "change" || block.Boundary != "龘靐" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", block.Kind, block.Boundary)
	}
	if !strings.Contains(block.Body, "- inner") {
		t.Fatalf("body should contain the inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<龘靐 summary") {
		t.Fatalf("body should contain the inner opening marker: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain the outer block body: %q", block.Body)
	}
}

func TestParseFirstBlockNestedDifferentDelimiter(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Outer\", file-path=\"/outer.go\")\n<<齉爩 change(op=\"MODIFY\", target=\"Inner\", file-path=\"/inner.go\")\nfunc Inner() {}\n齉爩\nfunc Outer() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Inner() {}") {
		t.Fatalf("body should contain inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain outer block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "齉爩") {
		t.Fatalf("body should contain different-delimiter lines as content: %q", block.Body)
	}
}

func TestParseFirstBlockNestedDifferentDelimiterOpeningWithoutClosing(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\n<<齉爩 tag\nfunc Foo() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "<<齉爩 tag") {
		t.Fatalf("body should contain the different-delimiter opening as content: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
}

func TestParseFirstBlockNestedMultipleLevels(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Outer\", file-path=\"/outer.go\")\n<<齉爩 change(op=\"MODIFY\", target=\"Middle\", file-path=\"/middle.go\")\n<<麤黿 change(op=\"MODIFY\", target=\"Inner\", file-path=\"/inner.go\")\nfunc Inner() {}\n麤黿\nfunc Middle() {}\n齉爩\nfunc Outer() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Inner() {}") {
		t.Fatalf("body should contain innermost body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Middle() {}") {
		t.Fatalf("body should contain middle body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain outer body: %q", block.Body)
	}
}

func TestParseFirstBlockNestedNotTriggeredByNonBlockContent(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\n<<some text without header\nfunc Foo() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if !strings.Contains(block.Body, "<<some text without header") {
		t.Fatalf("body should contain the non-block line: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
}

func TestParseFirstBlockNestedNotTriggeredByInvalidXML(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\n<<some text with ( chars) and stuff\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if !strings.Contains(block.Body, "<<some text with ( chars) and stuff") {
		t.Fatalf("body should contain the non-block line: %q", block.Body)
	}
}

func TestParseFirstBlockNestedNotTriggeredByTrailingContent(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\n<<爨虋 bar(attr=\"value\") some text\nfunc Foo() {}\n龘靐\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<爨虋") {
		t.Fatalf("body should contain the non-block line: %q", block.Body)
	}
}

func TestParseFirstBlockNestedUnclosedInnerBlock(t *testing.T) {
	content := []byte("<<龘靐 change(op=\"MODIFY\", target=\"Outer\", file-path=\"/outer.go\")\n<<龘靐 change(op=\"MODIFY\", target=\"Inner\", file-path=\"/inner.go\")\nfunc Inner() {}\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed inner block")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed inner block")
	}
}

func TestKindPromptsNoLiteralDelimiterTemplate(t *testing.T) {
	// Kind-specific prompts must not display the literal template marker
	// "<<DELIMITER". The model imitates kind templates verbatim, and a
	// literal placeholder produces blocks with a non-unique delimiter.
	// Kind prompts must instead show complete examples with concrete
	// delimiters, so the model has a correct header/footer imitation
	// target. The Go-specific kind prompts (go-test, go-src) are covered
	// by the equivalent guard in the gotools package. See
	// TheoryOfBlockFormatGeneral.
	prompts := map[string]string{
		"ContinueBlockSystemPrompt": ContinueBlockSystemPrompt,
		"ShellBlockSystemPrompt":    ShellBlockSystemPrompt,
		"SummaryBlockSystemPrompt":  SummaryBlockSystemPrompt,
		"IngestBlockSystemPrompt":   IngestBlockSystemPrompt,
	}
	for name, prompt := range prompts {
		if strings.Contains(prompt, "<<DELIMITER") {
			t.Fatalf("%s displays the literal template marker '<<DELIMITER', which the model imitates verbatim; use a complete example with a concrete delimiter instead", name)
		}
	}
}

func TestPromptsUseUncommonChineseDelimiterPolicy(t *testing.T) {
	// The delimiter policy mandates an uncommon Chinese two-character word
	// per block. Only the unified block format prompt states the policy;
	// kind prompts reference the general format and must not restate it.
	// The Go-specific kind prompts (go-test, go-src) live in the gotools
	// package and are covered by its equivalent guard. See
	// TheoryOfBlockFormatGeneral.
	if !strings.Contains(BlockFormatSystemPrompt, "uncommon Chinese two-character word") {
		t.Fatal("BlockFormatSystemPrompt must mandate the uncommon-Chinese-two-character-word delimiter policy")
	}
	kindPrompts := map[string]string{
		"ContinueBlockSystemPrompt": ContinueBlockSystemPrompt,
		"ShellBlockSystemPrompt":    ShellBlockSystemPrompt,
		"SummaryBlockSystemPrompt":  SummaryBlockSystemPrompt,
		"IngestBlockSystemPrompt":   IngestBlockSystemPrompt,
	}
	for name, prompt := range kindPrompts {
		if strings.Contains(prompt, "uncommon Chinese two-character word") {
			t.Fatalf("%s must not restate the delimiter policy; the unified BlockFormatSystemPrompt covers it", name)
		}
		for _, legacy := range []string{
			"<<DELIM1", "<<BLOCK1", "<<ENDBLOCK", "<<TESTEND",
			"<<SHELLEND", "<<ENDSUM", "<<CTXEND", "<<CHG1", "<<MEMEND",
			"<<徕珑龘 ", "<<龘靐齉 ", "<<黿鼍爩 ", "<<齉爩龖 ",
		} {
			if strings.Contains(prompt, legacy) {
				t.Fatalf("%s must not display legacy example delimiter %s", name, legacy)
			}
		}
	}
}

func TestSummaryPromptsClosingSelfCheck(t *testing.T) {
	// The summary prompt must teach a mechanical closing self-check —
	// the same emission-time technique the block format prompt uses for
	// the line-start rule — because a model that ends its response on
	// a non-summary block (e.g., a go-src block) violates the
	// every-response summary requirement without noticing. The check
	// names the consequence: the response is discarded and retried, so
	// none of its blocks take effect. The prompt must also cover the
	// fetch-only response shape — a response whose only blocks are
	// ingest, go-src, shell, or go-test blocks still requires the summary
	// — and state why the summary is non-omittable: the system reads it
	// as its only proof that the response was generated completely and
	// followed the rules. See TheoryOfSummaryBlocks.
	if !strings.Contains(SummaryBlockSystemPrompt, "Closing self-check") {
		t.Fatal("system prompt must teach the closing self-check")
	}
	if !strings.Contains(SummaryBlockSystemPrompt, "No other block kind can close a response") {
		t.Fatal("system prompt must state that no other block kind can close a response")
	}
	if !strings.Contains(SummaryBlockSystemPrompt, "ingest, go-src, shell, or go-test blocks") {
		t.Fatal("system prompt must cover the fetch-only response shape: a response whose only blocks are fetching kinds still requires the summary")
	}
	if !strings.Contains(SummaryBlockSystemPrompt, "never omittable") {
		t.Fatal("system prompt must state that the summary is never omittable")
	}
}

func TestBlockFormatPromptsNoNegativeExamples(t *testing.T) {
	// Negative examples are deliberately omitted from the block format
	// prompt: a model may imitate a displayed bad pattern, so the
	// prompt states the rules directly and shows only a correct example.
	// See TheoryOfBlockFormatGeneral.
	if !strings.Contains(BlockFormatSystemPrompt, "Do this") {
		t.Fatal("BlockFormatSystemPrompt must keep the correct example")
	}
	if strings.Contains(BlockFormatSystemPrompt, "NOT this") {
		t.Fatal("BlockFormatSystemPrompt must not display a negative example")
	}
	if strings.Contains(BlockFormatSystemPrompt, "Writing \"<<DELIMITER\"") {
		t.Fatal("BlockFormatSystemPrompt must not display the forbidden literal pattern")
	}
}

func TestBlockFormatPromptsStateDeferredExecution(t *testing.T) {
	// The deferred-execution contract is centralized in the unified block
	// format prompt: blocks are processed only after the response ends,
	// and results arrive as user content in the next round. Kind prompts
	// may state kind-specific arrival details but must not restate the
	// general principle. See TheoryOfDeferredExecution.
	if !strings.Contains(BlockFormatSystemPrompt, "Blocks Are Not Tool Calls") {
		t.Fatal("BlockFormatSystemPrompt must state the not-tool-calls contract")
	}
	if !strings.Contains(BlockFormatSystemPrompt, "hallucination") {
		t.Fatal("BlockFormatSystemPrompt must name the fabricated-result risk")
	}
	if !strings.Contains(BlockFormatSystemPrompt, "NEXT round") {
		t.Fatal("BlockFormatSystemPrompt must state that block results arrive in the NEXT round")
	}
}

func TestBlockFormatPromptsLineStartSelfCheck(t *testing.T) {
	// The line-start rule must be stated as a mechanical emission-time
	// self-check, not only as an abstract placement rule: models
	// occasionally emit blocks mid-line, and the parser discards them.
	// See TheoryOfBlockFormatGeneral.
	if !strings.Contains(BlockFormatSystemPrompt, "Before emitting") {
		t.Fatal("BlockFormatSystemPrompt must instruct a self-check before emitting the opening marker")
	}
	if !strings.Contains(BlockFormatSystemPrompt, "newline first") {
		t.Fatal("BlockFormatSystemPrompt must instruct emitting a newline first when the preceding character is not a newline")
	}
}

func TestBlockFormatPromptsAreKindAgnostic(t *testing.T) {
	// The block format prompt must stay kind-agnostic: third-party
	// programs may embed it without providing any tai-implemented
	// kind, so it describes only the format and universally applicable
	// rules. Kind semantics live in the kind prompts.
	// See TheoryOfBlockFormatGeneral.
	kindReferences := []string{
		"go-test",
		"go-src",
		"ingest block",
		"shell",
		"summary block",
		"continue block",
		"change(",
		"changes will be lost",
		"command output",
	}
	for _, ref := range kindReferences {
		if strings.Contains(BlockFormatSystemPrompt, ref) {
			t.Fatalf("BlockFormatSystemPrompt must not reference kind-specific semantics %q; see TheoryOfBlockFormatGeneral", ref)
		}
	}
}
