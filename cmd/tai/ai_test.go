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

func TestAISystemPromptExcludesRestatePrompts(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
	).Call(func(
		getSystemPrompt AISystemPrompt,
		comps AIComponents,
	) {
		prompt, err := getSystemPrompt()
		if err != nil {
			t.Fatal(err)
		}
		// System prompt must NOT contain restate prompts (they are placed
		// at the end of the user prompt via UserPromptParts()).
		if strings.Contains(prompt, "Block format (CRITICAL)") {
			t.Fatal("system prompt must not include block format restate prompt")
		}
		if strings.Contains(prompt, "Continue block:") {
			t.Fatal("system prompt must not include continue block restate prompt")
		}
		if strings.Contains(prompt, "Memory block:") {
			t.Fatal("system prompt must not include memory block restate prompt")
		}
		// Restate prompts must be available via comps.RestatePrompts() and
		// comps.UserPromptParts().
		restate := comps.RestatePrompts()
		if !strings.Contains(restate, "Block format (CRITICAL)") {
			t.Fatal("restate prompts must include block format restate prompt")
		}
		if strings.Contains(restate, "Continue block:") {
			t.Fatal("restate prompts must not include continue block restate prompt; the ai command does not process continue blocks")
		}
		if !strings.Contains(restate, "Memory block:") {
			t.Fatal("restate prompts must include memory block restate prompt")
		}
		// UserPromptParts must include restate content.
		userParts := comps.UserPromptParts()
		foundRestate := false
		for _, part := range userParts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "Block format (CRITICAL)") {
					foundRestate = true
				}
			}
		}
		if !foundRestate {
			t.Fatal("UserPromptParts must include restate prompt content")
		}
	})
}

func TestAIRestatePromptsIncludeShellWhenEnabled(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
		func() flags.Shell { return flags.Shell(true) },
	).Call(func(
		comps AIComponents,
	) {
		restate := comps.RestatePrompts()
		if !strings.Contains(restate, "Shell block:") {
			t.Fatal("restate prompts must include shell block restate prompt when shell is enabled")
		}
	})
}

func TestAIRestatePromptsExcludeShellWhenDisabled(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
	).Call(func(
		comps AIComponents,
	) {
		restate := comps.RestatePrompts()
		if strings.Contains(restate, "Shell block:") {
			t.Fatal("restate prompts must not include shell block restate prompt when shell is disabled")
		}
	})
}

func TestAIRestatePromptsExcludeMemoryWhenNoMemory(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
		func() NoMemory { return NoMemory(true) },
	).Call(func(
		comps AIComponents,
	) {
		restate := comps.RestatePrompts()
		if strings.Contains(restate, "Memory block:") {
			t.Fatal("restate prompts must not include memory block restate prompt when noMemory is true")
		}
		// Block format restate prompt should still be present
		if !strings.Contains(restate, "Block format (CRITICAL)") {
			t.Fatal("restate prompts must still include block format restate prompt when noMemory is true")
		}
	})
}

func TestMemoryPromptsUseUncommonChineseDelimiter(t *testing.T) {
	// The delimiter policy mandates exactly three uncommon Chinese characters
	// per block. Both memory prompts must state the policy and must not
	// display the legacy MEMEND example delimiter. See
	// TheoryOfBlockFormatGeneral in blocks/block.go.
	prompt := memoryBlockSystemPrompt("")
	if !strings.Contains(prompt, "非常用汉字") {
		t.Fatal("memoryBlockSystemPrompt must mandate the three-uncommon-Chinese-characters delimiter policy")
	}
	if strings.Contains(prompt, "<<MEMEND") {
		t.Fatal("memoryBlockSystemPrompt must not display the legacy MEMEND example delimiter")
	}
	if !strings.Contains(memoryBlockRestatePrompt, "uncommon Chinese characters") {
		t.Fatal("memoryBlockRestatePrompt must mandate the three-uncommon-Chinese-characters delimiter policy")
	}
	if strings.Contains(memoryBlockRestatePrompt, "<<MEMEND") {
		t.Fatal("memoryBlockRestatePrompt must not display the legacy MEMEND example delimiter")
	}
}

func TestAIComponentsExcludesContinueComponent(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
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
	})
}
