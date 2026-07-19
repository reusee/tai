package blocks

import (
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessContinueBlocks(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	// Append a continue block
	text := ":::徕珑 <continue>\nPlease continue the task.\n:::徕珑 </continue>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*ParserState)

	parts, newParserState := ProcessContinueBlocks(parserState)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	content, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(content), "Please continue the task.") {
		t.Fatalf("expected content to contain 'Please continue the task.', got %q", content)
	}

	// Verify that continue blocks were consumed
	if remaining, _ := newParserState.PopBlocksByKind("continue"); len(remaining) != 0 {
		t.Fatalf("expected 0 remaining continue blocks, got %d", len(remaining))
	}
}

func TestProcessContinueBlocksNoBlock(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	parts, _ := ProcessContinueBlocks(parserState)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}
