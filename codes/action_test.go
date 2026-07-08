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

func TestApplyFinishBlock(t *testing.T) {
	t.Run("AtEnd", func(t *testing.T) {
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

		content := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\" />\n\nfunc New() {}\n:::end 徕珑\n\n:::finish 栢彣\nRenamed Old to New.\n:::end 栢彣\n"
		diffPath := filepath.Join(dir, "diff.txt")
		if err := os.WriteFile(diffPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		handler := BoundaryDiffHandler{}
		count := 0
		for _, err := range handler.Apply(root, diffPath) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("expected 1 hunk, got %d", count)
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

	t.Run("BeforeChange", func(t *testing.T) {
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

		// finish block before change block — should be skipped and change still applied
		content := ":::finish 栢彣\nRenamed Old to New.\n:::end 栢彣\n\n:::change 徕珑\n<change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\" />\n\nfunc New() {}\n:::end 徕珑\n"
		diffPath := filepath.Join(dir, "diff.txt")
		if err := os.WriteFile(diffPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		handler := BoundaryDiffHandler{}
		count := 0
		for _, err := range handler.Apply(root, diffPath) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("expected 1 hunk, got %d", count)
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
}

func TestApplyPreservesNonChangeBlocks(t *testing.T) {
	run := func(t *testing.T, content string) {
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

		diffPath := filepath.Join(dir, "diff.txt")
		if err := os.WriteFile(diffPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		handler := BoundaryDiffHandler{}
		count := 0
		for _, err := range handler.Apply(root, diffPath) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("expected 1 hunk, got %d", count)
		}

		remaining, err := os.ReadFile(diffPath)
		if err != nil {
			t.Fatal(err)
		}
		remainingStr := string(remaining)
		if strings.Contains(remainingStr, "徕珑") {
			t.Fatalf("applied change block should be removed from diff file:\n%s", remainingStr)
		}
		if !strings.Contains(remainingStr, "Renamed Old to New.") {
			t.Fatalf("finish block should be preserved in diff file:\n%s", remainingStr)
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

	changeBlock := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\" />\n\nfunc New() {}\n:::end 徕珑\n"
	finishBlock := ":::finish 栢彣\nRenamed Old to New.\n:::end 栢彣\n"

	t.Run("ChangeThenFinish", func(t *testing.T) {
		run(t, changeBlock+"\n"+finishBlock)
	})
	t.Run("FinishThenChange", func(t *testing.T) {
		run(t, finishBlock+"\n"+changeBlock)
	})
}

func TestApplyHunkMultiEntityRemovesDuplicates(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\n" +
		"type Foo struct {\n\tBar int\n}\n\n" +
		"func (f *Foo) GetBar() int {\n\treturn f.Bar\n}\n\n" +
		"func (f *Foo) SetBar(b int) {\n\tf.Bar = b\n}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	body := "type Foo struct {\n\tBar int\n\tBaz int\n}\n\n" +
		"func (f *Foo) GetBar() int {\n\treturn f.Bar\n}\n\n" +
		"func (f *Foo) SetBar(b int) {\n\tf.Bar = b\n}\n"
	h := codetypes.Hunk{
		Op:       "MODIFY",
		Target:   "Foo",
		FilePath: "test.go",
		Body:     body,
	}
	if err := applyHunk(root, h); err != nil {
		t.Fatalf("applyHunk failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)

	if !strings.Contains(resultStr, "Baz int") {
		t.Fatalf("result should contain Baz field:\n%s", resultStr)
	}
	if count := strings.Count(resultStr, "type Foo struct"); count != 1 {
		t.Fatalf("expected 1 Foo type, got %d:\n%s", count, resultStr)
	}
	if count := strings.Count(resultStr, "func (f *Foo) GetBar()"); count != 1 {
		t.Fatalf("expected 1 GetBar method, got %d:\n%s", count, resultStr)
	}
	if count := strings.Count(resultStr, "func (f *Foo) SetBar(b int)"); count != 1 {
		t.Fatalf("expected 1 SetBar method, got %d:\n%s", count, resultStr)
	}
}

func TestApplyHunkTrailingNewlineConsistentWithGoFmt(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	assertSingleTrailingNewline := func(t *testing.T, content []byte) {
		t.Helper()
		if len(content) == 0 {
			t.Fatal("content is empty")
		}
		if content[len(content)-1] != '\n' {
			t.Fatalf("content must end with '\\n', got: %q", string(content))
		}
		if len(content) >= 2 && content[len(content)-2] == '\n' {
			t.Fatalf("content must end with exactly one '\\n', got: %q", string(content))
		}
	}

	t.Run("Modify", func(t *testing.T) {
		original := "package x\n\nfunc Old() {}\n"
		if err := root.WriteFile("modify.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := codetypes.Hunk{
			Op:       "MODIFY",
			Target:   "Old",
			FilePath: "modify.go",
			Body:     "func New() {}",
		}
		if err := applyHunk(root, h); err != nil {
			t.Fatalf("applyHunk failed: %v", err)
		}

		result, err := root.ReadFile("modify.go")
		if err != nil {
			t.Fatal(err)
		}
		assertSingleTrailingNewline(t, result)
	})

	t.Run("WriteGo", func(t *testing.T) {
		h := codetypes.Hunk{
			Op:       "WRITE",
			FilePath: "write.go",
			Body:     "package x\n\nfunc New() {}\n",
		}
		if err := applyHunk(root, h); err != nil {
			t.Fatalf("applyHunk failed: %v", err)
		}

		result, err := root.ReadFile("write.go")
		if err != nil {
			t.Fatal(err)
		}
		assertSingleTrailingNewline(t, result)
	})

	t.Run("WriteNonGo", func(t *testing.T) {
		h := codetypes.Hunk{
			Op:       "WRITE",
			FilePath: "readme.md",
			Body:     "# Title\n\nContent\n",
		}
		if err := applyHunk(root, h); err != nil {
			t.Fatalf("applyHunk failed: %v", err)
		}

		result, err := root.ReadFile("readme.md")
		if err != nil {
			t.Fatal(err)
		}
		assertSingleTrailingNewline(t, result)
	})
}