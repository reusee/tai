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
	codeProvider anytexts.CodeProvider,
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

	parts, err := codeProvider.Parts(
		maxInputTokens,
		generator.CountTokens,
		slices.Collect(maps.Keys(flagFiles)),
	)
	ce(err)

	// Restate prompts are placed at the end of the user prompt, not the
	// system prompt, so critical format reminders are the last thing the
	// model reads before generating. The unified block format restate
	// prompt precedes the change-specific restate prompt, so the model
	// is reminded of the shared heredoc format before the change-specific
	// rules. See blocks.TheoryOfBlockFormatGeneral.
	if hasFiles {
		parts = append(parts, generators.Text(blocks.BlockFormatRestatePrompt+"\n"+changes.ChangeBlockRestatePrompt()))
	}

	return UserPrompt(parts)
}
