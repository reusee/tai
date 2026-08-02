package blocks

import (
	"strings"
	"testing"
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
	content2 := []byte("<<DELIM1 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\nDELIM1\n<<徕珑 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n徕珑\n")
	block, _, _, ok, err := ParseFirstBlock(content2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block with Han delimiter after skipping non-Han delimiter")
	}
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", block.Boundary)
	}
}

func TestBoundaryBlockLineStart(t *testing.T) {
	// << not at beginning of line should not be recognized as a block start
	content1 := []byte("some text <<徕珑 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody\n徕珑\n")
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
	content2 := []byte("<<徕珑 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody text徕珑\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for unclosed block with embedded delimiter")
	}
	if ok {
		t.Fatal("expected no block for embedded delimiter")
	}

	// Properly placed markers should succeed
	content3 := []byte("<<徕珑 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody\n徕珑\n")
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
	content := []byte("some text <<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\ninvalid body\n徕珑\n\n<<龘靐 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/b.go\">\nfunc Bar() {}\n龘靐\n")
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
	if block.Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", block.Boundary)
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
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n")
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
	if e.BlockKind != "change" || e.Boundary != "徕珑" {
		t.Fatalf("expected unclosed block kind=change boundary=徕珑, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}

	// Opening marker found but end marker has a different delimiter.
	// The non-matching 龘靐 line is treated as body content. Since no
	// matching 徕珑 line exists, the block is unclosed.
	content2 := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nbody\n龘靐\n")
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
	if e.BlockKind != "change" || e.Boundary != "徕珑" {
		t.Fatalf("expected unclosed block kind=change boundary=徕珑, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}
}

func TestParseFirstBlockUnclosedReturnsPositions(t *testing.T) {
	// Verify that start and end are set even for unclosed blocks, so
	// callers can skip past the opening marker and continue scanning.
	content := []byte("prose\n<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n")
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
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {\n\treturn\n}\n")
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
	if !strings.Contains(e.Content, "<<徕珑") {
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

func TestParseFirstBlockNonMatchingEndIsBodyContent(t *testing.T) {
	// A body containing a line with a different delimiter
	// is treated as body content. The block closes at the matching
	// 徕珑 line, and the non-matching 龘靐 line is
	// preserved in the body.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody line 1\n龘靐\nbody line 2\n徕珑\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Kind != "change" || block.Boundary != "徕珑" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", block.Kind, block.Boundary)
	}
	if !strings.Contains(block.Body, "龘靐") {
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
		{"徕珑", "徕珑"},
		{"徕珑 extra", "徕珑"},
		{" 徕珑 ", "徕珑"},
		{"徕珑\trest", "徕珑"},
		{"徕珑<change", "徕珑"},
		{"龘靐", "龘靐"},
		{"龘靐 <change op=\"MODIFY\"", "龘靐"},
		{"齉爩", "齉爩"},
		{"", ""},
		{" 徕珑 ", "徕珑"},
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
	content := []byte("<<徕珑 extra <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑\n")
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
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %q", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
}

func TestParseFirstBlockClosingLineWithTrailingContent(t *testing.T) {
	// The closing line must be the delimiter alone. Trailing content
	// causes the line to not match the delimiter, leaving the block unclosed.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑 extra\n")
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
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑")
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
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
	if strings.Contains(block.Body, "徕珑") {
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
	content := []byte("<<徕珑\nsummary body\n徕珑\n")
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
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %q", block.Boundary)
	}
	if block.Body != "summary body" {
		t.Fatalf("expected body 'summary body', got %q", block.Body)
	}
}

func TestParseFirstBlockMultipleBlocksWithNoTrailingNewline(t *testing.T) {
	// Two blocks, the second ending without a trailing newline.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n徕珑\n<<龘靐 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n龘靐")

	// First block
	block, _, end, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error for first block: %v", err)
	}
	if !ok {
		t.Fatal("expected first block to be found")
	}
	if block.Boundary != "徕珑" {
		t.Fatalf("expected first boundary 徕珑, got %s", block.Boundary)
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
	if block2.Boundary != "龘靐" {
		t.Fatalf("expected second boundary 龘靐, got %s", block2.Boundary)
	}
	if !strings.Contains(block2.Body, "func Bar() {}") {
		t.Fatalf("second body should contain code: %q", block2.Body)
	}
	if strings.Contains(block2.Body, "龘靐") {
		t.Fatalf("second body should not contain end marker: %q", block2.Body)
	}
	if end2 != len(remaining) {
		t.Fatalf("expected second end %d, got %d", len(remaining), end2)
	}
}

func TestParseBlocks(t *testing.T) {
	content := []byte("<<徕珑 <summary>\nfirst\n徕珑\n<<龘靐\nsecond\n龘靐\n")
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
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody\n龘靐")
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
	if e.BlockKind != "change" || e.Boundary != "徕珑" {
		t.Fatalf("expected unclosed block kind=change boundary=徕珑, got kind=%q boundary=%q", e.BlockKind, e.Boundary)
	}
}

func TestParseBlocksSkipsUnclosed(t *testing.T) {
	content := []byte("<<徕珑 <summary>\nunclosed\n<<龘靐\nclosed body\n龘靐\n")
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
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<徕珑 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n徕珑\nfunc Outer() {}\n徕珑\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Inner() {}") {
		t.Fatalf("body should contain inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain outer block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<徕珑") {
		t.Fatalf("body should contain inner block opening marker: %q", block.Body)
	}
}

func TestParseFirstBlockNestedDifferentDelimiter(t *testing.T) {
	// When the body contains a nested block with a different delimiter,
	// the nested block's closing marker pops the inner level. A
	// non-matching delimiter line at the outer level is body content.
	// See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<龘靐 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n龘靐\nfunc Outer() {}\n徕珑\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Inner() {}") {
		t.Fatalf("body should contain inner block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "func Outer() {}") {
		t.Fatalf("body should contain outer block body: %q", block.Body)
	}
	if !strings.Contains(block.Body, "龘靐") {
		t.Fatalf("body should contain inner block closing marker: %q", block.Body)
	}
}

func TestParseFirstBlockNestedMultipleLevels(t *testing.T) {
	// Multiple levels of nesting: outer > middle > inner. Each level's
	// closing marker pops one level. See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<龘靐 <change op=\"MODIFY\" target=\"Middle\" file-path=\"/middle.go\">\n<<齉爩 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n齉爩\nfunc Middle() {}\n龘靐\nfunc Outer() {}\n徕珑\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", block.Boundary)
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
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<some text without xml tag\nfunc Foo() {}\n徕珑\n")
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
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<some text with < chars> and stuff\n徕珑\n")
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
	// Without the trailing-content check, "黿鼍" would be pushed onto the
	// stack, and the outer block's closing marker "徕珑" would be
	// treated as body content (because stack top is "黿鼍", not "徕珑"),
	// causing the block to be incorrectly reported as unclosed.
	// See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\n<<黿鼍 <bar attr=\"value\"> some text\nfunc Foo() {}\n徕珑\n")
	block, _, _, ok, err := ParseFirstBlock(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block to be found")
	}
	if block.Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", block.Boundary)
	}
	if !strings.Contains(block.Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", block.Body)
	}
	if !strings.Contains(block.Body, "<<黿鼍") {
		t.Fatalf("body should contain the non-block line: %q", block.Body)
	}
}

func TestParseFirstBlockNestedUnclosedInnerBlock(t *testing.T) {
	// When the inner block is unclosed, the outer block is also
	// unclosed because the stack never returns to empty.
	// See TheoryOfNestedBlockParsing.
	content := []byte("<<徕珑 <change op=\"MODIFY\" target=\"Outer\" file-path=\"/outer.go\">\n<<徕珑 <change op=\"MODIFY\" target=\"Inner\" file-path=\"/inner.go\">\nfunc Inner() {}\n")
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
	// The delimiter policy mandates exactly two uncommon Chinese characters
	// per block. Every prompt that shows a block example must state this
	// policy, and must not display legacy English example delimiters that
	// the model would imitate verbatim. See TheoryOfBlockFormatGeneral.
	prompts := map[string]string{
		"BlockFormatSystemPrompt":     BlockFormatSystemPrompt,
		"BlockFormatRestatePrompt":    BlockFormatRestatePrompt,
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
		if !strings.Contains(prompt, "uncommon Chinese characters") {
			t.Fatalf("%s must mandate the two-uncommon-Chinese-characters delimiter policy", name)
		}
		for _, legacy := range []string{
			"<<DELIM1", "<<BLOCK1", "<<ENDBLOCK", "<<TESTEND",
			"<<SHELLEND", "<<ENDSUM", "<<CTXEND", "<<CHG1", "<<MEMEND",
		} {
			if strings.Contains(prompt, legacy) {
				t.Fatalf("%s must not display legacy example delimiter %s", name, legacy)
			}
		}
	}
}
