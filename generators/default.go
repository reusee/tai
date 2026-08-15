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

// ModelFamily is the family of the resolved default generator. It selects
// family-specific extra system prompts. The default provider returns an
// empty family; the tai command forks this type with the resolved
// generator's family. See codes.TheoryOfFamilyExtraSystemPrompt.
type ModelFamily string

func (Module) ModelFamily() ModelFamily {
	return ""
}

func (Module) GetDefaultFastModel(
	name flags.FastModelName,
	get GetGenerator,
) GetDefaultFastModel {
	return func() (Generator, error) {
		return get(string(name))
	}
}
