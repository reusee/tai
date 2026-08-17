package states

import (
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

const TheoryOfHandoffModel = `
tai handoff model theory:
- The handoff models are a list tried in order during handoff retries:
  each retry attempt uses the next model, cycling back to the beginning
  when the list is exhausted. This provides fault tolerance: if one model's
  API is down or returns empty responses, the next model in the list is
  tried without waiting for the full retry budget to expire on a single
  model.
- Selection: HandoffModels when configured (non-empty list), then the fast
  model (FastModelName), then the default model (ModelName). An empty
  HandoffModels falls through to the fast model; an empty fast model falls
  through to the default model. A configured but unresolvable model fails
  with an error rather than silently falling back, so misconfiguration is
  surfaced.
- GetHandoffGenerator (singular) provides the first model in the list (or
  the fallback) for thought summarization, which needs a single stable
  generator. GetHandoffGenerators (plural) provides the full resolved list
  for handoff generation, which cycles through all models on retry.
`

// GetHandoffGenerator returns the primary handoff generator: the first
// model in HandoffModels when configured, otherwise the fast model,
// otherwise the default model. Used by thought summarization, which needs
// a single stable generator. See TheoryOfHandoffModel.
type GetHandoffGenerator func() (generators.Generator, error)

func (Module) GetHandoffGenerator(
	handoffModels flags.HandoffModels,
	fastModel flags.FastModelName,
	defaultGenerator generators.GetDefaultGenerator,
	getGenerator generators.GetGenerator,
) GetHandoffGenerator {
	return func() (generators.Generator, error) {
		if len(handoffModels) > 0 {
			return getGenerator(handoffModels[0])
		}
		if fastModel != "" {
			return getGenerator(string(fastModel))
		}
		return defaultGenerator()
	}
}

// GetHandoffGenerators returns the full list of handoff generators resolved
// from HandoffModels, the fast model, or the default model. The list is
// used by handoff generation to cycle through models on retry: each attempt
// uses the next generator, wrapping around when the list is exhausted.
// See TheoryOfHandoffModel.
type GetHandoffGenerators func() ([]generators.Generator, error)

func (Module) GetHandoffGenerators(
	handoffModels flags.HandoffModels,
	fastModel flags.FastModelName,
	defaultGenerator generators.GetDefaultGenerator,
	getGenerator generators.GetGenerator,
) GetHandoffGenerators {
	return func() ([]generators.Generator, error) {
		var names []string
		if len(handoffModels) > 0 {
			names = handoffModels
		} else if fastModel != "" {
			names = []string{string(fastModel)}
		}
		if len(names) == 0 {
			gen, err := defaultGenerator()
			if err != nil {
				return nil, err
			}
			return []generators.Generator{gen}, nil
		}
		gens := make([]generators.Generator, 0, len(names))
		for _, name := range names {
			gen, err := getGenerator(name)
			if err != nil {
				return nil, err
			}
			gens = append(gens, gen)
		}
		return gens, nil
	}
}
