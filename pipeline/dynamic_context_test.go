package pipeline

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
)

type mockPartsProvider struct{}

var _ codetypes.PartsProvider = mockPartsProvider{}

func (mockPartsProvider) Parts(int, func(string) (int, error), []string) ([]generators.Part, error) {
	return nil, nil
}

func TestSystemPromptDynamicContext(t *testing.T) {
	// Dynamic context is always enabled: the ingest section is an
	// unconditional part of the codes system prompt.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Ingest Block Kind") {
			t.Fatal("system prompt must include ingest block section")
		}
	})
}

func TestSystemPromptIngestBlockNotCompletionSignal(t *testing.T) {
	// Mirrors TestSystemPromptGoSrcBlock: the assembled codes system prompt
	// must teach that an ingest block does not replace the summary block, so
	// the stop-and-wait instruction never licenses omitting the round's
	// summary block. See blocks.TheoryOfIngestBlocks and
	// blocks.TheoryOfSummaryBlocks.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)
		if !strings.Contains(s, "ingest block is NOT a completion signal") {
			t.Fatal("system prompt must state that ingest block is not a completion signal and summary is still required")
		}
	})
}

func TestSystemPromptReadOnlyFiles(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
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

func TestSystemPromptSkeletonFiles(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Skeleton Files") {
			t.Fatal("system prompt must include the skeleton files section")
		}
		if !strings.Contains(string(prompt), "begin of skeleton of file") {
			t.Fatal("system prompt must reference the skeleton marker")
		}
		if !strings.Contains(string(prompt), "ingest block") {
			t.Fatal("system prompt must instruct fetching originals with ingest blocks")
		}
	})
}

func TestSystemPromptGoExtraSystemPrompt(t *testing.T) {
	// go.extra_system_prompt enters only Go sessions: the codes pipeline
	// gates the Go-specific prompts on the session's parts provider being
	// gotools.PartsProvider, so the any_text default command (a non-Go
	// project) never carries them.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return gotools.PartsProvider{} },
		func() gotools.ExtraSystemPrompt {
			return gotools.ExtraSystemPrompt{"go-specific system prompt"}
		},
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "go-specific system prompt") {
			t.Fatal("go session must include go.extra_system_prompt content")
		}
	})

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() gotools.ExtraSystemPrompt {
			return gotools.ExtraSystemPrompt{"go-specific system prompt"}
		},
	).Call(func(
		prompt SystemPrompt,
	) {
		if strings.Contains(string(prompt), "go-specific system prompt") {
			t.Fatal("non-go session must not include go.extra_system_prompt content")
		}
	})
}

func TestSystemPromptContinueBlock(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)
		if !strings.Contains(s, "Continue Block Kind") {
			t.Fatal("system prompt must teach continue blocks in every codes session: continue blocks remain the model's way of prompting the next round's user input")
		}
		if strings.Contains(s, "continue blocks are not accepted in this session") {
			t.Fatal("system prompt must not announce continue blocks as unavailable: the continue mechanism is retained")
		}
	})
}

func TestSystemPromptSummaryBlock(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
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

func TestSystemPromptNoTheoryConstantReferences(t *testing.T) {
	// System prompts are model-facing text, and a theory constant's source
	// may never reach the model's context, so a prompt reference such as
	// "See TheoryOfXxx." is a dangling pointer the model cannot resolve.
	// The assembled prompts of the codes pipeline — the plain prompt and
	// the goal-mode prompt, which embed every kind prompt and the change
	// prompt — must carry no such reference. The "**Theory Storage**"
	// guidance bullet is excluded: it teaches the theory-constant naming
	// convention with illustrative constant names, not a reference to a
	// constant's value. Lines are trimmed before matching because the
	// guidance section indents its sub-bullets.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
		comps CodesComponents,
	) {
		for _, text := range []string{
			string(prompt),
			string(GoalSystemPromptText(comps, "", nil)),
		} {
			for _, line := range strings.Split(text, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "- **Theory Storage**") {
					continue
				}
				if strings.Contains(trimmed, "TheoryOf") {
					t.Fatalf("system prompt must not reference theory text constants: %s", trimmed)
				}
			}
		}
	})
}
