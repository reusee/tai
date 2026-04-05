package codes

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyHunksMethodNameOnly(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

type T struct{}

func (t T) Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
func (t T) Foo() {
	println("new")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(newContent), "new") {
		t.Errorf("content not updated: %s", string(newContent))
	}
}

func TestApplyHunksReplaceComments(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

// Old comment
func Foo() {
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
// New comment
func Foo() {
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if strings.Contains(s, "Old comment") {
		t.Errorf("Old comment still exists:\n%s", s)
	}
	if !strings.Contains(s, "New comment") {
		t.Errorf("New comment missing:\n%s", s)
	}
}

func TestApplyHunksNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "new_dir", "new_file.go")
	aiFile := filepath.Join(tmpDir, "test.AI")
	aiContent := []byte(`[[[ ADD_BEFORE BEGIN IN ` + targetFile + `
package newfile

func New() {}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "package newfile") {
		t.Errorf("content incorrect: %s", string(content))
	}
}

func TestApplyHunksAmbiguousName(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

type Foo func()

type T struct{}

func (t T) Foo() {
	println("old method")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
func (t T) Foo() {
	println("new method")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new method") {
		t.Errorf("method not updated")
	}
	if !strings.Contains(s, "type Foo func()") {
		t.Errorf("type Foo was incorrectly overwritten")
	}
}

func TestApplyHunksModifyNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Existing() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY NonExistent IN ` + targetFile + `
func NonExistent() {
	println("should not be added")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}
	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if strings.Contains(s, "func NonExistent") {
		t.Errorf("MODIFY incorrectly added a non-existent function: %s", s)
	}
	if !strings.Contains(s, "func Existing") {
		t.Errorf("existing function lost: %s", s)
	}
}

func TestApplyHunksDeleteNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Existing() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ DELETE NonExistent IN ` + targetFile + ` ]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}
	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(newContent), "func Existing") {
		t.Errorf("existing content modified: %s", string(newContent))
	}
	aiNewContent, _ := os.ReadFile(aiFile)
	if len(bytes.TrimSpace(aiNewContent)) > 0 {
		t.Errorf("hunk not removed from AI file")
	}
}

func TestApplyHunksWithPackageDeclaration(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
package test

func Foo() {
	println("new")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if strings.Count(s, "package test") != 1 {
		t.Errorf("expected 1 package declaration, got %d:\n%s", strings.Count(s, "package test"), s)
	}
	if !strings.Contains(s, "new") {
		t.Errorf("content not updated:\n%s", s)
	}
}

func TestApplyHunksPathWithSpaces(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	// File path with spaces must be quoted
	aiContent := []byte(`[[[ MODIFY Foo IN "` + targetFile + `"
func Foo() {
	println("new")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatalf("ApplyHunks failed: %v", err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(newContent), "new") {
		t.Errorf("content not updated: %s", string(newContent))
	}
}

func TestApplyHunksBodyEndCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {
	println("old value")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	// The body ends with a character before the closing bracket
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
func Foo() {
	println("new value x")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, `"new value x"`) {
		t.Errorf("last character of body was truncated: %s", s)
	}
	if strings.Contains(s, `"new value "`) && !strings.Contains(s, `"new value x"`) {
		t.Errorf("trailing 'x' is missing due to bug")
	}
}

func TestApplyHunksNoPackage(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
func Foo() {
	println("new")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new") {
		t.Errorf("content not updated: %s", s)
	}
	if strings.Contains(s, "package p") {
		t.Errorf("virtual package leaked into file: %s", s)
	}
}

func TestApplyHunksWithMarkdownFences(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
` + "```go" + `
func Foo() {
	println("new")
}
` + "```" + `
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new") {
		t.Errorf("content not updated:\n%s", s)
	}
	if strings.Contains(s, "```") {
		t.Errorf("markdown fences leaked into file:\n%s", s)
	}
}

func TestApplyHunksMalformedSingleLine(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	// Malformed: ]]] on header line, but body follows
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + ` ]]]
func Foo() {
	println("new")
}
`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new") {
		t.Errorf("content not updated (likely treated as empty body/delete):\n%s", s)
	}
}

func TestApplyHunksMultipleMalformed(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {}
func Bar() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + ` ]]]
func Foo() { println("f") }
[[[ MODIFY Bar IN ` + targetFile + ` ]]]
func Bar() { println("b") }
`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, `println("f")`) || !strings.Contains(s, `println("b")`) {
		t.Errorf("one or more updates failed:\n%s", s)
	}
}

func TestApplyHunksRedundantHeaderFooter(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	// Header has ]]], body follows, then footer ]]]
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + ` ]]]
func Foo() {
	println("new")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new") {
		t.Errorf("content not updated: %s", s)
	}
	if strings.Contains(s, "]]]") {
		t.Errorf("footer delimiter leaked into body: %s", s)
	}
}

func TestApplyHunksTrailingFooterInBody(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	// Closing ]]] is on the same line as the end of body
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
func Foo() {
	println("new")
} ]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new") {
		t.Errorf("content not updated: %s", s)
	}
	if strings.Contains(s, "]]]") {
		t.Errorf("footer delimiter leaked into body: %s", s)
	}
}

func TestApplyHunksWithJunkAfter(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + ` ]]]
` + "```go" + `
func Foo() { println("new") }
` + "```" + `
Hope this helps!
`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new") {
		t.Errorf("content not updated: %s", s)
	}
	if strings.Contains(s, "Hope this helps") {
		t.Errorf("junk leaked into file: %s", s)
	}
}

func TestApplyHunksNoSpaceBeforeFooter(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

func Foo() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	// No space before ]]]
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `]]]` + "\nfunc Foo() { println(\"new\") }\n")
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(newContent), "new") {
		t.Errorf("content not updated: %s", string(newContent))
	}
}

func TestApplyHunksConstInBlockPreservation(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

const (
	A = 1
	B = 2
)
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY A IN ` + targetFile + `
const A = 3
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "A = 3") {
		t.Errorf("A not updated: %s", s)
	}
	if !strings.Contains(s, "B = 2") {
		t.Errorf("B incorrectly removed: %s", s)
	}
}

func TestApplyHunksDeleteInBlock(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

const (
	A = 1
	B = 2
)
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ DELETE A IN ` + targetFile + ` ]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if strings.Contains(s, "A = 1") {
		t.Errorf("A not deleted: %s", s)
	}
	if !strings.Contains(s, "B = 2") {
		t.Errorf("B incorrectly removed: %s", s)
	}
}

func TestApplyHunksTypeAndMethodsCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

type T struct{}

func (t T) Foo() {
	println("old foo")
}

func (t T) Bar() {
	println("old bar")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY T IN ` + targetFile + `
type T struct { I int }
func (t T) Foo() { println("new foo") }
func (t T) Bar() { println("new bar") }
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if strings.Count(s, "func (t T) Foo()") != 1 {
		t.Errorf("expected 1 Foo, got %d:\n%s", strings.Count(s, "func (t T) Foo()"), s)
	}
	if strings.Count(s, "func (t T) Bar()") != 1 {
		t.Errorf("expected 1 Bar, got %d:\n%s", strings.Count(s, "func (t T) Bar()"), s)
	}
	if !strings.Contains(s, "new foo") || !strings.Contains(s, "new bar") {
		t.Errorf("new content missing:\n%s", s)
	}
}

func TestApplyHunksConstWithoutKeyword(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, "test.go")
	content := []byte(`package test

const A = 1
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := filepath.Join(tmpDir, "test.go.AI")
	aiContent := []byte(`[[[ MODIFY A IN ` + targetFile + `
A = 2
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "const A = 2") {
		t.Errorf("const keyword lost: %s", s)
	}
}

func TestApplyHunksAddMethodAfterType(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte(`package test

type foo struct {
	I int
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	aiContent := []byte(`[[[ ADD_AFTER foo IN ` + targetFile + `
func (f *foo) M() {
	println(f.I)
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "func (f *foo) M()") {
		t.Errorf("method not added:\n%s", s)
	}
}

func TestApplyHunksWithCommentedPackage(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte(`package test

type foo struct{}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	aiContent := []byte(`[[[ ADD_AFTER foo IN ` + targetFile + `
// This is a comment before the package
package test

func (f *foo) M() {}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatalf("ApplyHunks failed: %v", err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if strings.Count(s, "package test") != 1 {
		t.Errorf("multiple package declarations found:\n%s", s)
	}
	if !strings.Contains(s, "func (f *foo) M()") {
		t.Errorf("method missing:\n%s", s)
	}
}

func TestApplyHunksModifyBegin(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte(`package foo

import "os"

func A() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	aiContent := []byte(`[[[ MODIFY BEGIN IN test.go
package foo

import (
	"fmt"
	"os"
)
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if strings.Count(s, "package foo") != 1 {
		t.Errorf("duplicated package declaration:\n%s", s)
	}
	if !strings.Contains(s, `"fmt"`) {
		t.Errorf("fmt import missing:\n%s", s)
	}
	if !strings.Contains(s, "func A()") {
		t.Errorf("function A lost:\n%s", s)
	}
}

func TestApplyHunksRawValueReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte("package test\n\nconst Theory = `old theory` \n")
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	aiContent := []byte(`[[[ MODIFY Theory IN test.go
new theory content
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "new theory content") {
		t.Errorf("content not updated:\n%s", s)
	}
}

func TestApplyHunksRawValueReplacementInt(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte("package test\n\nconst A = 1\n")
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	aiContent := []byte(`[[[ MODIFY A IN test.go
42
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "A = 42") {
		t.Errorf("int constant not updated:\n%s", s)
	}
}

func TestApplyHunksAddAfterBeginImport(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte(`package test

func Foo() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	aiContent := []byte(`[[[ ADD_AFTER BEGIN IN test.go
import "fmt"
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	// Check that import comes after package
	pkgIdx := strings.Index(s, "package test")
	impIdx := strings.Index(s, `import "fmt"`)
	if pkgIdx == -1 || impIdx == -1 {
		t.Fatalf("missing package or import:\n%s", s)
	}
	if impIdx < pkgIdx {
		t.Errorf("import prepended before package declaration:\n%s", s)
	}
}

func TestApplyHunksImportMerging(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte(`package test

import "os"

func Foo() {}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	// Model sends ADD_AFTER BEGIN with new import block
	aiContent := []byte(`[[[ ADD_AFTER BEGIN IN test.go
import (
	"fmt"
	"os"
)
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)

	// Check that we don't have two import declarations
	if strings.Count(s, "import") != 1 {
		t.Errorf("expected 1 import declaration, got %d:\n%s", strings.Count(s, "import"), s)
	}
	if !strings.Contains(s, `"fmt"`) || !strings.Contains(s, `"os"`) {
		t.Errorf("imports missing:\n%s", s)
	}
}

func TestApplyHunksTargetNotFirstInBody(t *testing.T) {
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targetFile := "test.go"
	content := []byte(`package test

var Bar = 1

func Foo() {
	println("old")
}
`)
	if err := os.WriteFile(targetFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	aiFile := "test.go.AI"
	// Target is Foo, but Bar comes first in hunk body
	aiContent := []byte(`[[[ MODIFY Foo IN test.go
var Bar = 2
func Foo() {
	println("new")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(root, aiFile); err != nil {
		t.Fatal(err)
	}

	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "Bar = 2") {
		t.Errorf("Bar not updated:\n%s", s)
	}
	if !strings.Contains(s, "println(\"new\")") {
		t.Errorf("Foo not updated:\n%s", s)
	}
	if strings.Count(s, "func Foo") != 1 {
		t.Errorf("Foo duplicated:\n%s", s)
	}
	if strings.Count(s, "var Bar") != 1 {
		t.Errorf("Bar duplicated:\n%s", s)
	}
}
