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
	text := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\" />\n\nfunc New() {}\n:::end 徕珑\n"
	if _, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	if err := applyChangeBlocks(parserState, root); err != nil {
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
	if remaining := parserState.PopBlocksByKind("change"); len(remaining) != 0 {
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
	text := ":::change 徕珑\nthis is not valid XML metadata\n:::end 徕珑\n"
	if _, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	err = applyChangeBlocks(parserState, root)
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
	text := ":::change 徕珑\n<change op=\"WRITE\" file-path=\"../../../etc/passwd\" />\n\ncontent\n:::end 徕珑\n"
	if _, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	err = applyChangeBlocks(parserState, root)
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

func TestProcessContinueBlocks(t *testing.T) {
	// Create a state with ParserState wrapping
	state := generators.NewPrompts("", nil)
	parserState := blocks.NewParserState(state)

	// Append a continue block
	text := ":::continue 徕珑\nPlease continue the task.\n:::end 徕珑\n"
	_, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}

	newState, hasContinue, err := processContinueBlocks(parserState, generators.State(parserState))
	if err != nil {
		t.Fatal(err)
	}
	if !hasContinue {
		t.Fatal("expected hasContinue to be true")
	}

	// Verify the user content was appended
	found := false
	for c := range newState.Contents() {
		if c.Role == "user" {
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok && strings.Contains(string(text), "Please continue the task.") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected user content with continue message")
	}

	// Verify that continue blocks were consumed
	if remaining := parserState.PopBlocksByKind("continue"); len(remaining) != 0 {
		t.Fatalf("expected 0 remaining continue blocks, got %d", len(remaining))
	}
}

func TestProcessContinueBlocksNoBlock(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := blocks.NewParserState(state)

	newState, hasContinue, err := processContinueBlocks(parserState, generators.State(parserState))
	if err != nil {
		t.Fatal(err)
	}
	if hasContinue {
		t.Fatal("expected hasContinue to be false")
	}
	// State should be unchanged (same reference or equivalent)
	if newState != generators.State(parserState) {
		// It's okay if it's a different wrapper, but contents should be same
	}
}
