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
	if !isParseErr || e.Mismatched {
		t.Fatalf("expected unclosed BlockParseError, got %T: %v", err, err)
	}

	// Opening marker found but end marker has a different boundary
	content2 := []byte(":::change 徕珑\nbody\n:::end 栢彣\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for mismatched end marker boundary")
	}
	if ok {
		t.Fatal("expected ok to be false for mismatched end marker boundary")
	}
	e, isParseErr = err.(*BlockParseError)
	if !isParseErr || !e.Mismatched {
		t.Fatalf("expected mismatched BlockParseError, got %T: %v", err, err)
	}
}