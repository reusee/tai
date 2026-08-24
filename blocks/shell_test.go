package blocks

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestProcessShellBlocks(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "echo hello world"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background())
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
}

func TestProcessShellBlocksSeparatesOutputsWithBlankLine(t *testing.T) {
	// Each shell output unit ends with a blank line so consecutive outputs
	// in the same round stay paragraph-separated after verbatim part
	// concatenation. See generators.TheoryOfContentUnitSeparation.
	blocks := []Block{
		{Kind: "shell", Body: "echo one"},
		{Kind: "shell", Body: "echo two"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background())
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	for i, part := range parts {
		output := string(part.(generators.Text))
		if !strings.HasSuffix(output, "\n\n") {
			t.Fatalf("shell output %d must end with a blank line, got %q", i, output)
		}
	}
}

func TestProcessShellBlocksCommandFailure(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "cat /nonexistent"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background())
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
	parts, err := ProcessShellBlocks(nil, context.Background())
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}

func TestProcessShellBlocksRejectsForbiddenCommand(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "rm -rf /tmp/test"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background())
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "Shell command rejected") {
		t.Fatalf("expected output to contain 'Shell command rejected', got: %s", output)
	}
	if !strings.Contains(output, "rm") {
		t.Fatalf("expected output to mention the rejected command, got: %s", output)
	}
}

func TestProcessShellBlocksRejectsRedirection(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "echo hello > /tmp/test"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background())
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "Shell command rejected") {
		t.Fatalf("expected output to contain 'Shell command rejected', got: %s", output)
	}
	if !strings.Contains(output, "redirection") {
		t.Fatalf("expected output to mention redirection, got: %s", output)
	}
}

func TestProcessShellBlocksAllowsGitStatus(t *testing.T) {
	blocks := []Block{
		{Kind: "shell", Body: "git status"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background())
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}
	output := string(parts[0].(generators.Text))
	if strings.Contains(output, "Shell command rejected") {
		t.Fatalf("git status should be allowed, got: %s", output)
	}
}

func TestProcessShellBlocksFiltersByKind(t *testing.T) {
	blocks := []Block{
		{Kind: "summary", Body: "echo hello"},
		{Kind: "shell", Body: "echo hello world"},
	}
	parts, err := ProcessShellBlocks(blocks, context.Background())
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	output := string(parts[0].(generators.Text))
	if !strings.Contains(output, "hello world") {
		t.Fatalf("expected output to contain 'hello world', got: %s", output)
	}
}

func TestShellPromptsWaitForResults(t *testing.T) {
	// Shell output is returned as user content only in the NEXT round, so
	// the model must not emit content that depends on shell output in the
	// same response. Multiple independent shell blocks in one response are
	// allowed, but a shell block whose command depends on another shell
	// block's output — or a change block that depends on shell output —
	// would act on results that have not yet arrived, creating pointless
	// loops. The prompts must state the wait-for-results semantics
	// explicitly. See TheoryOfShellBlocks.
	prompts := map[string]string{
		"ShellBlockSystemPrompt":  ShellBlockSystemPrompt,
		"ShellBlockRestatePrompt": ShellBlockRestatePrompt,
	}
	for name, prompt := range prompts {
		if strings.Contains(prompt, "ONE shell block") {
			t.Fatalf("%s must not restrict the model to a single shell block per response", name)
		}
		if !strings.Contains(prompt, "NEXT round") {
			t.Fatalf("%s must state that shell output is returned only in the next round", name)
		}
		if !strings.Contains(prompt, "summary block") {
			t.Fatalf("%s must instruct the model to end the response with a summary block after emitting shell blocks", name)
		}
		if !strings.Contains(prompt, "independent") {
			t.Fatalf("%s must state that multiple shell blocks are only allowed when their commands are independent", name)
		}
		if !strings.Contains(prompt, "Never end a response on a shell block") {
			t.Fatalf("%s must state the sequence rule: the block after the last shell block must be the summary block", name)
		}
	}
}
