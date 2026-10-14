package generators

import (
	"context"

	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/logs"
)

type GetDefaultGenerator func() (Generator, error)

func (Module) GetDefaultGenerator(
	name flags.ModelName,
	get GetGenerator,
	logger logs.Logger,
) GetDefaultGenerator {
	return func() (Generator, error) {
		logger.InfoContext(context.Background(), "resolving default generator",
			"name", string(name),
		)
		generator, err := get(string(name))
		if err != nil {
			logger.WarnContext(context.Background(), "default generator resolution failed",
				"name", string(name),
				"error", err,
			)
			return nil, err
		}
		logger.InfoContext(context.Background(), "default generator resolved",
			"name", string(name),
			"model", generator.Spec().Model,
			"family", generator.Spec().Family,
		)
		return generator, nil
	}
}

// ModelFamily is the family of the resolved default generator. It selects
// family-specific extra system prompts. The default provider derives the
// family from the resolved default generator, so no customization is
// needed. See pipeline.TheoryOfFamilyExtraSystemPrompt.
type ModelFamily string

func (Module) ModelFamily(
	getDefaultGenerator GetDefaultGenerator,
	logger logs.Logger,
) ModelFamily {
	generator, err := getDefaultGenerator()
	if err != nil {
		logger.WarnContext(context.Background(), "model family resolution failed",
			"error", err,
		)
		return ""
	}
	family := generator.Spec().Family
	logger.InfoContext(context.Background(), "model family resolved",
		"family", family,
		"model", generator.Spec().Model,
	)
	return ModelFamily(family)
}
