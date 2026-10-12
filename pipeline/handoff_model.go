package pipeline

import (
	"fmt"

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
- A resolution failure names the selection that produced the name — the
  handoff model flag or the fast model flag — and wraps the resolver's
  detailed invalid-model report, so the configuration that holds the wrong
  name is known from the error alone.
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
			generator, err := getGenerator(string(handoffModel))
			if err != nil {
				return nil, fmt.Errorf("handoff model %q (set by the -handoff-model flag or the handoff_model config path): %w", handoffModel, err)
			}
			return generator, nil
		}
		if fastModel != "" {
			generator, err := getGenerator(string(fastModel))
			if err != nil {
				return nil, fmt.Errorf("fast model %q (selected as the fallback handoff model): %w", fastModel, err)
			}
			return generator, nil
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
