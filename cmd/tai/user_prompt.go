package main

import (
	"maps"
	"slices"
	"strings"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

type UserPrompt []generators.Part

func (Module) UserPrompt(
	partsProvider anytexts.PartsProvider,
	getDefaultGenerator generators.GetDefaultGenerator,
	systemPrompt SystemPrompt,
	maxTokens flags.MaxTokens,
	flagFiles flags.Files,
	flagChats flags.Chats,
) UserPrompt {

	generator, err := getDefaultGenerator()
	ce(err)

	args := generator.Spec()
	maxInputTokens := min(
		args.ContextTokens,
		int(maxTokens),
	)
	maxGenerateTokens := 8192
	if args.MaxGenerateTokens != nil {
		maxGenerateTokens = *args.MaxGenerateTokens
	}
	maxInputTokens -= maxGenerateTokens
	systemPromptTokens, err := generator.CountTokens(string(systemPrompt))
	ce(err)
	// The system prompt is charged twice: once as the actual system
	// prompt, and once for the verbatim restate appended at the end of
	// the user prompt (components.SystemPromptRestate), which re-sends
	// the full system prompt inside the user content. See
	// components.TheoryOfComponents.
	maxInputTokens -= systemPromptTokens * 2

	// File patterns come from a map (flags.Files); Go map iteration
	// order is randomized per range, so the keys must be sorted before
	// reaching Parts. Pattern order reaches the prompt bytes: IterFiles
	// enqueues pattern matches in order and deduplicates followed symlink
	// targets by first-wins, so two aliases of one directory yield
	// different file paths depending on which pattern is seen first.
	// Sorting makes the emitted file set — and therefore the prompt
	// prefix — byte-identical across runs with equal configuration.
	// pipeline.Module.Patterns sorts the same map for the codes pipeline;
	// see TheoryOfPrefixCaching in generators/state_func_map.go.
	patterns := slices.Collect(maps.Keys(flagFiles))
	slices.Sort(patterns)

	// The chat input precedes the parts provider content when -chat
	// arguments are given: the model reads the task before the long file
	// context, while the restate after the context re-exposes the rules
	// immediately before generating. The part ends with a blank line so
	// the context starts a fresh paragraph. See
	// pipeline.TheoryOfChatBracketing and
	// generators.TheoryOfContentUnitSeparation.
	var parts []generators.Part
	if chats := strings.Join(flagChats, "\n"); chats != "" {
		parts = append(parts, generators.Text(chats+"\n\n"))
	}
	providerParts, err := partsProvider.Parts(
		maxInputTokens,
		generator.CountTokens,
		patterns,
	)
	ce(err)
	parts = append(parts, providerParts...)

	// The system prompt restate is the last user prompt part before the
	// dynamic user input: the model re-reads the complete instructions
	// verbatim immediately before generating, and the restate is built
	// from the same text as the system prompt so the two can never
	// diverge. It ends with a blank line so the user input that follows
	// starts a fresh paragraph. See components.TheoryOfComponents and
	// generators.TheoryOfContentUnitSeparation.
	parts = append(parts, components.SystemPromptRestate(string(systemPrompt)))

	return UserPrompt(parts)
}
