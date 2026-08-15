package blocks

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseFirstBlockRejectsNonHanDelimiter(t *testing.T) {
	// A delimiter that is not Unicode Han characters should not be
	// recognized as a block delimiter. The parser skips it and
	// continues searching for a valid block.
	content := []byte("<<DELIM1 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\nDELIM1\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for non-Han delimiter")
	}

	// Non-Han delimiter followed by a valid Han-delimited block:
	// the parser should skip the non-Han block and find the valid one.
	content2 := []byte("<<DELIM1 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\nDELIM1\n<<徕珑龘 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block with Han delimiter after skipping non-Han delimiter")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
	}
}

func TestParseFirstBlockRejectsTwoCharDelimiter(t *testing.T) {
	// A two-character Han delimiter does not satisfy the three-character
	// delimiter policy and must not be recognized as a block delimiter.
	// The parser skips it and continues searching for a valid block.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for two-character Han delimiter")
	}

	// Two-character delimiter followed by a valid three-character block:
	// the parser should skip the two-character marker and find the valid one.
	content2 := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑\n<<徕珑龘 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block with three-character delimiter after skipping two-character delimiter")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
	}
}

func TestBoundaryBlockLineStart(t *testing.T) {
	// << not at beginning of line should not be recognized as a block start
	content1 := []byte("some text <<徕珑龘 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody\n徕珑龘\n")
	_, _, _, ok, err := ParseFirstBlock(content1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for mid-line start marker")
	}

	// closing delimiter embedded in a line (not alone on its own line):
	// opening marker is valid but no matching closing line exists, so
	// this is an unclosed block error.
	content2 := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody text徕珑龘\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for unclosed block with embedded delimiter")
	}
	if ok {
		t.Fatal("expected no block for embedded delimiter")
	}

	// Properly placed markers should succeed
	content3 := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody\n徕珑龘\n")
	_, _, _, ok, err = ParseFirstBlock(content3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block for properly placed markers")
	}
}

func TestParseFirstBlockSkipMalformed(t *testing.T) {
	// Content with a malformed block (marker not at line start) followed by a valid block
	content := []byte("some text <<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\ninvalid body\n徕珑龘\n\n<<龘靐齉 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/b.go\">\nfunc Bar() {}\n龘靐齉\n")
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
	if block.Boundary != "龘靐齉" {
		t.Fatalf("expected boundary 龘靐齉, got %s", block.Boundary)
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
	// Opening marker at line start with no end marker at all
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n")
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
	if e.BlockKind != "change" || e.Boundary != "徕珑龘" {
		t.Fatalf("expected unclosed block kind=change boundary=徕珑龘, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}

	// Opening marker found but end marker has a different delimiter.
	// The non-matching 龘靐齉 line is treated as body content. Since no
	// matching 徕珑龘 line exists, the block is unclosed.
	content2 := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nbody\n龘靐齉\n")
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
	if e.BlockKind != "change" || e.Boundary != "徕珑龘" {
		t.Fatalf("expected unclosed block kind=change boundary=徕珑龘, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}
}

func TestParseFirstBlockUnclosedReturnsPositions(t *testing.T) {
	// Verify that start and end are set even for unclosed blocks, so
	// callers can skip past the opening marker and continue scanning.
	content := []byte("prose\n<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n")
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
	// Verify that skipping past the unclosed block allows finding
	// subsequent content.
	remaining := content[end:]
	if !strings.Contains(string(remaining), "func Foo() {}") {
		t.Fatalf("remaining content after skip should contain body text, got %q", remaining)
	}
}

func TestParseFirstBlockUnclosedIncludesContent(t *testing.T) {
	// When an unclosed block is detected, the BlockParseError must include
	// the full content from the opening marker to the end of the available
	// content, providing context for debugging truncated output.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {\n\treturn\n}\n")
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
	if !strings.Contains(e.Content, "<<徕珑龘") {
		t.Fatalf("Content should include the opening marker: %q", e.Content)
	}
	if !strings.Contains(e.Content, "func Foo()") {
		t.Fatalf("Content should include the partial body: %q", e.Content)
	}
	// The error message should include the content for user-facing display.
	if !strings.Contains(err.Error(), "func Foo()") {
		t.Fatalf("error message should include the partial content: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "Content parsed so far") {
		t.Fatalf("error message should include the 'Content parsed so far' label: %s", err.Error())
	}
}

func TestBlockParseErrorLineNumber(t *testing.T) {
	// The error must include the 1-based line number of the opening
	// marker so the model can locate the malformed block in its output,
	// especially when the content is truncated. See
	// TheoryOfParseErrorCollection.
	content := []byte("prose line\n<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n")
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
	// An opening marker whose line extends to EOF (no trailing newline)
	// is a truncated block: the closing marker must be alone on its own
	// line, which cannot exist after EOF. The parser must report an
	// unclosed-block error instead of silently treating the marker as
	// prose. See TheoryOfBlockFormat.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">")
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
	if e.BlockKind != "change" || e.Boundary != "徕珑龘" {
		t.Fatalf("expected unclosed block kind=change boundary=徕珑龘, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}

	// A kindless opening marker at EOF is also a truncated block.
	content2 := []byte("<<徕珑龘")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for truncated kindless opening at EOF")
	}
	if ok {
		t.Fatal("expected ok to be false for truncated kindless opening at EOF")
	}

	// Text at line start that is not a block opening (non-Han delimiter)
	// is still silently skipped at EOF.
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
	// A line with a valid three-character Han delimiter followed by a
	// malformed XML opening tag (missing '>') is a malformed block, not
	// prose. It must be reported as a parse error so the model can
	// correct it. See TheoryOfParseErrorCollection.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\"\nfunc Foo() {}\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for malformed opening tag")
	}
	if ok {
		t.Fatal("expected ok to be false for malformed opening tag")
	}
	e, isParseErr := err.(*BlockParseError)
	if !isParseErr {
		t.Fatalf("expected BlockParseError, got %T: %v", err, err)
	}
	if e.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %q", e.Boundary)
	}
	if e.BlockKind != "change" {
		t.Fatalf("expected block kind change, got %q", e.BlockKind)
	}
	if e.Line != 1 {
		t.Fatalf("expected line 1, got %d", e.Line)
	}
	if e.Reason == "" {
		t.Fatal("expected reason for malformed opening tag")
	}
	if !strings.Contains(err.Error(), "malformed block") {
		t.Fatalf("expected 'malformed block' in error, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "XML opening tag") {
		t.Fatalf("expected 'XML opening tag' in error, got: %s", err.Error())
	}
}

func TestBlockParseErrorCollisionHints(t *testing.T) {
	// A line where the delimiter appears with trailing text is not a
	// valid closing marker. The unclosed-block error should include a
	// hint pointing at the malformed line so the model can locate it
	// without scanning the entire body. See TheoryOfBoundaryUniqueness.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘 extra\n")
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
	if !strings.Contains(e.Hints[0], "徕珑龘 extra") {
		t.Fatalf("hint should contain the malformed line: %q", e.Hints[0])
	}
	if !strings.Contains(err.Error(), "hint:") {
		t.Fatalf("error should include the hint section: %s", err.Error())
	}

	// A line where the delimiter appears with leading text is also a
	// malformed closing marker.
	content2 := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\nend 徕珑龘\n")
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

	// No lines with the delimiter at the start or end: no hints.
	content3 := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n")
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
	// A very large unclosed block must produce an error message that omits
	// the middle of the content, keeping only a truncated head and tail.
	// Without truncation, the error message would be enormous and waste
	// context when fed back to the model for self-correction.
	middleMarker := "MIDDLE_CONTENT_MARKER"
	largeBody := strings.Repeat("x", 5000) + middleMarker + strings.Repeat("y", 5000)
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\n" + largeBody)
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
	// The middle of the content must be omitted from the error message.
	if strings.Contains(err.Error(), middleMarker) {
		t.Fatal("error message should not include the middle of a large block")
	}
	// The error message must indicate that the content was truncated.
	if !strings.Contains(err.Error(), "bytes omitted") {
		t.Fatalf("error message should indicate truncation: %s", err.Error())
	}
	// The opening marker must be preserved (it identifies the block).
	if !strings.Contains(err.Error(), "<<徕珑龘") {
		t.Fatalf("error message should preserve the opening marker: %s", err.Error())
	}
	// The tail must be preserved so the model sees where the content ended.
	if !strings.Contains(err.Error(), strings.Repeat("y", 10)) {
		t.Fatalf("error message should preserve the tail: %s", err.Error())
	}
}

func TestParseFirstBlockNonMatchingEndIsBodyContent(t *testing.T) {
	// A body containing a line with a different delimiter
	// is treated as body content. The block closes at the matching
	// 徕珑龘 line, and the non-matching 龘靐齉 line is
	// preserved in the body.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody line 1\n龘靐齉\nbody line 2\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "change" || block.Boundary != "徕珑龘" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", block.Kind, block.Boundary)
	}
	if !strings.Contains(block.Body, "龘靐齉") {
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
		{"徕珑龘", "徕珑龘"},
		{"徕珑龘 extra", "徕珑龘"},
		{" 徕珑龘 ", "徕珑龘"},
		{"徕珑龘\trest", "徕珑龘"},
		{"徕珑龘<change", "徕珑龘"},
		{"龘靐齉", "龘靐齉"},
		{"龘靐齉 <change op=\"MODIFY\"", "龘靐齉"},
		{"齉爩龖", "齉爩龖"},
		{"", ""},
		// Two-character Han delimiters are rejected (empty string returned)
		{"徕珑", ""},
		{"徕珑 extra", ""},
		{" 徕珑 ", ""},
		{"徕珑\trest", ""},
		{"徕珑<change", ""},
		// Four-character Han delimiters are rejected
		{"徕珑龘靐", ""},
		{" 徕珑龘靐 ", ""},
		{"徕珑龘靐<change", ""},
		// Non-Han delimiters are rejected (empty string returned)
		{"BLOCK1", ""},
		{"BLOCK1 extra", ""},
		{" DELIM ", ""},
		{"DELIM_X9K2", ""},
		{"TEST-END", ""},
		{"BLOCK1<BLOCK2", ""},
		{"BLOCK1\tBLOCK2", ""},
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
	// Trailing content after the delimiter on the opening line is ignored;
	// the delimiter is the text up to the first space or <.
	// The closing line must be the delimiter alone (with optional whitespace).
	content := []byte("<<徕珑龘 extra <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n")
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
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %q", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
}

func TestParseFirstBlockLeadingWhitespaceAfterMarker(t *testing.T) {
	// A block with leading whitespace between << and the delimiter
	// must be parsed correctly. extractDelimiter trims the input, so
	// the rest-of-line computation must also use the trimmed string
	// to avoid slicing into the middle of a multi-byte rune.
	content := []byte("<< 徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n")
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
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %q", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}

	// Multiple leading spaces.
	content2 := []byte("<<   龘靐齉 <summary>\n- done\n龘靐齉\n")
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
	if block2.Boundary != "龘靐齉" {
		t.Fatalf("expected boundary 龘靐齉, got %q", block2.Boundary)
	}
	if block2.Body != "- done" {
		t.Fatalf("expected body '- done', got %q", block2.Body)
	}

	// Kindless block with leading whitespace.
	content3 := []byte("<< 齉爩龖\nbody text\n齉爩龖\n")
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
	if block3.Boundary != "齉爩龖" {
		t.Fatalf("expected boundary 齉爩龖, got %q", block3.Boundary)
	}
	if block3.Body != "body text" {
		t.Fatalf("expected body 'body text', got %q", block3.Body)
	}
}

func TestParseFirstBlockClosingLineWithTrailingContent(t *testing.T) {
	// The closing line must be the delimiter alone. Trailing content
	// causes the line to not match the delimiter, leaving the block unclosed.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘 extra\n")
	_, _, _, ok, err := ParseFirstBlock(content)
	if err == nil {
		t.Fatal("expected error for unclosed block with trailing content on closing line")
	}
	if ok {
		t.Fatal("expected ok to be false for unclosed block")
	}
}

func TestParseFirstBlockEndMarkerNoTrailingNewline(t *testing.T) {
	// End marker at the very end of content without a trailing newline.
	// The block should be correctly parsed during streaming (non-final).
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘")
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
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
	if strings.Contains(block.Body, "徕珑龘") {
		t.Fatalf("body should not contain the end marker: %q", block.Body)
	}
	if end != len(content) {
		t.Fatalf("expected end %d, got %d", len(content), end)
	}
}

func TestParseFirstBlockWithoutXMLHeader(t *testing.T) {
	// A block whose opening marker omits the XML opening tag is parsed
	// with an empty Kind. Such blocks can only be located by iterating
	// all blocks, not by filtering by kind. See TheoryOfKindlessBlocks.
	content := []byte("<<徕珑龘\nsummary body\n徕珑龘\n")
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
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %q", block.Boundary)
	}
	if block.Body != "summary body" {
		t.Fatalf("expected body 'summary body', got %q", block.Body)
	}
}

func TestParseFirstBlockBareKind(t *testing.T) {
	// A kind without the XML opening tag — <<DELIMITER kind — is
	// accepted on equal footing with <<DELIMITER <kind ...>. The bare
	// kind yields no attributes; a marker line with trailing text still
	// takes its first token as the kind; a first token that is not an
	// XML name (e.g. Han text) leaves the block kindless.
	// See TheoryOfBareKinds.

	t.Run("Change", func(t *testing.T) {
		content := []byte("<<徕珑龘 change\nfunc Foo() {}\n徕珑龘\n")
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
		content := []byte("<<龘靐齉 summary\n- done\n龘靐齉\n")
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
		content := []byte("<<黿鼍爩 go-test\n-run\nTestFoo\n黿鼍爩\n")
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

	t.Run("TrailingText", func(t *testing.T) {
		// A marker line with trailing text after the bare kind still
		// yields that kind; the trailing text is ignored, matching the
		// lenient handling of trailing content after an XML opening tag.
		content := []byte("<<徕珑龘 summary be brief\n- done\n徕珑龘\n")
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

	t.Run("NonNameTokenStaysKindless", func(t *testing.T) {
		// A first token containing a non-XML-name character (Han text,
		// punctuation) does not become a kind; the block stays kindless.
		content := []byte("<<徕珑龘 中文\nbody\n徕珑龘\n")
		block, _, _, ok, err := ParseFirstBlock(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected block to be found")
		}
		if block.Kind != "" {
			t.Fatalf("expected kindless block, got %q", block.Kind)
		}
	})

	t.Run("ParseBlocks", func(t *testing.T) {
		// Bare-kind blocks compose with XML-kind blocks and are found in
		// order by ParseBlocks, each with its own kind.
		content := []byte("<<徕珑龘 summary\nfirst\n徕珑龘\n<<龘靐齉 change\nfunc Bar() {}\n龘靐齉\n")
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

func TestParseFirstBlockMultipleBlocksWithNoTrailingNewline(t *testing.T) {
	// Two blocks, the second ending without a trailing newline.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑龘\n<<龘靐齉 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n龘靐齉")

	// First block
	block, _, end, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error for first block: %v", err)
	}
	if !ok {
		t.Fatal("expected first block to be found")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected first boundary 徕珑龘, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("first body should contain code: %q", block.Body)
	}

	// Second block (from remaining content after first block)
	remaining := content[end:]
	block2, _, end2, ok2, err2 := ParseFirstBlock(remaining)
	if err2 != nil {
		t.Fatalf("unexpected error for second block: %v", err2)
	}
	if !ok2 {
		t.Fatal("expected second block to be found")
	}
	if block2.Boundary != "龘靐齉" {
		t.Fatalf("expected second boundary 龘靐齉, got %s", block2.Boundary)
	}
	if !strings.Contains(block2.Body, "func Bar() {}") {
		t.Fatalf("second body should contain code: %q", block2.Body)
	}
	if strings.Contains(block2.Body, "龘靐齉") {
		t.Fatalf("second body should not contain end marker: %q", block2.Body)
	}
	if end2 != len(remaining) {
		t.Fatalf("expected second end %d, got %d", len(remaining), end2)
	}
}

func TestParseBlocks(t *testing.T) {
	content := []byte("<<徕珑龘 <summary>\nfirst\n徕珑龘\n<<龘靐齉\nsecond\n龘靐齉\n")
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
	// A non-matching end marker at the end without a trailing newline.
	// The block should remain unclosed because no matching closing line exists.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody\n龘靐齉")
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
	if e.BlockKind != "change" || e.Boundary != "徕珑龘" {
		t.Fatalf("expected unclosed block kind=change boundary=徕珑龘, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}
}

func TestParseBlocksSkipsUnclosed(t *testing.T) {
	content := []byte("<<徕珑龘 <summary>\nunclosed\n<<龘靐齉\nclosed body\n龘靐齉\n")
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
	// When the body contains a nested block with the same delimiter,
	// the nested block's closing marker must not prematurely close
	// the outer block. See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<徕珑龘 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n徕珑龘\nfunc Outer() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Inner() {}") {
		t.Fatalf("body should contain inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain outer block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<徕珑龘") {
		t.Fatalf("body should contain inner block opening marker: %q", block.Body)
	}
}

func TestParseFirstBlockNestedSameDelimiterBareKind(t *testing.T) {
	// A nested block whose opening marker carries a bare kind — with the
	// same delimiter as the outer block — must not prematurely close the
	// outer block, mirroring the XML opening tag form.
	// See TheoryOfBareKinds.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<徕珑龘 summary\n- inner\n徕珑龘\nfunc Outer() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "change" || block.Boundary != "徕珑龘" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", block.Kind, block.Boundary)
	}
	if !strings.Contains(block.Body, "- inner") {
		t.Fatalf("body should contain the inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<徕珑龘 summary") {
		t.Fatalf("body should contain the inner opening marker: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain the outer block body: %q", block.Body)
	}
}

func TestParseFirstBlockNestedDifferentDelimiter(t *testing.T) {
	// When the body contains a line that looks like a nested block
	// opening with a different delimiter, it is treated as body content
	// rather than pushed onto the stack. The different-delimiter closing
	// line is also body content. The outer block closes at its own
	// delimiter. See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<龘靐齉 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n龘靐齉\nfunc Outer() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Inner() {}") {
		t.Fatalf("body should contain inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain outer block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "龘靐齉") {
		t.Fatalf("body should contain different-delimiter lines as content: %q", block.Body)
	}
}

func TestParseFirstBlockNestedDifferentDelimiterOpeningWithoutClosing(t *testing.T) {
	// A body line that starts with "<<" and contains a valid XML tag
	// but uses a DIFFERENT delimiter from the outer block must be
	// treated as body content, not a nested opening. If it were pushed
	// onto the stack, the outer block's closing marker would not match
	// the stack top and the block would be incorrectly reported as
	// unclosed. See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<龘靐齉 <tag>\nfunc Foo() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "<<龘靐齉 <tag>") {
		t.Fatalf("body should contain the different-delimiter opening as content: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
}

func TestParseFirstBlockNestedMultipleLevels(t *testing.T) {
	// Multiple levels of nesting: outer > middle > inner. Each level's
	// closing marker pops one level. See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<龘靐齉 <change op=\"MODIFY\" target=\"Middle\" file-path=\"/middle.go\">\n<<齉爩龖 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n齉爩龖\nfunc Middle() {}\n龘靐齉\nfunc Outer() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
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
	// A body line starting with "<<" that is not a valid block opening
	// (no XML tag after the delimiter) must not trigger nesting.
	// See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<some text without xml tag\nfunc Foo() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if !strings.Contains(block.Body, "<<some text without xml tag") {
		t.Fatalf("body should contain the non-block line: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
}

func TestParseFirstBlockNestedNotTriggeredByInvalidXML(t *testing.T) {
	// A body line starting with "<<" followed by text containing "<"
	// but not forming a valid XML opening tag must not trigger nesting.
	// See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<some text with < chars> and stuff\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if !strings.Contains(block.Body, "<<some text with < chars> and stuff") {
		t.Fatalf("body should contain the non-block line: %q", block.Body)
	}
}

func TestParseFirstBlockNestedNotTriggeredByTrailingContent(t *testing.T) {
	// A body line starting with "<<" followed by a valid XML tag but
	// with trailing content must not trigger nesting. The trailing
	// content indicates this is prose or code, not a real block opening.
	// Without the trailing-content check, "黿鼍爩" would be pushed onto the
	// stack, and the outer block's closing marker "徕珑龘" would be
	// treated as body content (because stack top is "黿鼍爩", not "徕珑龘"),
	// causing the block to be incorrectly reported as unclosed.
	// See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<黿鼍爩 <bar attr=\"value\"> some text\nfunc Foo() {}\n徕珑龘\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑龘" {
		t.Fatalf("expected boundary 徕珑龘, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<黿鼍爩") {
		t.Fatalf("body should contain the non-block line: %q", block.Body)
	}
}

func TestParseFirstBlockNestedUnclosedInnerBlock(t *testing.T) {
	// When the inner block is unclosed, the outer block is also
	// unclosed because the stack never returns to empty.
	// See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<徕珑龘 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n")
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
	// target. See TheoryOfBlockFormatGeneral.
	prompts := map[string]string{
		"ContinueBlockSystemPrompt":   ContinueBlockSystemPrompt,
		"ContinueBlockRestatePrompt":  ContinueBlockRestatePrompt,
		"GoTestBlockSystemPrompt":     GoTestBlockSystemPrompt,
		"GoTestBlockRestatePrompt":    GoTestBlockRestatePrompt,
		"ShellBlockSystemPrompt":      ShellBlockSystemPrompt,
		"ShellBlockRestatePrompt":     ShellBlockRestatePrompt,
		"SummaryBlockSystemPrompt":    SummaryBlockSystemPrompt,
		"SummaryBlockRestatePrompt":   SummaryBlockRestatePrompt,
		"RequestContextSystemPrompt":  RequestContextSystemPrompt,
		"RequestContextRestatePrompt": RequestContextRestatePrompt,
	}
	for name, prompt := range prompts {
		if strings.Contains(prompt, "<<DELIMITER") {
			t.Fatalf("%s displays the literal template marker '<<DELIMITER', which the model imitates verbatim; use a complete example with a concrete delimiter instead", name)
		}
	}
}

func TestPromptsUseUncommonChineseDelimiterPolicy(t *testing.T) {
	// The delimiter policy mandates exactly three uncommon Chinese characters
	// per block. Only the unified block format prompts state the policy;
	// kind prompts reference the general format and must not restate it.
	// See TheoryOfBlockFormatGeneral.
	policyPrompts := map[string]string{
		"BlockFormatSystemPrompt":  BlockFormatSystemPrompt,
		"BlockFormatRestatePrompt": BlockFormatRestatePrompt,
	}
	for name, prompt := range policyPrompts {
		if !strings.Contains(prompt, "three uncommon Chinese characters") {
			t.Fatalf("%s must mandate the three-uncommon-Chinese-characters delimiter policy", name)
		}
	}
	kindPrompts := map[string]string{
		"ContinueBlockSystemPrompt":   ContinueBlockSystemPrompt,
		"ContinueBlockRestatePrompt":  ContinueBlockRestatePrompt,
		"GoTestBlockSystemPrompt":     GoTestBlockSystemPrompt,
		"GoTestBlockRestatePrompt":    GoTestBlockRestatePrompt,
		"ShellBlockSystemPrompt":      ShellBlockSystemPrompt,
		"ShellBlockRestatePrompt":     ShellBlockRestatePrompt,
		"SummaryBlockSystemPrompt":    SummaryBlockSystemPrompt,
		"SummaryBlockRestatePrompt":   SummaryBlockRestatePrompt,
		"RequestContextSystemPrompt":  RequestContextSystemPrompt,
		"RequestContextRestatePrompt": RequestContextRestatePrompt,
	}
	for name, prompt := range kindPrompts {
		if strings.Contains(prompt, "three uncommon Chinese characters") {
			t.Fatalf("%s must not restate the delimiter policy; the unified BlockFormatSystemPrompt covers it", name)
		}
		for _, legacy := range []string{
			"<<DELIM1", "<<BLOCK1", "<<ENDBLOCK", "<<TESTEND",
			"<<SHELLEND", "<<ENDSUM", "<<CTXEND", "<<CHG1", "<<MEMEND",
			"<<徕珑 ", "<<龘靐 ", "<<黿鼍 ", "<<齉爩 ",
		} {
			if strings.Contains(prompt, legacy) {
				t.Fatalf("%s must not display legacy example delimiter %s", name, legacy)
			}
		}
	}
}
