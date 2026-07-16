package codes

import (
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

func TestShellBlockSystemPrompt(t *testing.T) {
	module := Module{}

	t.Run("Disabled", func(t *testing.T) {
		prompt := module.SystemPrompt(
			mockCodeProvider{},
			BoundaryDiffHandler{},
			DynamicContext(false),
			Shell(false),
			ExtraSystemPrompt(""),
		)
		if strings.Contains(string(prompt), "Shell Block Kind") {
			t.Fatal("system prompt must not include shell section when shell is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		prompt := module.SystemPrompt(
			mockCodeProvider{},
			BoundaryDiffHandler{},
			DynamicContext(false),
			Shell(true),
			ExtraSystemPrompt(""),
		)
		if !strings.Contains(string(prompt), "Shell Block Kind") {
			t.Fatal("system prompt must include shell section when shell is enabled")
		}
	})
}

func TestProcessShellBlocks(t *testing.T) {
	state := generators.NewPrompts("", nil)
	blockState := blocks.NewBlockState(state)

	// Append a shell block with echo command
	text := ":::shell 徕珑\necho hello world\n:::end 徕珑\n"
	_, err := blockState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}

	parts, err := processShellBlocks(blockState)
	if err != nil {
		t.Fatalf("processShellBlocks failed: %v", err)
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
	if remaining := blockState.PopBlocksByKind("shell"); len(remaining) != 0 {
		t.Fatalf("expected 0 remaining shell blocks, got %d", len(remaining))
	}
}

func TestProcessShellBlocksCommandFailure(t *testing.T) {
	state := generators.NewPrompts("", nil)
	blockState := blocks.NewBlockState(state)

	text := ":::shell 徕珑\nexit 1\n:::end 徕珑\n"
	_, err := blockState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}

	parts, err := processShellBlocks(blockState)
	if err != nil {
		t.Fatalf("processShellBlocks failed: %v", err)
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
	blockState := blocks.NewBlockState(state)

	parts, err := processShellBlocks(blockState)
	if err != nil {
		t.Fatalf("processShellBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}

func TestShellFlagDefault(t *testing.T) {
	if bool(shellFlag) {
		t.Fatal("shellFlag should default to false (disabled by default)")
	}
}
