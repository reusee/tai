package codes

import (
	"github.com/reusee/prompts"
	"github.com/reusee/tai/codes/codetypes"
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
