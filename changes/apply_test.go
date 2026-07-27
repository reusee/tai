package changes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyChangeBlockAddBeforeConstSpec(t *testing.T) {
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

	h := ChangeBlock{
		Op:       "ADD_BEFORE",
		Target:   "ccc",
		FilePath: "test.go",
		Body:     "const aaa = 42",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
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

func TestApplyChangeBlockRename(t *testing.T) {
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

	h := ChangeBlock{
		Op:       "RENAME",
		Target:   "newname.go",
		FilePath: "test.go",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
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

func TestApplyChangeBlockModifyPackage(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package oldpkg\n\nimport \"fmt\"\n\nfunc Foo() { fmt.Println(\"hello\") }\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "package",
		FilePath: "test.go",
		Body:     "package newpkg",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if strings.Contains(resultStr, "oldpkg") {
		t.Fatalf("result should not contain oldpkg:\n%s", resultStr)
	}
	if !strings.Contains(resultStr, "package newpkg") {
		t.Fatalf("result should contain 'package newpkg':\n%s", resultStr)
	}
}

func TestApplyChangeBlockModifyImportReplace(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nimport (\n\t\"fmt\"\n)\n\nfunc Foo() { fmt.Println(\"hello\"); os.Exit(0) }\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "import",
		FilePath: "test.go",
		Body:     "import (\n\t\"fmt\"\n\t\"os\"\n)",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if !strings.Contains(resultStr, "\"os\"") {
		t.Fatalf("result should contain 'os' import:\n%s", resultStr)
	}
	if !strings.Contains(resultStr, "\"fmt\"") {
		t.Fatalf("result should still contain 'fmt' import:\n%s", resultStr)
	}
}

func TestApplyChangeBlockModifyImportAddToFileWithoutImports(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nfunc Foo() { fmt.Println(\"hello\") }\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "import",
		FilePath: "test.go",
		Body:     "import \"fmt\"",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if !strings.Contains(resultStr, "\"fmt\"") {
		t.Fatalf("result should contain 'fmt' import:\n%s", resultStr)
	}
}

func TestApplyChangeBlockModifyImportRemoveAll(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// Use fmt in the function so goimports would normally add it back.
	// The test verifies that goimports re-adds the needed import after removal.
	original := "package x\n\nimport \"fmt\"\n\nfunc Foo() { fmt.Println(\"hello\") }\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "import",
		FilePath: "test.go",
		Body:     "",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	// goimports should re-add fmt since Foo uses it.
	if !strings.Contains(resultStr, "\"fmt\"") {
		t.Fatalf("result should contain 'fmt' import (re-added by goimports since Foo uses it):\n%s", resultStr)
	}
}

func TestApplyChangeBlockModifyPackageBodyWithoutPackageKeyword(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package oldpkg\n\nfunc Foo() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// Body without "package " prefix — the implementation extracts the name.
	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "package",
		FilePath: "test.go",
		Body:     "newpkg",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
	}

	result, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if strings.Contains(resultStr, "oldpkg") {
		t.Fatalf("result should not contain oldpkg:\n%s", resultStr)
	}
	if !strings.Contains(resultStr, "package newpkg") {
		t.Fatalf("result should contain 'package newpkg':\n%s", resultStr)
	}
}

func TestApplyChangeBlockModifyPackageNonModifyRejected(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nfunc Foo() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := ChangeBlock{
		Op:       "ADD_BEFORE",
		Target:   "package",
		FilePath: "test.go",
		Body:     "some text",
	}
	err = ApplyChangeBlock(root, h)
	if err == nil {
		t.Fatal("expected error for non-MODIFY op on package target")
	}
	if !strings.Contains(err.Error(), "only supports MODIFY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyChangeBlockModifyImportNonModifyRejected(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nimport \"fmt\"\n\nfunc Foo() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := ChangeBlock{
		Op:       "DELETE",
		Target:   "import",
		FilePath: "test.go",
		Body:     "",
	}
	err = ApplyChangeBlock(root, h)
	if err == nil {
		t.Fatal("expected error for non-MODIFY op on import target")
	}
	if !strings.Contains(err.Error(), "only supports MODIFY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyChangeBlockDeleteFile(t *testing.T) {
	t.Run("GoFile", func(t *testing.T) {
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

		h := ChangeBlock{
			Op:       "DELETE",
			Target:   "*",
			FilePath: "test.go",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		_, err = root.Stat("test.go")
		if err == nil {
			t.Fatal("file should not exist after deletion")
		}
		if !os.IsNotExist(err) {
			t.Fatalf("expected IsNotExist, got %v", err)
		}
	})

	t.Run("NonGoFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		if err := root.WriteFile("readme.md", []byte("# Title\n"), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "DELETE",
			Target:   "*",
			FilePath: "readme.md",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		_, err = root.Stat("readme.md")
		if err == nil {
			t.Fatal("file should not exist after deletion")
		}
		if !os.IsNotExist(err) {
			t.Fatalf("expected IsNotExist, got %v", err)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		h := ChangeBlock{
			Op:       "DELETE",
			Target:   "*",
			FilePath: "nonexistent.go",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock should be no-op for non-existent file, got: %v", err)
		}
	})
}

func TestApplyChangeBlockNoBlankLinesInBody(t *testing.T) {
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

	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "Old",
		FilePath: "test.go",
		Body:     "func New() {}",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
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

func TestApplyChangeBlockWrite(t *testing.T) {
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

		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "test.go",
			Body:     "package x\n\nfunc New() {}\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
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

		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "new.go",
			Body:     "package x\n\nfunc New() {}\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
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

		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "readme.md",
			Body:     "# New Title\n\nNew content\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
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

		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "sub/dir/notes.md",
			Body:     "# Notes\n\nSome content\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		_, err = root.Stat("sub/dir/notes.md")
		if err != nil {
			t.Fatalf("file should exist: %v", err)
		}
	})
}

func TestApplyChangeBlockPathWithDoubleDotPrefix(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// A file whose name starts with ".." but is not a parent-directory
	// traversal (e.g., "..notescape.go") must be accepted by ApplyChangeBlock.
	// Before the fix, strings.HasPrefix(filepath.Clean(path), "..")
	// incorrectly rejected any path starting with two dots.
	filename := "..notescape.go"
	original := "package x\n\nfunc Old() {}\n"
	if err := root.WriteFile(filename, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "Old",
		FilePath: filename,
		Body:     "func New() {}",
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed for path starting with double dots: %v", err)
	}

	result, err := root.ReadFile(filename)
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

func TestApplyUnclosedBlockError(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	content := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/f.go\">\nfunc Foo() {}\n"
	diffPath := filepath.Join(dir, "diff.txt")
	if err := os.WriteFile(diffPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sawError := false
	for _, err := range ApplyDiffFile(root, diffPath) {
		if err == nil {
			t.Fatal("expected error, got a change block")
		}
		sawError = true
		if !strings.Contains(err.Error(), "unclosed") {
			t.Fatalf("expected unclosed block error, got: %v", err)
		}
	}
	if !sawError {
		t.Fatal("expected an error from ApplyChangeBlock")
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

		content := ":::徕珑 <change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\">\nfunc New() {}\n:::徕珑 </change>\n\n:::栢彣 <finish>\nRenamed Old to New.\n:::栢彣 </finish>\n"
		diffPath := filepath.Join(dir, "diff.txt")
		if err := os.WriteFile(diffPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		count := 0
		for _, err := range ApplyDiffFile(root, diffPath) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("expected 1 change block, got %d", count)
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
		content := ":::栢彣 <finish>\nRenamed Old to New.\n:::栢彣 </finish>\n\n:::徕珑 <change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\">\nfunc New() {}\n:::徕珑 </change>\n"
		diffPath := filepath.Join(dir, "diff.txt")
		if err := os.WriteFile(diffPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		count := 0
		for _, err := range ApplyDiffFile(root, diffPath) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("expected 1 change block, got %d", count)
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

		count := 0
		for _, err := range ApplyDiffFile(root, diffPath) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("expected 1 change block, got %d", count)
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

	changeBlock := ":::徕珑 <change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\">\nfunc New() {}\n:::徕珑 </change>\n"
	finishBlock := ":::栢彣 <finish>\nRenamed Old to New.\n:::栢彣 </finish>\n"

	t.Run("ChangeThenFinish", func(t *testing.T) {
		run(t, changeBlock+"\n"+finishBlock)
	})
	t.Run("FinishThenChange", func(t *testing.T) {
		run(t, finishBlock+"\n"+changeBlock)
	})
}

func TestApplyChangeBlockMultiEntityRemovesDuplicates(t *testing.T) {
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
	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "Foo",
		FilePath: "test.go",
		Body:     body,
	}
	if err := ApplyChangeBlock(root, h); err != nil {
		t.Fatalf("ApplyChangeBlock failed: %v", err)
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

func TestApplyChangeBlockTrailingNewlineConsistentWithGoFmt(t *testing.T) {
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

		h := ChangeBlock{
			Op:       "MODIFY",
			Target:   "Old",
			FilePath: "modify.go",
			Body:     "func New() {}",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("modify.go")
		if err != nil {
			t.Fatal(err)
		}
		assertSingleTrailingNewline(t, result)
	})

	t.Run("WriteGo", func(t *testing.T) {
		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "write.go",
			Body:     "package x\n\nfunc New() {}\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("write.go")
		if err != nil {
			t.Fatal(err)
		}
		assertSingleTrailingNewline(t, result)
	})

	t.Run("WriteNonGo", func(t *testing.T) {
		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "readme.md",
			Body:     "# Title\n\nContent\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("readme.md")
		if err != nil {
			t.Fatal(err)
		}
		assertSingleTrailingNewline(t, result)
	})
}

func TestApplyChangeBlockTextLevelOps(t *testing.T) {
	t.Run("ReplaceNonGoFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "# Title\n\nold description\n\nMore content\n"
		if err := root.WriteFile("readme.md", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "REPLACE",
			Find:     "old description",
			FilePath: "readme.md",
			Body:     "new description",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("readme.md")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		if strings.Contains(resultStr, "old description") {
			t.Fatalf("result should not contain 'old description':\n%s", resultStr)
		}
		if !strings.Contains(resultStr, "new description") {
			t.Fatalf("result should contain 'new description':\n%s", resultStr)
		}
		if !strings.Contains(resultStr, "More content") {
			t.Fatalf("result should preserve 'More content':\n%s", resultStr)
		}
	})

	t.Run("ReplaceNotFound", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "# Title\n\nContent\n"
		if err := root.WriteFile("readme.md", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "REPLACE",
			Find:     "nonexistent string",
			FilePath: "readme.md",
			Body:     "replacement",
		}
		err = ApplyChangeBlock(root, h)
		if err == nil {
			t.Fatal("expected error for find string not found")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected 'not found' error, got: %v", err)
		}
	})

	t.Run("ReplaceNotUnique", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "duplicate\nduplicate\n"
		if err := root.WriteFile("readme.md", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "REPLACE",
			Find:     "duplicate",
			FilePath: "readme.md",
			Body:     "unique",
		}
		err = ApplyChangeBlock(root, h)
		if err == nil {
			t.Fatal("expected error for non-unique find string")
		}
		if !strings.Contains(err.Error(), "must be unique") {
			t.Fatalf("expected 'must be unique' error, got: %v", err)
		}
	})

	t.Run("ReplaceEmptyBodyDeletesText", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "# Title\n\nDELETE ME\n\nMore content\n"
		if err := root.WriteFile("readme.md", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "REPLACE",
			Find:     "DELETE ME\n\n",
			FilePath: "readme.md",
			Body:     "",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("readme.md")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		if strings.Contains(resultStr, "DELETE ME") {
			t.Fatalf("result should not contain 'DELETE ME':\n%s", resultStr)
		}
		if !strings.Contains(resultStr, "More content") {
			t.Fatalf("result should preserve 'More content':\n%s", resultStr)
		}
	})

	t.Run("InsertBeforeNonGoFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "# Title\n\n## Section\n"
		if err := root.WriteFile("readme.md", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "INSERT_BEFORE",
			Find:     "## Section",
			FilePath: "readme.md",
			Body:     "## New Section\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("readme.md")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		newIdx := strings.Index(resultStr, "## New Section")
		sectionIdx := strings.Index(resultStr, "## Section")
		if newIdx == -1 || sectionIdx == -1 {
			t.Fatalf("both sections should be present:\n%s", resultStr)
		}
		if newIdx > sectionIdx {
			t.Fatalf("New Section should appear before Section:\n%s", resultStr)
		}
	})

	t.Run("InsertAfterNonGoFile", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "[dependencies]\n"
		if err := root.WriteFile("Cargo.toml", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "INSERT_AFTER",
			Find:     "[dependencies]",
			FilePath: "Cargo.toml",
			Body:     "serde = { version = \"1.0\" }\n",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("Cargo.toml")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		depIdx := strings.Index(resultStr, "[dependencies]")
		serdeIdx := strings.Index(resultStr, "serde")
		if depIdx == -1 || serdeIdx == -1 {
			t.Fatalf("both should be present:\n%s", resultStr)
		}
		if depIdx > serdeIdx {
			t.Fatalf("[dependencies] should appear before serde:\n%s", resultStr)
		}
	})

	t.Run("ReplaceGoFileWithGoimports", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "package x\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		// Replace fmt.Println with os.Exit, which requires changing the import.
		// goimports should remove the unused fmt import and add the os import.
		h := ChangeBlock{
			Op:       "REPLACE",
			Find:     "fmt.Println(\"hello\")",
			FilePath: "test.go",
			Body:     "os.Exit(0)",
		}
		if err := ApplyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		result, err := root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		if strings.Contains(resultStr, "fmt.Println") {
			t.Fatalf("result should not contain fmt.Println:\n%s", resultStr)
		}
		if !strings.Contains(resultStr, "os.Exit(0)") {
			t.Fatalf("result should contain os.Exit(0):\n%s", resultStr)
		}
		// goimports should have added the "os" import
		if !strings.Contains(resultStr, "\"os\"") {
			t.Fatalf("result should contain os import (added by goimports):\n%s", resultStr)
		}
	})
}
