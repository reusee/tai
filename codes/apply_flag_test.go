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
	parserState := blocks.NewParserState(state)
	text := ":::徕珑 <change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\">\nfunc New() {}\n:::徕珑 </change>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*blocks.ParserState)

	newParserState, err := applyChangeBlocks(parserState, root)
	if err != nil {
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
	if remaining, _ := newParserState.PopBlocksByKind("change"); len(remaining) != 0 {
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
	parserState := blocks.NewParserState(state)
	// A change block missing the required "op" attribute is unparseable.
	text := ":::徕珑 <change target=\"Foo\" file-path=\"test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*blocks.ParserState)

	_, err = applyChangeBlocks(parserState, root)
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
	parserState := blocks.NewParserState(state)
	text := ":::徕珑 <change op=\"WRITE\" file-path=\"../../../etc/passwd\">\ncontent\n:::徕珑 </change>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*blocks.ParserState)

	_, err = applyChangeBlocks(parserState, root)
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
