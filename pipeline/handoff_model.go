package pipeline

import (
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

const TheoryOfHandoffModel = `
tai handoff model theory:
- The handoff model is a single model specified by HandoffModel. When empty,
  the fast model (FastModelName) is used if configured; otherwise the default
  model (ModelName) is used. A configured but unresolvable model fails with an
  error rather than silently falling back, so misconfiguration is surfaced.
- GetHandoffGenerator provides the handoff model (or the fallback) for thought
  summarization, which needs a single stable generator. GetHandoffGenerators
  provides a single-element slice containing the resolved handoff model for
  handoff generation.
`

// GetHandoffGenerator returns the primary handoff generator: the
// handoff model when configured, otherwise the fast model,
// otherwise the default model. Used by thought summarization, which needs
// a single stable generator. See TheoryOfHandoffModel.
type GetHandoffGenerator func() (generators.Generator, error)

func (Module) GetHandoffGenerator(
	handoffModel flags.HandoffModel,
	fastModel flags.FastModelName,
	defaultGenerator generators.GetDefaultGenerator,
	getGenerator generators.GetGenerator,
) GetHandoffGenerator {
	return func() (generators.Generator, error) {
		if handoffModel != "" {
			return getGenerator(string(handoffModel))
		}
		if fastModel != "" {
			return getGenerator(string(fastModel))
		}
		return defaultGenerator()
	}
}

// GetHandoffGenerators returns a single-element slice containing the
// resolved handoff generator. The list is used by handoff generation.
// See TheoryOfHandoffModel.
type GetHandoffGenerators func() ([]generators.Generator, error)

func (Module) GetHandoffGenerators(
	getHandoffGenerator GetHandoffGenerator,
) GetHandoffGenerators {
	return func() ([]generators.Generator, error) {
		generator, err := getHandoffGenerator()
		if err != nil {
			return nil, err
		}
		return []generators.Generator{generator}, nil
	}
}
