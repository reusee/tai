package codes

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gocodes"
	"github.com/reusee/tai/modes"
)

func TestSystemPromptGoTestBlock(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Go-Test Block Kind") {
			t.Fatal("system prompt must include go-test block section")
		}
		// The go-test prompt must instruct the model to emit a summary block
		// even when emitting a go-test block. Without this, the model may omit
		// the summary, causing unnecessary retries (see TheoryOfSummaryCompletionRetry
		// in codes/generate.go and TheoryOfGoTestBlocks in blocks/gotest.go).
		if !strings.Contains(string(prompt), "go-test block is NOT a completion signal") {
			t.Fatal("system prompt must state that go-test block is not a completion signal and summary is still required")
		}
	})
}

func TestGoTestComponentPassDoesNotTriggerRound(t *testing.T) {
	goTestBlocks := []blocks.Block{
		{Kind: "go-test", Body: "-run\n___nonexistent___"},
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
	).Call(func(
		comps CodesComponents,
	) {
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
				t.Fatalf("expected no Parts when tests pass, got %d parts", len(result.Parts))
			}
			// BackgroundParts should contain a pass confirmation so that
			// when another component triggers a new round, the model is
			// informed that tests passed and does not re-emit go-test blocks.
			if len(result.BackgroundParts) == 0 {
				t.Fatal("expected BackgroundParts when tests pass")
			}
			text, ok := result.BackgroundParts[0].(generators.Text)
			if !ok {
				t.Fatalf("expected Text part in BackgroundParts, got %T", result.BackgroundParts[0])
			}
			if !strings.Contains(string(text), "passed") {
				t.Fatalf("expected pass confirmation in BackgroundParts, got %q", text)
			}
			return
		}
		t.Fatal("go-test component not found")
	})
}

func TestGoTestComponentFailTriggersRound(t *testing.T) {
	goTestBlocks := []blocks.Block{
		{Kind: "go-test", Body: "-bogusflag"},
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
	).Call(func(
		comps CodesComponents,
	) {
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
			// BackgroundParts should not be set when tests fail — the
			// failure output in Parts is the triggering content.
			if len(result.BackgroundParts) != 0 {
				t.Fatalf("expected no BackgroundParts when tests fail, got %d", len(result.BackgroundParts))
			}
			return
		}
		t.Fatal("go-test component not found")
	})
}

func TestCodesComponentsIncludesFamilyExtraSystemPrompt(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
		func() generators.ModelFamily { return "gemini" },
		func() flags.FamilyExtraSystemPrompt {
			return flags.FamilyExtraSystemPrompt{"gemini": {"gemini family prompt"}}
		},
		func() gocodes.FamilyExtraSystemPrompt {
			return gocodes.FamilyExtraSystemPrompt{"gemini": {"go gemini family prompt"}}
		},
	).Call(func(comps CodesComponents) {
		prompt := comps.PromptSections()
		if !strings.Contains(prompt, "gemini family prompt") {
			t.Fatal("expected top-level family prompt in system prompt")
		}
		if !strings.Contains(prompt, "go gemini family prompt") {
			t.Fatal("expected go-specific family prompt in system prompt")
		}
	})
}

func TestCodesComponentsExcludesNonMatchingFamilyPrompt(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
		func() generators.ModelFamily { return "other" },
		func() flags.FamilyExtraSystemPrompt {
			return flags.FamilyExtraSystemPrompt{"gemini": {"gemini family prompt"}}
		},
	).Call(func(comps CodesComponents) {
		if strings.Contains(comps.PromptSections(), "gemini family prompt") {
			t.Fatal("non-matching family prompt must not be included")
		}
	})
}
