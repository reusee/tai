package changes

import (
	"os"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
)

func TestApplyChangeBlocks(t *testing.T) {
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

	changeBlocks := []blocks.Block{
		{
			Kind:       "change",
			Boundary:   "徕珑",
			Attributes: map[string]string{"op": "MODIFY", "target": "Old", "file-path": "test.go"},
			Body:       "func New() {}",
		},
	}

	if err := ApplyChangeBlocks(changeBlocks, root); err != nil {
		t.Fatalf("ApplyChangeBlocks failed: %v", err)
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

func TestApplyChangeBlocksUnparseable(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// A change block missing the required "op" attribute is unparseable.
	changeBlocks := []blocks.Block{
		{
			Kind:       "change",
			Boundary:   "徕珑",
			Attributes: map[string]string{"target": "Foo", "file-path": "test.go"},
			Body:       "func Foo() {}",
		},
	}

	err = ApplyChangeBlocks(changeBlocks, root)
	if err == nil {
		t.Fatal("expected error for unparseable change block")
	}
	if !strings.Contains(err.Error(), "unparseable") {
		t.Fatalf("expected unparseable error, got: %v", err)
	}
}

func TestApplyChangeBlocksApplyError(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	changeBlocks := []blocks.Block{
		{
			Kind:       "change",
			Boundary:   "徕珑",
			Attributes: map[string]string{"op": "WRITE", "file-path": "../../../etc/passwd"},
			Body:       "content",
		},
	}

	err = ApplyChangeBlocks(changeBlocks, root)
	if err == nil {
		t.Fatal("expected error for path escape")
	}
	if !strings.Contains(err.Error(), "apply change block") {
		t.Fatalf("expected apply change block error, got: %v", err)
	}
}
