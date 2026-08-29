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

// ModelFamily is the family of the resolved default generator. It selects
// family-specific extra system prompts. The default provider derives the
// family from the resolved default generator, so no customization is
// needed. See pipeline.TheoryOfFamilyExtraSystemPrompt.
type ModelFamily string

func (Module) ModelFamily(
	getDefaultGenerator GetDefaultGenerator,
) ModelFamily {
	generator, err := getDefaultGenerator()
	if err != nil {
		return ""
	}
	return ModelFamily(generator.Spec().Family)
}
