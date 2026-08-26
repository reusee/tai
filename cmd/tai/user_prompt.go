package main

import (
	"maps"
	"slices"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
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
	hasFiles HasFiles,
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
	maxInputTokens -= systemPromptTokens

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
	parts, err := partsProvider.Parts(
		maxInputTokens,
		generator.CountTokens,
		patterns,
	)
	ce(err)

	// Restate prompts are placed at the end of the user prompt, not in
	// the system prompt, so critical format reminders are the last thing
	// the model reads before generating. The unified block format restate
	// prompt precedes the change-specific restate prompt, so the model
	// is reminded of the shared heredoc format before the change-specific
	// rules. The two constants are joined with a blank line so they stay
	// separate paragraphs. See blocks.TheoryOfBlockFormatGeneral and
	// generators.TheoryOfContentUnitSeparation.
	if hasFiles {
		parts = append(parts, generators.Text(blocks.BlockFormatRestatePrompt+"\n\n"+changes.ChangeBlockRestatePrompt()))
	}

	return UserPrompt(parts)
}
