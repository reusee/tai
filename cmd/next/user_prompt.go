package main

import (
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiconfigs"
)

type UserPrompt []generators.Part

func (Module) UserPrompt(
	codeProvider anytexts.CodeProvider,
	generator Generator,
	systemPrompt SystemPrompt,
	maxTokens taiconfigs.MaxTokens,
) UserPrompt {

	args := generator.Args()
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
	)
	ce(err)

	return UserPrompt(parts)
}
