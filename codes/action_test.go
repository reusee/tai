package codes

import (
	"os"
	"strings"
	"testing"

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
	// Valid with blank line
	content := "---change abc-def-gh\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\n\nfunc myFunc() {}\n\n---end abc-def-gh\n"
	h, start, end, ok := parseFirstBoundaryHunk([]byte(content))
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

	// Body line that looks like a header after all headers are set (no blank line)
	content2 := "---change x-y-z\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\nop: MODIFY // comment\nfunc myFunc() {}\n\n---end x-y-z\n"
	h2, _, _, ok2 := parseFirstBoundaryHunk([]byte(content2))
	if !ok2 {
		t.Fatal("expected ok for content2")
	}
	if h2.Op != "MODIFY" {
		t.Fatal("op should remain MODIFY, not overwritten")
	}
	if !strings.Contains(h2.Body, "op: MODIFY // comment") {
		t.Fatal("body should contain the header-like line")
	}

	// RENAME operation with empty body
	t.Run("RENAME", func(t *testing.T) {
		content := "---change 徕珑\nop: RENAME\ntarget: new.go\nfile-path: old.go\n\n---end 徕珑\n"
		h, _, _, ok := parseFirstBoundaryHunk([]byte(content))
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
}

func TestBoundaryBlockLineStart(t *testing.T) {
	// --- not at beginning of line should not be recognized as a block start
	content1 := []byte("some text ---change 瑱魃\nop: MODIFY\ntarget: x\nfile-path: /x.go\n\nbody\n---end 瑱魃\n")
	_, _, _, ok := ParseFirstBlock(content1, ParseBlockConfig{
		KnownHeaders:    []string{"op", "target", "file-path"},
		RequiredHeaders: []string{"op", "target", "file-path"},
	})
	if ok {
		t.Fatal("expected no block for mid-line start marker")
	}

	// ---end not at beginning of line should not be recognized
	content2 := []byte("---change 瑱魃\nop: MODIFY\ntarget: x\nfile-path: /x.go\n\nbody text---end 瑱魃\n")
	_, _, _, ok = ParseFirstBlock(content2, ParseBlockConfig{
		KnownHeaders:    []string{"op", "target", "file-path"},
		RequiredHeaders: []string{"op", "target", "file-path"},
	})
	if ok {
		t.Fatal("expected no block for mid-line end marker")
	}

	// Properly placed markers (start and end at beginning of lines) should succeed
	content3 := []byte("---change 瑱魃\nop: MODIFY\ntarget: x\nfile-path: /x.go\n\nbody\n---end 瑱魃\n")
	_, _, _, ok = ParseFirstBlock(content3, ParseBlockConfig{
		KnownHeaders:    []string{"op", "target", "file-path"},
		RequiredHeaders: []string{"op", "target", "file-path"},
	})
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

	h := Hunk{
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

	h := Hunk{
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