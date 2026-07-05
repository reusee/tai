package blocks

import (
	"strings"
	"testing"
)

func TestParseFirstBoundaryHunk(t *testing.T) {
	// Valid with XML metadata and blank line
	content := ":::change 测试\n<change op=\"MODIFY\" target=\"myFunc\" file-path=\"/file.go\" />\n\nfunc myFunc() {}\n\n:::end 测试\n"
	h, start, end, ok, err := ParseFirstBoundaryHunk([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "MODIFY" {
		t.Fatalf("expected MODIFY, got %s", h.Op)
	}
	if h.Target != "myFunc" {
		t.Fatalf("expected myFunc, got %s", h.Target)
	}
	if h.FilePath != "/file.go" {
		t.Fatalf("expected /file.go, got %s", h.FilePath)
	}
	if !strings.Contains(h.Body, "func myFunc() {}") {
		t.Fatal("body does not contain expected code")
	}
	expectedEnd := len(content)
	if end != expectedEnd {
		t.Fatalf("expected end %d, got %d", expectedEnd, end)
	}
	_ = start

	// Body content with header-like lines is preserved after the XML tag
	content2 := ":::change 边界\n<change op=\"MODIFY\" target=\"myFunc\" file-path=\"/file.go\" />\n\nop: MODIFY // comment in body\nfunc myFunc() {}\n\n:::end 边界\n"
	h2, _, _, ok2, err2 := ParseFirstBoundaryHunk([]byte(content2))
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if !ok2 {
		t.Fatal("expected ok for content2")
	}
	if h2.Op != "MODIFY" {
		t.Fatal("op should be MODIFY")
	}
	if !strings.Contains(h2.Body, "op: MODIFY // comment in body") {
		t.Fatal("body should contain the header-like line")
	}

	// RENAME operation with empty body
	t.Run("RENAME", func(t *testing.T) {
		content := ":::change 徕珑\n<change op=\"RENAME\" target=\"new.go\" file-path=\"old.go\" />\n\n:::end 徕珑\n"
		h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok")
		}
		if h.Op != "RENAME" {
			t.Fatalf("expected RENAME, got %s", h.Op)
		}
		if h.Target != "new.go" {
			t.Fatalf("expected new.go, got %s", h.Target)
		}
		if h.FilePath != "old.go" {
			t.Fatalf("expected old.go, got %s", h.FilePath)
		}
	})

	// Header-based (key-value) format is no longer supported
	t.Run("HeaderFormatRejected", func(t *testing.T) {
		content := ":::change 格式\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\n\nfunc myFunc() {}\n\n:::end 格式\n"
		_, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("header-based format should be rejected")
		}
	})
}

func TestParseFirstBoundaryHunkXML(t *testing.T) {
	content := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑\n"
	h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "MODIFY" {
		t.Fatalf("expected MODIFY, got %s", h.Op)
	}
	if h.Target != "Foo" {
		t.Fatalf("expected Foo, got %s", h.Target)
	}
	if h.FilePath != "/test.go" {
		t.Fatalf("expected /test.go, got %s", h.FilePath)
	}
	if h.Body != "func Foo() {}" {
		t.Fatalf("unexpected body: %q", h.Body)
	}
}

func TestParseFirstBoundaryHunkXMLRename(t *testing.T) {
	content := ":::change 徕珑\n<change op=\"RENAME\" target=\"new.go\" file-path=\"old.go\" />\n:::end 徕珑\n"
	h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "RENAME" || h.Target != "new.go" || h.FilePath != "old.go" {
		t.Fatalf("unexpected hunk: %+v", h)
	}
	if h.Body != "" {
		t.Fatalf("body should be empty, got %q", h.Body)
	}
}

func TestParseFirstBoundaryHunkWrite(t *testing.T) {
	content := ":::change 徕珑\n<change op=\"WRITE\" file-path=\"/test.go\" />\n\npackage x\n\nfunc New() {}\n:::end 徕珑\n"
	h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "WRITE" {
		t.Fatalf("expected WRITE, got %s", h.Op)
	}
	if h.FilePath != "/test.go" {
		t.Fatalf("expected /test.go, got %s", h.FilePath)
	}
	if !strings.Contains(h.Body, "package x") {
		t.Fatalf("body should contain package declaration: %q", h.Body)
	}
	if !strings.Contains(h.Body, "func New() {}") {
		t.Fatalf("body should contain func New: %q", h.Body)
	}
}

func TestParseFirstBoundaryHunkNoBlankLines(t *testing.T) {
	// Code body without blank lines before or after the XML tag
	content := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\nfunc Foo() {}\n:::end 徕珑\n"
	h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "MODIFY" {
		t.Fatalf("expected MODIFY, got %s", h.Op)
	}
	if h.Target != "Foo" {
		t.Fatalf("expected Foo, got %s", h.Target)
	}
	if h.Body != "func Foo() {}" {
		t.Fatalf("unexpected body: %q", h.Body)
	}
}