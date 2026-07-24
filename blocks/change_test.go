package blocks

import (
	"strings"
	"testing"
)

func TestParseFirstBoundaryHunk(t *testing.T) {
	// Valid with XML attributes on opening tag and code body
	content := ":::测试 <change op=\"MODIFY\" target=\"myFunc\" file-path=\"/file.go\">\nfunc myFunc() {}\n:::测试 </change>\n"
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

	// Body content with header-like lines is preserved in the body
	content2 := ":::边界 <change op=\"MODIFY\" target=\"myFunc\" file-path=\"/file.go\">\nop: MODIFY // comment in body\nfunc myFunc() {}\n:::边界 </change>\n"
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
		content := ":::徕珑 <change op=\"RENAME\" target=\"new.go\" file-path=\"old.go\">\n:::徕珑 </change>\n"
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

	// Old header-based (key-value) format is no longer supported
	t.Run("HeaderFormatRejected", func(t *testing.T) {
		content := ":::格式 <change>\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\n\nfunc myFunc() {}\n:::格式 </change>\n"
		_, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The block is parsed but ParseChangeBlock returns false because op is missing
		// (the body is not parsed for metadata in the new format)
		if ok {
			t.Fatal("header-based format should be rejected (no op attribute on opening tag)")
		}
	})
}

func TestParseFirstBoundaryHunkXML(t *testing.T) {
	content := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n"
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
	content := ":::徕珑 <change op=\"RENAME\" target=\"new.go\" file-path=\"old.go\">\n:::徕珑 </change>\n"
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
	content := ":::徕珑 <change op=\"WRITE\" file-path=\"/test.go\">\npackage x\n\nfunc New() {}\n:::徕珑 </change>\n"
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

func TestParseFirstBoundaryHunkNonGoFileRestriction(t *testing.T) {
	// WRITE on non-Go file should succeed
	t.Run("WriteSucceeds", func(t *testing.T) {
		content := ":::徕珑 <change op=\"WRITE\" file-path=\"/test.md\">\n# Markdown\n:::徕珑 </change>\n"
		h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for WRITE on non-Go file")
		}
		if h.Op != "WRITE" {
			t.Fatalf("expected WRITE, got %s", h.Op)
		}
	})

	// MODIFY on non-Go file should fail
	t.Run("ModifyFails", func(t *testing.T) {
		content := ":::徕珑 <change op=\"MODIFY\" target=\"someHeading\" file-path=\"/test.md\">\n# Modified\n:::徕珑 </change>\n"
		_, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err == nil {
			t.Fatal("expected error for MODIFY on non-Go file")
		}
		if ok {
			t.Fatal("expected ok=false for MODIFY on non-Go file")
		}
	})

	// ADD_BEFORE on non-Go file should fail
	t.Run("AddBeforeFails", func(t *testing.T) {
		content := ":::徕珑 <change op=\"ADD_BEFORE\" target=\"someHeading\" file-path=\"/test.md\">\nnew content\n:::徕珑 </change>\n"
		_, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err == nil {
			t.Fatal("expected error for ADD_BEFORE on non-Go file")
		}
		if ok {
			t.Fatal("expected ok=false for ADD_BEFORE on non-Go file")
		}
	})

	// ADD_AFTER on non-Go file should fail
	t.Run("AddAfterFails", func(t *testing.T) {
		content := ":::徕珑 <change op=\"ADD_AFTER\" target=\"someHeading\" file-path=\"/test.md\">\nnew content\n:::徕珑 </change>\n"
		_, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err == nil {
			t.Fatal("expected error for ADD_AFTER on non-Go file")
		}
		if ok {
			t.Fatal("expected ok=false for ADD_AFTER on non-Go file")
		}
	})

	// DELETE with target=* on non-Go file should succeed
	t.Run("DeleteAllSucceeds", func(t *testing.T) {
		content := ":::徕珑 <change op=\"DELETE\" target=\"*\" file-path=\"/test.md\">\n:::徕珑 </change>\n"
		h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for DELETE * on non-Go file")
		}
		if h.Op != "DELETE" || h.Target != "*" {
			t.Fatalf("unexpected hunk: %+v", h)
		}
	})

	// DELETE with specific target on non-Go file should fail
	t.Run("DeleteSpecificFails", func(t *testing.T) {
		content := ":::徕珑 <change op=\"DELETE\" target=\"someHeading\" file-path=\"/test.md\">\n:::徕珑 </change>\n"
		_, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err == nil {
			t.Fatal("expected error for DELETE with specific target on non-Go file")
		}
		if ok {
			t.Fatal("expected ok=false for DELETE with specific target on non-Go file")
		}
	})

	// RENAME on non-Go file should succeed
	t.Run("RenameSucceeds", func(t *testing.T) {
		content := ":::徕珑 <change op=\"RENAME\" target=\"/new.md\" file-path=\"/old.md\">\n:::徕珑 </change>\n"
		h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for RENAME on non-Go file")
		}
		if h.Op != "RENAME" {
			t.Fatalf("expected RENAME, got %s", h.Op)
		}
	})

	// MODIFY on Go file should still succeed (regression check)
	t.Run("ModifyGoFileSucceeds", func(t *testing.T) {
		content := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n"
		h, _, _, ok, err := ParseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for MODIFY on Go file")
		}
		if h.Op != "MODIFY" {
			t.Fatalf("expected MODIFY, got %s", h.Op)
		}
	})
}
