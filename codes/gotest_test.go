package codes

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

func TestSystemPromptGoTestBlock(t *testing.T) {
	module := Module{}
	comps := module.CodesComponents(
		BoundaryDiffHandler{},
		mockCodeProvider{},
		ExtraSystemPrompt(""),
		DynamicContext(false),
		Apply(true),
		Plan(false),
		flags.Shell(false),
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
		BoundaryDiffHandler{},
		mockCodeProvider{},
		ExtraSystemPrompt(""),
		DynamicContext(false),
		Apply(true),
		Plan(false),
		flags.Shell(false),
	)

	// Create a ParserState with a go-test block that matches no tests.
	// go test -run ___nonexistent___ succeeds (exit code 0) because no
	// tests match, so no Parts are returned.
	state := generators.NewPrompts("", nil)
	parserState := blocks.NewParserState(state)
	text := ":::徕珑 <go-test>\n-run ___nonexistent___\n:::徕珑 </go-test>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*blocks.ParserState)

	for _, comp := range comps.Processable() {
		if comp.Kind != "go-test" {
			continue
		}
		result := comp.Process(context.Background(), &components.ProcessContext{
			ParserState: parserState,
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
		BoundaryDiffHandler{},
		mockCodeProvider{},
		ExtraSystemPrompt(""),
		DynamicContext(false),
		Apply(true),
		Plan(false),
		flags.Shell(false),
	)

	// Create a ParserState with a go-test block using an invalid flag.
	// go test -bogusflag fails immediately with a flag parsing error,
	// so Parts are produced to trigger a new round.
	state := generators.NewPrompts("", nil)
	parserState := blocks.NewParserState(state)
	text := ":::徕珑 <go-test>\n-bogusflag\n:::徕珑 </go-test>\n"
	newState, err := parserState.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	parserState = newState.(*blocks.ParserState)

	for _, comp := range comps.Processable() {
		if comp.Kind != "go-test" {
			continue
		}
		result := comp.Process(context.Background(), &components.ProcessContext{
			ParserState: parserState,
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
