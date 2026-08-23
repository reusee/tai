package codes

import (
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes/codetypes"
)

func (Module) PartsProvider(
	anyTextsProvider anytexts.PartsProvider,
) codetypes.PartsProvider {
	return anyTextsProvider
}
