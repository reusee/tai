package main

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

// aiMockGenerator satisfies generators.Generator for tests that need
// memories.CurrentMemory resolved via the memories.Memory provider.
type aiMockGenerator struct{}

func (aiMockGenerator) Spec() generators.Spec { return generators.Spec{Model: "test-model"} }

func (aiMockGenerator) CountTokens(string) (int, error) { return 0, nil }

func (aiMockGenerator) Generate(context.Context, generators.State, *generators.GenerateOptions) (generators.State, error) {
	return nil, nil
}

func TestMemoryPromptsUseUncommonChineseDelimiter(t *testing.T) {
	// The delimiter policy lives only in blocks.BlockFormatSystemPrompt,
	// which AIComponents embeds as a prompt-only component. The memory
	// prompts describe only the memory kind and must not restate the
	// policy or display the legacy MEMEND example delimiter. See
	// TheoryOfBlockFormatGeneral in blocks/block.go.
	prompt := memoryBlockSystemPrompt("")
	if strings.Contains(prompt, "非常用汉字") {
		t.Fatal("memoryBlockSystemPrompt must not restate the delimiter policy; the unified BlockFormatSystemPrompt covers it")
	}
	if strings.Contains(prompt, "<<MEMEND") {
		t.Fatal("memoryBlockSystemPrompt must not display the legacy MEMEND example delimiter")
	}
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return aiMockGenerator{}, nil
			}
		},
	).Call(func(comps AIComponents) {
		if !strings.Contains(comps.PromptSections(), "uncommon Chinese two-character word") {
			t.Fatal("AIComponents must embed the unified BlockFormatSystemPrompt, which states the delimiter policy")
		}
	})
}

func TestMemoryPromptsTeachDeletion(t *testing.T) {
	// The processing side (memories.UpdateMemoryFromBlock) parses
	// <memory-delete> entries and delete_user_profile pseudo-calls, with
	// deletion taking precedence over addition in the same round. The ai
	// command's memory prompts must teach the deletion syntax, otherwise
	// the mechanism is unreachable through normal model output. See
	// memories.TheoryOfMemory.
	prompt := memoryBlockSystemPrompt("")
	if !strings.Contains(prompt, "memory-delete") {
		t.Fatal("memoryBlockSystemPrompt must teach the memory-delete element")
	}
	if !strings.Contains(prompt, "删除生效") {
		t.Fatal("memoryBlockSystemPrompt must state that deletion wins over addition in the same round")
	}
}

func TestAIComponentsExcludesContinueComponent(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return aiMockGenerator{}, nil
			}
		},
	).Call(func(
		comps AIComponents,
	) {
		// The interactive ai chat receives user input through OnIdle, so a
		// continue component would only feed the model's own body back as
		// user content, allowing meaningless self-prompts (e.g., "Please
		// provide the next task or user input") to bypass the prompt.
		// See TheoryOfAIComponents.
		if strings.Contains(comps.PromptSections(), "Continue Block Kind") {
			t.Fatal("ai command must not include the continue block prompt section")
		}
		for _, comp := range comps.Processable() {
			if comp.Kind == "continue" {
				t.Fatal("ai command must not include a processable continue component")
			}
		}

		// Disabled kinds are announced explicitly so the model does not
		// emit them from habit; unprocessed blocks would be silently
		// ignored while implying actions that never happened. See
		// components.TheoryOfDisabledBlocks.
		prompt := comps.PromptSections()
		if !strings.Contains(prompt, "Disabled Block Kinds") {
			t.Fatal("ai command should carry the disabled-blocks notice")
		}
		if !strings.Contains(prompt, "continue blocks are not accepted") {
			t.Fatal("disabled-blocks notice should list continue")
		}
		if !strings.Contains(prompt, "change blocks are not processed") {
			t.Fatal("disabled-blocks notice should list change")
		}
		if !strings.Contains(prompt, "shell execution is disabled") {
			t.Fatal("disabled-blocks notice should list shell when the flag is off")
		}
		if strings.Contains(prompt, "the user profile is not updated") {
			t.Fatal("disabled-blocks notice must not list memory when memory is enabled")
		}
		for _, comp := range comps.Processable() {
			if comp.PromptSection != "" && strings.Contains(comp.PromptSection, "Disabled Block Kinds") {
				t.Fatal("the notice component must be prompt-only, never processable")
			}
		}
	})
}

func TestNextSystemPromptListsDisabledBlocks(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return aiMockGenerator{}, nil
			}
		},
	).Call(func(
		systemPrompt SystemPrompt,
	) {
		// The next command runs a single-shot loop with no components, so
		// the component-driven kinds are never processed here. The notice
		// states that explicitly. See components.TheoryOfDisabledBlocks
		// and TheoryOfNextCommand.
		prompt := string(systemPrompt)
		if !strings.Contains(prompt, "Disabled Block Kinds") {
			t.Fatal("next system prompt should carry the disabled-blocks notice")
		}
		if !strings.Contains(prompt, "shell execution is disabled") {
			t.Fatal("next disabled-blocks notice should list shell")
		}
		if !strings.Contains(prompt, "continue blocks are not accepted") {
			t.Fatal("next disabled-blocks notice should list continue")
		}
		if !strings.Contains(prompt, "additional files and network resources are not fetched") {
			t.Fatal("next disabled-blocks notice should list read")
		}
		if strings.Contains(prompt, "change blocks are not processed") {
			t.Fatal("next disabled-blocks notice must not list change; change blocks are applied by the BlockHandler")
		}
	})
}

func TestAIComponentsIncludesFamilyExtraSystemPrompt(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return aiMockGenerator{}, nil
			}
		},
		func() generators.ModelFamily { return "gemini" },
		func() flags.FamilyExtraSystemPrompt {
			return flags.FamilyExtraSystemPrompt{"gemini": {"gemini family prompt"}}
		},
	).Call(func(comps AIComponents) {
		if !strings.Contains(comps.PromptSections(), "gemini family prompt") {
			t.Fatal("expected family prompt in AI system prompt")
		}
	})
}
