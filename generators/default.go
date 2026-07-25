package generators

import (
	"github.com/reusee/tai/flags"
)

type GetDefaultGenerator func() (Generator, error)

func (Module) GetDefaultGenerator(
	name flags.ModelName,
	get GetGenerator,
) GetDefaultGenerator {
	return func() (Generator, error) {
		return get(string(name))
	}
}

type GetDefaultFastModel func() (Generator, error)

func (Module) GetDefaultFastModel(
	name flags.FastModelName,
	get GetGenerator,
) GetDefaultFastModel {
	return func() (Generator, error) {
		return get(string(name))
	}
}
