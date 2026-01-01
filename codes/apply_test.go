package codes

import (
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
