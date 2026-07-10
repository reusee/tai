package codes

import (
	"os"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
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

	state := generators.NewPrompts("", nil)
	blockState := blocks.NewBlockState(state)
	text := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\" />\n\nfunc New() {}\n:::end 徕珑\n"
	if _, err := blockState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	if err := applyChangeBlocks(blockState, root); err != nil {
		t.Fatalf("applyChangeBlocks failed: %v", err)
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

	// Change blocks should have been consumed by applyChangeBlocks.
	if remaining := blockState.PopBlocksByKind("change"); len(remaining) != 0 {
		t.Fatalf("expected 0 remaining change blocks, got %d", len(remaining))
	}
}

func TestApplyChangeBlocksUnparseable(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	state := generators.NewPrompts("", nil)
	blockState := blocks.NewBlockState(state)
	text := ":::change 徕珑\nthis is not valid XML metadata\n:::end 徕珑\n"
	if _, err := blockState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	err = applyChangeBlocks(blockState, root)
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

	state := generators.NewPrompts("", nil)
	blockState := blocks.NewBlockState(state)
	text := ":::change 徕珑\n<change op=\"WRITE\" file-path=\"../../../etc/passwd\" />\n\ncontent\n:::end 徕珑\n"
	if _, err := blockState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	err = applyChangeBlocks(blockState, root)
	if err == nil {
		t.Fatal("expected error for path escape")
	}
	if !strings.Contains(err.Error(), "apply hunk") {
		t.Fatalf("expected apply hunk error, got: %v", err)
	}
}

func TestApplyDefaultEnabled(t *testing.T) {
	// By default, immediate apply is enabled. The -no-apply flag
	// disables it by setting applyFlag to false.
	if !bool(applyFlag) {
		t.Fatal("applyFlag should default to true (immediate apply enabled by default)")
	}
}
