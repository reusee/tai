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

func TestAISystemPromptIncludesRestatePrompts(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
	).Call(func(
		getSystemPrompt AISystemPrompt,
	) {
		prompt, err := getSystemPrompt()
		if err != nil {
			t.Fatal(err)
		}
		// Block format restate prompt
		if !strings.Contains(prompt, "Block format (CRITICAL)") {
			t.Fatal("system prompt must include block format restate prompt")
		}
		// Continue block restate prompt (always included via CommonComponents)
		if !strings.Contains(prompt, "Continue block:") {
			t.Fatal("system prompt must include continue block restate prompt")
		}
		// Memory block restate prompt (included by default, noMemory=false)
		if !strings.Contains(prompt, "Memory block:") {
			t.Fatal("system prompt must include memory block restate prompt")
		}
	})
}

func TestAISystemPromptIncludesShellRestatePromptWhenEnabled(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
		func() flags.Shell { return flags.Shell(true) },
	).Call(func(
		getSystemPrompt AISystemPrompt,
	) {
		prompt, err := getSystemPrompt()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, "Shell block:") {
			t.Fatal("system prompt must include shell block restate prompt when shell is enabled")
		}
	})
}

func TestAISystemPromptExcludesShellRestatePromptWhenDisabled(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
	).Call(func(
		getSystemPrompt AISystemPrompt,
	) {
		prompt, err := getSystemPrompt()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(prompt, "Shell block:") {
			t.Fatal("system prompt must not include shell block restate prompt when shell is disabled")
		}
	})
}

func TestAISystemPromptExcludesMemoryRestatePromptWhenNoMemory(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.Generator { return aiMockGenerator{} },
		func() NoMemory { return NoMemory(true) },
	).Call(func(
		getSystemPrompt AISystemPrompt,
	) {
		prompt, err := getSystemPrompt()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(prompt, "Memory block:") {
			t.Fatal("system prompt must not include memory block restate prompt when noMemory is true")
		}
		// Block format restate prompt should still be present
		if !strings.Contains(prompt, "Block format (CRITICAL)") {
			t.Fatal("system prompt must still include block format restate prompt when noMemory is true")
		}
	})
}

func TestMemoryPromptsUseUncommonChineseDelimiter(t *testing.T) {
	// The delimiter policy mandates exactly two uncommon Chinese characters
	// per block. Both memory prompts must state the policy and must not
	// display the legacy MEMEND example delimiter. See
	// TheoryOfBlockFormatGeneral in blocks/block.go.
	prompt := memoryBlockSystemPrompt("")
	if !strings.Contains(prompt, "非常用汉字") {
		t.Fatal("memoryBlockSystemPrompt must mandate the two-uncommon-Chinese-characters delimiter policy")
	}
	if strings.Contains(prompt, "<<MEMEND") {
		t.Fatal("memoryBlockSystemPrompt must not display the legacy MEMEND example delimiter")
	}
	if !strings.Contains(memoryBlockRestatePrompt, "uncommon Chinese characters") {
		t.Fatal("memoryBlockRestatePrompt must mandate the two-uncommon-Chinese-characters delimiter policy")
	}
	if strings.Contains(memoryBlockRestatePrompt, "<<MEMEND") {
		t.Fatal("memoryBlockRestatePrompt must not display the legacy MEMEND example delimiter")
	}
}
