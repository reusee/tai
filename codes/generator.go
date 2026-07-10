package codes

import (
	"sync"

	"github.com/reusee/tai/generators"
)

type GetDefaultGenerator func() (generators.Generator, error)

func (Module) GetDefaultGenerator(
	get generators.GetDefaultGenerator,
) GetDefaultGenerator {
	return sync.OnceValues(get)
}
