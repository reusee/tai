package codes

import (
	"github.com/reusee/prompts"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/configs"
)

type ExtraSystemPrompt string

var _ExtraSystemPromptConfigurable configs.Configurable = ExtraSystemPrompt("")

func (e ExtraSystemPrompt) TaigoConfigurable() {}

func (Module) ExtraSystemPrompt(
	loader configs.Loader,
) ExtraSystemPrompt {
	return configs.First[ExtraSystemPrompt](loader, "extra_system_prompt")
}

type SystemPrompt string

func (Module) SystemPrompt(
	codeProvider codetypes.CodeProvider,
	diffHandler codetypes.DiffHandler,
	dynamicContext DynamicContext,
	extra ExtraSystemPrompt,
) (ret SystemPrompt) {
	prompt := prompts.Codes + "\n" +
		codeProvider.SystemPrompt() + "\n" +
		diffHandler.SystemPrompt() + "\n" +
		blocks.FinishBlockSystemPrompt + "\n"
	if bool(dynamicContext) {
		prompt += blocks.RequestContextSystemPrompt + "\n"
	}
	prompt += string(extra)
	return SystemPrompt(prompt)
}