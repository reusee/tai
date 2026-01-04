package codes

import (
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/prompts"
)

type SystemPrompt string

func (Module) SystemPrompt(
	codeProvider codetypes.CodeProvider,
	diffHandler codetypes.DiffHandler,
) (ret SystemPrompt) {
	return SystemPrompt(
		prompts.Codes + "\n" +
			codeProvider.SystemPrompt() + "\n" +
			diffHandler.SystemPrompt(),
	)
}
