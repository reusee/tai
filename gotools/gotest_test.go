package gotools

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

func TestGoBlockPromptsFormatGuards(t *testing.T) {
	// The Go-specific kind prompts migrated from the blocks package keep
	// the two format guards the blocks package enforces for its own kind
	// prompts: no literal '<<DELIMITER' template marker (the model imitates
	// templates verbatim), and no restatement of the delimiter policy (the
	// unified blocks.BlockFormatSystemPrompt covers it). See
	// blocks.TheoryOfBlockFormatGeneral.
	prompts := map[string]string{
		"GoTestBlockSystemPrompt":  GoTestBlockSystemPrompt,
		"GoTestBlockRestatePrompt": GoTestBlockRestatePrompt,
		"GoSrcBlockSystemPrompt":   GoSrcBlockSystemPrompt,
		"GoSrcBlockRestatePrompt":  GoSrcBlockRestatePrompt,
	}
	for name, prompt := range prompts {
		if strings.Contains(prompt, "<<DELIMITER") {
			t.Fatalf("%s displays the literal template marker '<<DELIMITER', which the model imitates verbatim", name)
		}
		if strings.Contains(prompt, "uncommon Chinese two-character word") {
			t.Fatalf("%s must not restate the delimiter policy; the unified BlockFormatSystemPrompt covers it", name)
		}
	}
}

func TestProcessGoTestBlocks(t *testing.T) {
	t.Run("TestsFail", func(t *testing.T) {
		// -run and Test[ on separate lines. Test[ is an invalid
		// regex, causing go test to fail with a regexp parsing error.
		bs := []blocks.Block{
			{Kind: "go-test", Body: "-run\nTest["},
		}
		parts, err := ProcessGoTestBlocks(bs, context.Background())
		if err != nil {
			t.Fatalf("ProcessGoTestBlocks failed: %v", err)
		}
		if len(parts) != 1 {
			t.Fatalf("expected 1 part for failing tests, got %d", len(parts))
		}
		output := string(parts[0].(generators.Text))
		if !strings.Contains(output, "Working directory:") {
			t.Fatalf("expected output to contain 'Working directory:', got: %s", output)
		}
		if !strings.Contains(output, "Go test command:") {
			t.Fatalf("expected output to contain 'Go test command:', got: %s", output)
		}
		if !strings.Contains(output, "Command failed") {
			t.Fatalf("expected output to contain 'Command failed', got: %s", output)
		}
	})

	t.Run("TestsPass", func(t *testing.T) {
		// -run and ___nonexistent___ on separate lines.
		// ___nonexistent___ matches no tests, so go test succeeds.
		// Test output is always returned to the model, even when tests pass.
		bs := []blocks.Block{
			{Kind: "go-test", Body: "-run\n___nonexistent___"},
		}
		parts, err := ProcessGoTestBlocks(bs, context.Background())
		if err != nil {
			t.Fatalf("ProcessGoTestBlocks failed: %v", err)
		}
		if len(parts) != 1 {
			t.Fatalf("expected 1 part for passing tests, got %d", len(parts))
		}
		output := string(parts[0].(generators.Text))
		if !strings.Contains(output, "Working directory:") {
			t.Fatalf("expected output to contain 'Working directory:', got: %s", output)
		}
		if !strings.Contains(output, "Command succeeded") {
			t.Fatalf("expected output to contain 'Command succeeded', got: %s", output)
		}
	})
}

func TestProcessGoTestBlocksEmpty(t *testing.T) {
	parts, err := ProcessGoTestBlocks(nil, context.Background())
	if err != nil {
		t.Fatalf("ProcessGoTestBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}
