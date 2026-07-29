package codes

import (
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes/codetypes"
)

func (Module) CodeProvider(
	anyTextsProvider anytexts.CodeProvider,
) codetypes.CodeProvider {
	return anyTextsProvider
}
