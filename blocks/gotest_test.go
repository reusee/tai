package blocks

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessGoTestBlocks(t *testing.T) {
	t.Run("TestsFail", func(t *testing.T) {
		state := generators.NewPrompts("", nil)
		parserState := NewParserState(state)

		// -run 'Test[' is an invalid regex, causing go test to fail
		// with a regexp parsing error. This verifies that stdout and
		// stderr are fed back to the model when tests fail.
		text := ":::徕珑 <go-test>\n-run 'Test['\n:::徕珑 </go-test>\n"
		newState, err := parserState.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		parserState = newState.(*ParserState)

		parts, newParserState, failed, err := ProcessGoTestBlocks(parserState, context.Background())
		if err != nil {
			t.Fatalf("ProcessGoTestBlocks failed: %v", err)
		}
		if !failed {
			t.Fatal("expected failed=true for failing tests")
		}
		if len(parts) != 1 {
			t.Fatalf("expected 1 part for failing tests, got %d", len(parts))
		}
		output := string(parts[0].(generators.Text))
		if !strings.Contains(output, "Go test command:") {
			t.Fatalf("expected output to contain 'Go test command:', got: %s", output)
		}
		if !strings.Contains(output, "Command failed") {
			t.Fatalf("expected output to contain 'Command failed', got: %s", output)
		}

		// go-test blocks should have been consumed
		if remaining, _ := newParserState.PopBlocksByKind("go-test"); len(remaining) != 0 {
			t.Fatalf("expected 0 remaining go-test blocks, got %d", len(remaining))
		}
	})

	t.Run("TestsPass", func(t *testing.T) {
		state := generators.NewPrompts("", nil)
		parserState := NewParserState(state)

		// -run ___nonexistent___ matches no tests, so go test succeeds.
		// When tests pass, no output parts should be returned.
		text := ":::徕珑 <go-test>\n-run ___nonexistent___\n:::徕珑 </go-test>\n"
		newState, err := parserState.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		parserState = newState.(*ParserState)

		parts, _, failed, err := ProcessGoTestBlocks(parserState, context.Background())
		if err != nil {
			t.Fatalf("ProcessGoTestBlocks failed: %v", err)
		}
		if failed {
			t.Fatal("expected failed=false for passing tests")
		}
		if len(parts) != 0 {
			t.Fatalf("expected 0 parts for passing tests, got %d", len(parts))
		}
	})
}

func TestProcessGoTestBlocksEmpty(t *testing.T) {
	state := generators.NewPrompts("", nil)
	parserState := NewParserState(state)

	parts, _, failed, err := ProcessGoTestBlocks(parserState, context.Background())
	if err != nil {
		t.Fatalf("ProcessGoTestBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
	if failed {
		t.Fatal("expected failed to be false for no go-test blocks")
	}
}

func TestProcessGoTestBlocksNilState(t *testing.T) {
	parts, _, failed, err := ProcessGoTestBlocks(nil, context.Background())
	if err != nil {
		t.Fatalf("ProcessGoTestBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts for nil state, got %d", len(parts))
	}
	if failed {
		t.Fatal("expected failed to be false for nil state")
	}
}

func TestProcessGoTestBlocksPreservesChangeBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	// Use -run 'Test[' (invalid regex) to make go test fail, so output
	// parts are returned and we can verify change blocks are preserved.
	text := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n:::栢彣 <go-test>\n-run 'Test['\n:::栢彣 </go-test>\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	parts, newPs, _, err := ProcessGoTestBlocks(ps, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) == 0 {
		t.Fatal("expected go-test output parts")
	}

	// Change blocks must still be available after processing go-test blocks.
	blocks, _ := newPs.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 change block to be preserved, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" {
		t.Fatalf("expected change block, got %s", blocks[0].Kind)
	}
}
