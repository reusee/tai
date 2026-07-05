package codes

import (
	"strings"
	"testing"
)

// TestBoundaryUniquenessInPrompts is a regression test for the issue where the
// model copied example boundary strings (e.g., 徕珑) verbatim from the system
// prompt, causing the parser to close blocks at the wrong :::end marker. The
// prompts must instruct the model to generate a fresh random boundary per block
// and must never reuse the example boundaries. See TheoryOfBoundaryUniqueness.
func TestBoundaryUniquenessInPrompts(t *testing.T) {
	handler := BoundaryDiffHandler{}
	systemPrompt := handler.SystemPrompt()
	restatePrompt := handler.RestatePrompt()

	// Both prompts must explicitly forbid reusing example boundaries.
	if !strings.Contains(systemPrompt, "Never reuse") {
		t.Fatal("SystemPrompt must instruct the model never to reuse example boundaries")
	}
	if !strings.Contains(restatePrompt, "Never reuse") {
		t.Fatal("RestatePrompt must instruct the model never to reuse example boundaries")
	}

	// RestatePrompt must generate a boundary per block, not per response.
	if !strings.Contains(restatePrompt, "for each block") {
		t.Fatal("RestatePrompt must instruct to generate a boundary for each block")
	}

	// The example in SystemPrompt must demonstrate distinct boundaries per block
	// rather than reusing one boundary for every block (the original bug).
	exampleStart := strings.Index(systemPrompt, "**Example:**")
	finishStart := strings.Index(systemPrompt, "**Finish Block Kind:**")
	if exampleStart == -1 || finishStart == -1 || finishStart <= exampleStart {
		t.Fatal("could not locate example section in SystemPrompt")
	}
	example := systemPrompt[exampleStart:finishStart]
	boundaries := extractChangeBoundariesFromExample(example)
	if len(boundaries) < 3 {
		t.Fatalf("expected at least 3 change blocks in example, got %d", len(boundaries))
	}
	seen := map[string]bool{}
	for _, b := range boundaries {
		if seen[b] {
			t.Fatalf("example reuses boundary %q across multiple change blocks", b)
		}
		seen[b] = true
	}
}

// TestBlockFormatSystemPromptBoundaryUniqueness verifies the base block format
// prompt contains the boundary uniqueness section that forbids reusing example
// boundaries.
func TestBlockFormatSystemPromptBoundaryUniqueness(t *testing.T) {
	if !strings.Contains(BlockFormatSystemPrompt, "Boundary Uniqueness") {
		t.Fatal("BlockFormatSystemPrompt must contain a Boundary Uniqueness section")
	}
	if !strings.Contains(BlockFormatSystemPrompt, "Never reuse") {
		t.Fatal("BlockFormatSystemPrompt must instruct the model never to reuse example boundaries")
	}
}

// extractChangeBoundariesFromExample collects the boundary string from each
// :::change <boundary> marker in the given text.
func extractChangeBoundariesFromExample(text string) []string {
	const prefix = ":::change "
	var boundaries []string
	for {
		idx := strings.Index(text, prefix)
		if idx == -1 {
			break
		}
		rest := text[idx+len(prefix):]
		end := strings.IndexByte(rest, '\n')
		if end == -1 {
			break
		}
		boundaries = append(boundaries, strings.TrimSpace(rest[:end]))
		text = rest[end:]
	}
	return boundaries
}