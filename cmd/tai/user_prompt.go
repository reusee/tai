package main

import (
	"maps"
	"slices"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiconfigs"
)

type UserPrompt []generators.Part

func (Module) UserPrompt(
	codeProvider anytexts.CodeProvider,
	generator generators.Generator,
	systemPrompt SystemPrompt,
	maxTokens taiconfigs.MaxTokens,
	flagFiles flags.Files,
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

	return UserPrompt(parts)
}
