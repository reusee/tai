package codes

import (
	"strings"
	"testing"
)

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
	noteStart := strings.Index(systemPrompt, "Note: Each block above")
	if exampleStart == -1 || noteStart == -1 || noteStart <= exampleStart {
		t.Fatal("could not locate example section in system prompt")
	}
	example := systemPrompt[exampleStart:noteStart]
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

func TestBlockFormatSystemPromptLineStart(t *testing.T) {
	if !strings.Contains(BlockFormatSystemPrompt, "Line-Start Requirement") {
		t.Fatal("BlockFormatSystemPrompt must contain a Line-Start Requirement section")
	}
	if !strings.Contains(BlockFormatSystemPrompt, "beginning of a line") {
		t.Fatal("BlockFormatSystemPrompt must instruct the model to place markers at the beginning of a line")
	}
	if !strings.Contains(BlockFormatSystemPrompt, "NEVER place a marker at the end of a line") {
		t.Fatal("BlockFormatSystemPrompt must explicitly forbid placing markers at the end of a line")
	}
}

func TestRestatePromptLineStart(t *testing.T) {
	handler := BoundaryDiffHandler{}
	restate := handler.RestatePrompt()
	if !strings.Contains(restate, "Line-start requirement") {
		t.Fatal("RestatePrompt must contain a line-start requirement instruction")
	}
	if !strings.Contains(restate, "beginning of its own line") {
		t.Fatal("RestatePrompt must instruct markers to start at the beginning of their own line")
	}
}

func TestSystemPromptLineStart(t *testing.T) {
	handler := BoundaryDiffHandler{}
	systemPrompt := handler.SystemPrompt()
	if !strings.Contains(systemPrompt, "Line-start requirement") {
		t.Fatal("SystemPrompt must contain a line-start requirement instruction")
	}
	if !strings.Contains(systemPrompt, "beginning of its own line") {
		t.Fatal("SystemPrompt must instruct markers to start at the beginning of their own line")
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