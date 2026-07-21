package blocks

import (
	"strings"
	"testing"
)

func TestBoundaryBlockLineStart(t *testing.T) {
	// ::: not at beginning of line should not be recognized as a block start
	content1 := []byte("some text :::瑱魃 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody\n:::瑱魃 </change>\n")
	_, _, _, ok, err := ParseFirstBlock(content1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for mid-line start marker")
	}

	// closing marker not at beginning of line: opening marker is valid but no
	// line-start closing marker exists, so this is an unclosed block error.
	content2 := []byte(":::瑱魃 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody text:::瑱魃 </change>\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for unclosed block with mid-line end marker")
	}
	if ok {
		t.Fatal("expected no block for mid-line end marker")
	}

	// Properly placed markers (start and end at beginning of lines) should succeed
	content3 := []byte(":::瑱魃 <change op=\"MODIFY\" target=\"x\" file-path=\"/x.go\">\nbody\n:::瑱魃 </change>\n")
	_, _, _, ok, err = ParseFirstBlock(content3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected block for line-start markers")
	}
}

func TestParseFirstBlockSkipMalformed(t *testing.T) {
	// Content with a malformed block (marker not at line start) followed by a valid block
	content := []byte("some text :::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\ninvalid body\n:::徕珑 </change>\n\n:::栢彣 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/b.go\">\nfunc Bar() {}\n:::栢彣 </change>\n")
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
	if block.Boundary != "栢彣" {
		t.Fatalf("expected boundary 栢彣, got %s", block.Boundary)
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
	content := []byte(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n")
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

	// Opening marker found but end marker has a different boundary.
	// The non-matching :::栢彣 </change> is treated as body content. Since no
	// matching :::徕珑 </change> exists, the block is unclosed.
	content2 := []byte(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nbody\n:::栢彣 </change>\n")
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
	content := []byte("prose\n:::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n")
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

func TestParseFirstBlockNonMatchingEndIsBodyContent(t *testing.T) {
	// A body containing a line-start :::<boundary> with a different boundary
	// is treated as body content. The block closes at the matching
	// :::徕珑 </change> marker, and the non-matching :::栢彣 </change> line is
	// preserved in the body.
	content := []byte(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody line 1\n:::栢彣 </change>\nbody line 2\n:::徕珑 </change>\n")
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
	if !strings.Contains(block.Body, ":::栢彣 </change>") {
		t.Fatalf("body should contain non-matching closing marker as content: %q", block.Body)
	}
	if !strings.Contains(block.Body, "body line 1") || !strings.Contains(block.Body, "body line 2") {
		t.Fatalf("body should contain both body lines: %q", block.Body)
	}
}

func TestExtractHanBoundary(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"徕珑", "徕珑"},
		{"徕珑 extra", "徕珑"},
		{" 徕珑 ", "徕珑"},
		{"徕珑\n", "徕珑"},
		{"徕 栢", "徕"},
		{"徕珑(注)", "徕珑"},
		{"abc", ""},
		{"", ""},
		{" 徕", "徕"},
		{"徕", "徕"},
		// CJK Unified Ideographs Extension B (U+20000+) is part of the Han
		// script. unicode.Is(unicode.Han, r) accepts it, whereas the prior
		// manual range check (capped at U+FAFF) did not.
		{"\U00020000", "\U00020000"},
	}
	for _, tc := range tests {
		got := extractHanBoundary(tc.input)
		if got != tc.expected {
			t.Errorf("extractHanBoundary(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestParseFirstBlockTrailingBoundaryContent(t *testing.T) {
	// Trailing non-Han content after the boundary is ignored on both the
	// opening and closing markers; the boundary is the leading Han chars.
	content := []byte(":::徕珑 extra <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 extra </change>\n")
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

func TestParseFirstBlockEndMarkerNoTrailingNewline(t *testing.T) {
	// End marker at the very end of content without a trailing newline.
	// The block should be correctly parsed during streaming (non-final).
	content := []byte(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>")
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
	if strings.Contains(block.Body, ":::徕珑") {
		t.Fatalf("body should not contain the end marker: %q", block.Body)
	}
	if end != len(content) {
		t.Fatalf("expected end %d, got %d", len(content), end)
	}
}

func TestParseFirstBlockMultipleBlocksWithNoTrailingNewline(t *testing.T) {
	// Two blocks, the second ending without a trailing newline.
	content := []byte(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n:::栢彣 <change op=\"MODIFY\" target=\"Bar\" file-path=\"/test.go\">\nfunc Bar() {}\n:::栢彣 </change>")

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
	if block2.Boundary != "栢彣" {
		t.Fatalf("expected second boundary 栢彣, got %s", block2.Boundary)
	}
	if !strings.Contains(block2.Body, "func Bar() {}") {
		t.Fatalf("second body should contain code: %q", block2.Body)
	}
	if strings.Contains(block2.Body, ":::栢彣") {
		t.Fatalf("second body should not contain end marker: %q", block2.Body)
	}
	if end2 != len(remaining) {
		t.Fatalf("expected second end %d, got %d", len(remaining), end2)
	}
}

func TestParseFirstBlockNonMatchingEndNoTrailingNewline(t *testing.T) {
	// A non-matching end marker at the end without a trailing newline.
	// The block should remain unclosed because no matching closing marker exists.
	content := []byte(":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nbody\n:::栢彣 </change>")
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
