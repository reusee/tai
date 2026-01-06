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

	if err := ApplyHunks(aiFile); err != nil {
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

	if err := ApplyHunks(aiFile); err != nil {
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
	targetFile := filepath.Join(tmpDir, "new_dir", "new_file.go")
	aiFile := filepath.Join(tmpDir, "test.AI")
	aiContent := []byte(`[[[ ADD_BEFORE BEGIN IN ` + targetFile + `
package newfile

func New() {}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyHunks(aiFile); err != nil {
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

	if err := ApplyHunks(aiFile); err != nil {
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
	println("appended")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyHunks(aiFile); err != nil {
		t.Fatal(err)
	}
	newContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(newContent)
	if !strings.Contains(s, "func NonExistent") {
		t.Errorf("missing appended function: %s", s)
	}
	if !strings.Contains(s, "func Existing") {
		t.Errorf("existing function lost: %s", s)
	}
}

func TestApplyHunksDeleteNotFound(t *testing.T) {
	tmpDir := t.TempDir()
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
	if err := ApplyHunks(aiFile); err != nil {
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

	if err := ApplyHunks(aiFile); err != nil {
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

	if err := ApplyHunks(aiFile); err != nil {
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
	// Bug would truncate the last character 'x'
	aiContent := []byte(`[[[ MODIFY Foo IN ` + targetFile + `
func Foo() {
	println("new value x")
}
]]]`)
	if err := os.WriteFile(aiFile, aiContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyHunks(aiFile); err != nil {
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

	if err := ApplyHunks(aiFile); err != nil {
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
