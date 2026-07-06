package blocks

import (
	"strings"
	"testing"
)

func TestBoundaryBlockLineStart(t *testing.T) {
	// ::: not at beginning of line should not be recognized as a block start
	content1 := []byte("some text :::change 瑱魃\nop: MODIFY\ntarget: x\nfile-path: /x.go\n\nbody\n:::end 瑱魃\n")
	_, _, _, ok, err := ParseFirstBlock(content1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no block for mid-line start marker")
	}

	// :::end not at beginning of line: opening marker is valid but no
	// line-start end marker exists, so this is an unclosed block error.
	content2 := []byte(":::change 瑱魃\nop: MODIFY\ntarget: x\nfile-path: /x.go\n\nbody text:::end 瑱魃\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for unclosed block with mid-line end marker")
	}
	if ok {
		t.Fatal("expected no block for mid-line end marker")
	}

	// Properly placed markers (start and end at beginning of lines) should succeed
	content3 := []byte(":::change 瑱魃\nop: MODIFY\ntarget: x\nfile-path: /x.go\n\nbody\n:::end 瑱魃\n")
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
	content := []byte("some text :::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\" />\n\ninvalid body\n:::end 徕珑\n\n:::change 栢彣\n<change op=\"MODIFY\" target=\"Bar\" file-path=\"/b.go\" />\n\nfunc Bar() {}\n:::end 栢彣\n")
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
	if !strings.Contains(block.Body, "target=\"Bar\"") {
		t.Fatalf("expected body to contain 'target=\"Bar\"': %s", block.Body)
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
	content := []byte(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\" />\n\nfunc Foo() {}\n")
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
	// The non-matching :::end 栢彣 is treated as body content. Since no
	// matching :::end 徕珑 exists, the block is unclosed.
	content2 := []byte(":::change 徕珑\nbody\n:::end 栢彣\n")
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

func TestParseFirstBlockNonMatchingEndIsBodyContent(t *testing.T) {
	// A body containing a line-start :::end with a different boundary
	// is treated as body content. The block closes at the matching
	// :::end 徕珑 marker, and the non-matching :::end 栢彣 line is
	// preserved in the body.
	content := []byte(":::change 徕珑\nbody line 1\n:::end 栢彣\nbody line 2\n:::end 徕珑\n")
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
	if !strings.Contains(block.Body, ":::end 栢彣") {
		t.Fatalf("body should contain non-matching :::end as content: %q", block.Body)
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
	content := []byte(":::change 徕珑 extra stuff\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑 also extra\n")
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
	// Before the fix, this returned a BlockParseError because the parser
	// could not locate the end of the closing marker line without a newline.
	content := []byte(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑")
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
	if strings.Contains(block.Body, ":::end") {
		t.Fatalf("body should not contain the end marker: %q", block.Body)
	}
	if end != len(content) {
		t.Fatalf("expected end %d, got %d", len(content), end)
	}
}

func TestParseFirstBlockMultipleBlocksWithNoTrailingNewline(t *testing.T) {
	// Two blocks, the second ending without a trailing newline.
	content := []byte(":::change 徕珑\nfunc Foo() {}\n:::end 徕珑\n:::change 栢彣\nfunc Bar() {}\n:::end 栢彣")

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
	if strings.Contains(block2.Body, ":::end") {
		t.Fatalf("second body should not contain end marker: %q", block2.Body)
	}
	if end2 != len(remaining) {
		t.Fatalf("expected second end %d, got %d", len(remaining), end2)
	}
}

func TestParseFirstBlockNonMatchingEndNoTrailingNewline(t *testing.T) {
	// A non-matching end marker at the end without a trailing newline.
	// The block should remain unclosed because no matching :::end exists.
	content := []byte(":::change 徕珑\nbody\n:::end 栢彣")
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