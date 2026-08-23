package pipeline

import (
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/pipeline/codetypes"
)

func (Module) PartsProvider(
	anyTextsProvider anytexts.PartsProvider,
) codetypes.PartsProvider {
	return anyTextsProvider
}
