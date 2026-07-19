package blocks

import (
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessSummaryBlocks(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	// Append a summary block
	text := ":::徕珑 <summary>\nAnalyzed the code and fixed the Foo function.\n:::徕珑 </summary>\n"
	_, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}

	summaries := ProcessSummaryBlocks(parserState)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0], "Analyzed the code and fixed the Foo function.") {
		t.Fatalf("expected summary to contain the description, got %q", summaries[0])
	}

	// Verify that summary blocks were consumed
	if remaining := parserState.PopBlocksByKind("summary"); len(remaining) != 0 {
		t.Fatalf("expected 0 remaining summary blocks, got %d", len(remaining))
	}
}

func TestProcessSummaryBlocksMultiple(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	text := ":::徕珑 <summary>\nRound 1 summary.\n:::徕珑 </summary>\n:::栢彣 <summary>\nRound 2 summary.\n:::栢彣 </summary>\n"
	_, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}

	summaries := ProcessSummaryBlocks(parserState)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0], "Round 1 summary.") {
		t.Fatalf("expected first summary to contain 'Round 1 summary.', got %q", summaries[0])
	}
	if !strings.Contains(summaries[1], "Round 2 summary.") {
		t.Fatalf("expected second summary to contain 'Round 2 summary.', got %q", summaries[1])
	}
}

func TestProcessSummaryBlocksNoBlock(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	summaries := ProcessSummaryBlocks(parserState)
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(summaries))
	}
}

func TestProcessSummaryBlocksNilState(t *testing.T) {
	summaries := ProcessSummaryBlocks(nil)
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for nil state, got %d", len(summaries))
	}
}

func TestProcessSummaryBlocksPreservesChangeBlocks(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	text := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n:::栢彣 <summary>\nFixed the Foo function.\n:::栢彣 </summary>\n"
	_, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Process summary blocks — must not discard change blocks.
	summaries := ProcessSummaryBlocks(parserState)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0], "Fixed the Foo function.") {
		t.Fatalf("expected summary to contain description, got %q", summaries[0])
	}

	// Change blocks must still be available after processing summaries.
	blocks := parserState.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 change block to be preserved, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" {
		t.Fatalf("expected change block, got %s", blocks[0].Kind)
	}
}
