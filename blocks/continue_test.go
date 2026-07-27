package blocks

import (
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessContinueBlocks(t *testing.T) {
	blocks := []Block{
		{Kind: "continue", Body: "Please continue the task."},
	}
	parts := ProcessContinueBlocks(blocks)
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
}

func TestProcessContinueBlocksMultiple(t *testing.T) {
	blocks := []Block{
		{Kind: "continue", Body: "first"},
		{Kind: "continue", Body: "second"},
	}
	parts := ProcessContinueBlocks(blocks)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
}

func TestProcessContinueBlocksNoBlock(t *testing.T) {
	parts := ProcessContinueBlocks(nil)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}
