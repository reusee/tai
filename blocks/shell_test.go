package blocks

import (
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessShellBlocks(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	// Append a shell block with echo command
	text := ":::徕珑 <shell>\necho hello world\n:::徕珑 </shell>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*ParserState)

	parts, newParserState, err := ProcessShellBlocks(parserState)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "hello world") {
		t.Fatalf("expected output to contain 'hello world', got: %s", output)
	}
	if !strings.Contains(output, "Command succeeded") {
		t.Fatalf("expected output to contain 'Command succeeded', got: %s", output)
	}

	// Shell blocks should have been consumed
	if remaining, _ := newParserState.PopBlocksByKind("shell"); len(remaining) != 0 {
		t.Fatalf("expected 0 remaining shell blocks, got %d", len(remaining))
	}
}

func TestProcessShellBlocksCommandFailure(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	text := ":::徕珑 <shell>\nexit 1\n:::徕珑 </shell>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*ParserState)

	parts, _, err := ProcessShellBlocks(parserState)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "Command failed") {
		t.Fatalf("expected output to contain 'Command failed', got: %s", output)
	}
}

func TestProcessShellBlocksEmpty(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	parts, _, err := ProcessShellBlocks(parserState)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}
