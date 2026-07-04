package codes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/taivm"
)

func TestExpandGoExprs(t *testing.T) {
	env := &taivm.Env{}
	env.Def("foo", "bar")
	env.Def("val", 42)

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `hello \go("world")`,
			expected: `hello world`,
		},
		{
			input:    `val is \go(val)`,
			expected: `val is 42`,
		},
		{
			input:    `add \go(val + 1)`,
			expected: `add 43`,
		},
		{
			input:    `complex strings \go("paren ) in string") and \go(foo)`,
			expected: `complex strings paren ) in string and bar`,
		},
		{
			input:    `unclosed \go(foo`,
			expected: `unclosed \go(foo`,
		},
	}

	for _, tc := range tests {
		got := expandGoExprs(tc.input, env)
		if got != tc.expected {
			t.Errorf("input: %s, got: %s, expected: %s", tc.input, got, tc.expected)
		}
	}
}

func TestParseFirstBoundaryHunk(t *testing.T) {
	// Valid with XML metadata and blank line
	content := ":::change abc-def-gh\n<change op=\"MODIFY\" target=\"myFunc\" file-path=\"/file.go\" />\n\nfunc myFunc() {}\n\n:::end abc-def-gh\n"
	h, start, end, ok, err := parseFirstBoundaryHunk([]byte(content))
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
	content2 := ":::change x-y-z\n<change op=\"MODIFY\" target=\"myFunc\" file-path=\"/file.go\" />\n\nop: MODIFY // comment in body\nfunc myFunc() {}\n\n:::end x-y-z\n"
	h2, _, _, ok2, err2 := parseFirstBoundaryHunk([]byte(content2))
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
		h, _, _, ok, err := parseFirstBoundaryHunk([]byte(content))
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
		content := ":::change abc-def-gh\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\n\nfunc myFunc() {}\n\n:::end abc-def-gh\n"
		_, _, _, ok, err := parseFirstBoundaryHunk([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("header-based format should be rejected")
		}
	})
}

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

func TestApplyHunkAddBeforeConstSpec(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nconst (\n\tbbb = 1\n\tccc = 2\n\tddd = 3\n)\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := codetypes.Hunk{
		Op:       "ADD_BEFORE",
		Target:   "ccc",
		FilePath: "test.go",
		Body:     "const aaa = 42",
	}
	if err := applyHunk(root, h); err != nil {
		t.Fatalf("applyHunk failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)

	if !strings.Contains(resultStr, "const aaa = 42") {
		t.Fatalf("result does not contain 'const aaa = 42':\n%s", resultStr)
	}
	aaaIdx := strings.Index(resultStr, "const aaa = 42")
	bbbIdx := strings.Index(resultStr, "bbb = 1")
	if aaaIdx == -1 || bbbIdx == -1 || aaaIdx > bbbIdx {
		t.Fatalf("'const aaa = 42' should appear before 'bbb = 1':\n%s", resultStr)
	}
	if !strings.Contains(resultStr, "ccc = 2") || !strings.Contains(resultStr, "ddd = 3") {
		t.Fatalf("const block should be intact:\n%s", resultStr)
	}
}

func TestApplyHunkRename(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := codetypes.Hunk{
		Op:       "RENAME",
		Target:   "newname.go",
		FilePath: "test.go",
	}
	if err := applyHunk(root, h); err != nil {
		t.Fatalf("applyHunk failed: %v", err)
	}

	// Old file must be gone
	_, err = root.Stat("test.go")
	if err == nil {
		t.Fatal("old file should not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected IsNotExist, got %v", err)
	}

	// New file must exist with original content
	_, err = root.Stat("newname.go")
	if err != nil {
		t.Fatalf("new file should exist: %v", err)
	}
	content, err := root.ReadFile("newname.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("expected %q, got %q", original, string(content))
	}
}

func TestParseFirstBoundaryHunkXML(t *testing.T) {
	content := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑\n"
	h, _, _, ok, err := parseFirstBoundaryHunk([]byte(content))
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
	h, _, _, ok, err := parseFirstBoundaryHunk([]byte(content))
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
	h, _, _, ok, err := parseFirstBoundaryHunk([]byte(content))
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
	h, _, _, ok, err := parseFirstBoundaryHunk([]byte(content))
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

func TestApplyHunkNoBlankLinesInBody(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nfunc Old() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := codetypes.Hunk{
		Op:       "MODIFY",
		Target:   "Old",
		FilePath: "test.go",
		Body:     "func New() {}",
	}
	if err := applyHunk(root, h); err != nil {
		t.Fatalf("applyHunk failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if strings.Contains(resultStr, "Old") {
		t.Fatalf("result should not contain Old:\n%s", resultStr)
	}
	if !strings.Contains(resultStr, "func New() {}") {
		t.Fatalf("result should contain New:\n%s", resultStr)
	}
}

func TestApplyHunkWrite(t *testing.T) {
	t.Run("ReplaceGoFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "package x\n\nfunc Old() {}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := codetypes.Hunk{
			Op:       "WRITE",
			FilePath: "test.go",
			Body:     "package x\n\nfunc New() {}\n",
		}
		if err := applyHunk(root, h); err != nil {
			t.Fatalf("applyHunk failed: %v", err)
		}

		result, err := root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		if strings.Contains(resultStr, "Old") {
			t.Fatalf("result should not contain Old:\n%s", resultStr)
		}
		if !strings.Contains(resultStr, "func New() {}") {
			t.Fatalf("result should contain New:\n%s", resultStr)
		}
	})

	t.Run("CreateGoFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		h := codetypes.Hunk{
			Op:       "WRITE",
			FilePath: "new.go",
			Body:     "package x\n\nfunc New() {}\n",
		}
		if err := applyHunk(root, h); err != nil {
			t.Fatalf("applyHunk failed: %v", err)
		}

		_, err = root.Stat("new.go")
		if err != nil {
			t.Fatalf("new file should exist: %v", err)
		}
	})

	t.Run("ReplaceNonGoFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "old content"
		if err := root.WriteFile("readme.md", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := codetypes.Hunk{
			Op:       "WRITE",
			FilePath: "readme.md",
			Body:     "# New Title\n\nNew content\n",
		}
		if err := applyHunk(root, h); err != nil {
			t.Fatalf("applyHunk failed: %v", err)
		}

		result, err := root.ReadFile("readme.md")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		if strings.Contains(resultStr, "old content") {
			t.Fatalf("result should not contain old content:\n%s", resultStr)
		}
		if !strings.Contains(resultStr, "# New Title") {
			t.Fatalf("result should contain new title:\n%s", resultStr)
		}
	})

	t.Run("CreateNonGoFileNested", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		h := codetypes.Hunk{
			Op:       "WRITE",
			FilePath: "sub/dir/notes.md",
			Body:     "# Notes\n\nSome content\n",
		}
		if err := applyHunk(root, h); err != nil {
			t.Fatalf("applyHunk failed: %v", err)
		}

		_, err = root.Stat("sub/dir/notes.md")
		if err != nil {
			t.Fatalf("file should exist: %v", err)
		}
	})
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

	// Opening marker found but end marker has a different boundary
	content2 := []byte(":::change 徕珑\nbody\n:::end 栢彣\n")
	_, _, _, ok, err = ParseFirstBlock(content2)
	if err == nil {
		t.Fatal("expected error for mismatched end marker boundary")
	}
	if ok {
		t.Fatal("expected ok to be false for mismatched end marker boundary")
	}
}

func TestApplyUnclosedBlockError(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	content := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\" />\n\nfunc Foo() {}\n"
	diffPath := filepath.Join(dir, "diff.txt")
	if err := os.WriteFile(diffPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	handler := BoundaryDiffHandler{}
	sawError := false
	for _, err := range handler.Apply(root, diffPath) {
		if err == nil {
			t.Fatal("expected error, got a hunk")
		}
		sawError = true
		if !strings.Contains(err.Error(), "unclosed") {
			t.Fatalf("expected unclosed block error, got: %v", err)
		}
	}
	if !sawError {
		t.Fatal("expected an error from Apply")
	}
}