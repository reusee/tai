package main

import (
	"maps"
	"slices"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

type UserPrompt []generators.Part

func (Module) UserPrompt(
	codeProvider anytexts.CodeProvider,
	generator generators.Generator,
	systemPrompt SystemPrompt,
	maxTokens flags.MaxTokens,
	flagFiles flags.Files,
	hasGoFiles HasGoFiles,
) UserPrompt {

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
	// model reads before generating.
	if hasGoFiles {
		parts = append(parts, generators.Text(changes.ChangeBlockRestatePrompt()))
	}

	return UserPrompt(parts)
}
