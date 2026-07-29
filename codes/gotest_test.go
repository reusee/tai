package codes

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
)

func TestSystemPromptGoTestBlock(t *testing.T) {
	module := Module{}
	comps := module.CodesComponents(
		mockCodeProvider{},
		flags.ExtraSystemPrompt(""),
		DynamicContext(false),
		flags.Apply(true),
		flags.Plan(false),
		flags.Shell(false),
		nil,
		nil,
	)
	prompt := module.SystemPrompt(
		comps,
		mockCodeProvider{},
	)
	if !strings.Contains(string(prompt), "Go-Test Block Kind") {
		t.Fatal("system prompt must include go-test block section")
	}
	if !strings.Contains(string(prompt), "<go-test>") {
		t.Fatal("system prompt must include go-test block format")
	}
	// The go-test prompt must instruct the model to emit a summary block
	// even when emitting a go-test block. Without this, the model may omit
	// the summary, causing unnecessary retries (see TheoryOfSummaryCompletionRetry
	// in codes/generate.go and TheoryOfGoTestBlocks in blocks/gotest.go).
	if !strings.Contains(string(prompt), "go-test block is NOT a completion signal") {
		t.Fatal("system prompt must state that go-test block is not a completion signal and summary is still required")
	}
}

func TestGoTestComponentPassDoesNotTriggerRound(t *testing.T) {
	module := Module{}
	comps := module.CodesComponents(
		mockCodeProvider{},
		flags.ExtraSystemPrompt(""),
		DynamicContext(false),
		flags.Apply(true),
		flags.Plan(false),
		flags.Shell(false),
		nil,
		nil,
	)

	// Create a go-test block that matches no tests.
	// -run and ___nonexistent___ are on separate lines. go test
	// -run ___nonexistent___ succeeds (exit code 0) because no tests
	// match, so no Parts are returned.
	goTestBlocks := []blocks.Block{
		{Kind: "go-test", Body: "-run\n___nonexistent___"},
	}

	for _, comp := range comps.Processable() {
		if comp.Kind != "go-test" {
			continue
		}
		result := comp.Process(context.Background(), &components.ProcessContext{
			Blocks: goTestBlocks,
		})
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if len(result.Parts) != 0 {
			t.Fatalf("expected no parts when tests pass, got %d parts", len(result.Parts))
		}
		return
	}
	t.Fatal("go-test component not found")
}

func TestGoTestComponentFailTriggersRound(t *testing.T) {
	module := Module{}
	comps := module.CodesComponents(
		mockCodeProvider{},
		flags.ExtraSystemPrompt(""),
		DynamicContext(false),
		flags.Apply(true),
		flags.Plan(false),
		flags.Shell(false),
		nil,
		nil,
	)

	// Create a go-test block using an invalid flag.
	// go test -bogusflag fails immediately with a flag parsing error,
	// so Parts are produced to trigger a new round.
	goTestBlocks := []blocks.Block{
		{Kind: "go-test", Body: "-bogusflag"},
	}

	for _, comp := range comps.Processable() {
		if comp.Kind != "go-test" {
			continue
		}
		result := comp.Process(context.Background(), &components.ProcessContext{
			Blocks: goTestBlocks,
		})
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if len(result.Parts) == 0 {
			t.Fatal("expected parts when tests fail; go-test must produce Parts to trigger a new round")
		}
		return
	}
	t.Fatal("go-test component not found")
}
