package blocks

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessGoTestBlocks(t *testing.T) {
	t.Run("TestsFail", func(t *testing.T) {
		// -run and Test[ on separate lines. Test[ is an invalid
		// regex, causing go test to fail with a regexp parsing error.
		blocks := []Block{
			{Kind: "go-test", Body: "-run\nTest["},
		}
		parts, failed, err := ProcessGoTestBlocks(blocks, context.Background())
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
		blocks := []Block{
			{Kind: "go-test", Body: "-run\n___nonexistent___"},
		}
		parts, failed, err := ProcessGoTestBlocks(blocks, context.Background())
		if err != nil {
			t.Fatalf("ProcessGoTestBlocks failed: %v", err)
		}
		if !failed {
			t.Fatal("expected failed=true to trigger a new round even when tests pass")
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
	parts, failed, err := ProcessGoTestBlocks(nil, context.Background())
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
