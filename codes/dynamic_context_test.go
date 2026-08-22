package codes

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
)

type mockCodeProvider struct{}

var _ codetypes.CodeProvider = mockCodeProvider{}

func (mockCodeProvider) Parts(int, func(string) (int, error), []string) ([]generators.Part, error) {
	return nil, nil
}

func TestSystemPromptDynamicContext(t *testing.T) {
	// Dynamic context is always enabled: the request-context section is an
	// unconditional part of the codes system prompt.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Request-Context Block Kind") {
			t.Fatal("system prompt must include request-context section")
		}
	})
}

func TestSystemPromptRequestContextNotCompletionSignal(t *testing.T) {
	// Mirrors TestSystemPromptGoSrcBlock: the assembled codes system prompt
	// must teach that a request-context block does not replace the summary
	// block, so the stop-and-wait instruction never licenses omitting the
	// round's summary block. See blocks.TheoryOfRequestContext and
	// blocks.TheoryOfSummaryBlocks.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)
		if !strings.Contains(s, "request-context block is NOT a completion signal") {
			t.Fatal("system prompt must state that request-context block is not a completion signal and summary is still required")
		}
	})
}

func TestSystemPromptReadOnlyFiles(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Read-Only Files") {
			t.Fatal("system prompt must include the read-only files section")
		}
		if !strings.Contains(string(prompt), "read-only") {
			t.Fatal("system prompt must reference read-only files")
		}
	})
}

func TestSystemPromptGoExtraSystemPrompt(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
		func() gotools.ExtraSystemPrompt {
			return gotools.ExtraSystemPrompt{"go-specific system prompt"}
		},
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "go-specific system prompt") {
			t.Fatal("system prompt must include go.extra_system_prompt content")
		}
	})
}

func TestSystemPromptContinueBlock(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
		func() flags.Plan { return true },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Continue Block Kind") {
			t.Fatal("system prompt must include continue block section")
		}
		if !strings.Contains(string(prompt), "Task Decomposition") {
			t.Fatal("system prompt must include task decomposition strategy for complex tasks")
		}
		if !strings.Contains(string(prompt), "task list") {
			t.Fatal("system prompt must include task list concept for multi-round continue blocks")
		}
	})
}

func TestSystemPromptMandatoryPlanning(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
		func() flags.Plan { return true },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)
		if !strings.Contains(s, "Mandatory Planning") {
			t.Fatal("system prompt must include the mandatory planning section")
		}
		if !strings.Contains(s, "overall plan") {
			t.Fatal("system prompt must require an overall plan before any change blocks")
		}
		if !strings.Contains(s, "Emit NO change blocks in the planning round") {
			t.Fatal("system prompt must forbid change blocks in the planning round")
		}
		if !strings.Contains(s, "supersedes") {
			t.Fatal("system prompt must state the mandate supersedes the single-response exemption")
		}
	})
}

func TestSystemPromptDecompositionPrecedesAnalysis(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
		func() flags.Plan { return true },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)
		if !strings.Contains(s, "precede any action") {
			t.Fatal("system prompt must state that decomposition must precede any action including analysis")
		}
		if !strings.Contains(s, "partition the input space") {
			t.Fatal("system prompt must require partitioning the input space for composite tasks")
		}
		if !strings.Contains(s, "find bugs and fix") {
			t.Fatal("system prompt must use the find-bugs-and-fix example to illustrate analysis-phase decomposition")
		}
	})
}

func TestSystemPromptTaskDecompositionStrategies(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
		func() flags.Plan { return true },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)

		categories := []string{
			"Structural strategies",
			"Adaptive strategies",
			"Quality strategies",
			"Scheduling strategies",
		}
		for _, c := range categories {
			if !strings.Contains(s, c) {
				t.Fatalf("system prompt must include category %q", c)
			}
		}

		strategies := []string{
			"Input-driven",
			"Logical-step-driven",
			"Interface-first",
			"Independence-driven",
			"Output-length-driven",
			"Progressive refinement",
			"Error recovery",
			"Feedback-driven",
			"Verification-driven",
			"Risk-driven",
			"Context-collection-first",
			"Dependency-driven",
			"Blast-radius-driven",
			"Token-budget-driven",
			"Reversibility-driven",
		}
		for _, strategy := range strategies {
			if !strings.Contains(s, strategy) {
				t.Fatalf("system prompt must include strategy %q", strategy)
			}
		}
	})
}

func TestSystemPromptSummaryBlock(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.CodeProvider { return mockCodeProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Summary Block Kind") {
			t.Fatal("system prompt must include summary block section")
		}
		if !strings.Contains(string(prompt), "bullet list") {
			t.Fatal("system prompt must describe the summary body as a bullet list")
		}
	})
}
