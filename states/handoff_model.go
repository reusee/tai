package states

import (
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

const TheoryOfHandoffModel = `
tai handoff model theory:
- The handoff model is selected in order: HandoffModel when
  configured, then the fast model (FastModelName), then the default model
  (ModelName). Selection is by configuration presence: an empty
  HandoffModel falls through to the fast model, and an empty fast model
  falls through to the default model. A configured but unresolvable model
  fails with an error rather than silently falling back, so
  misconfiguration is surfaced.
- GetHandoffGenerator provides the selection as a dscope provider. Both
  handoff generation (codes.GenerateWithResultWithStats) and thought
  summarization (GetDefaultSummarizer) consume it, so one flag configures
  every handoff and summarization path.
`

// GetHandoffGenerator returns the generator used for handoff: the
// summarization of truncated or failed generation output before retry,
// and periodic thought summaries. The selection is: HandoffModel when
// configured, otherwise the fast model when configured, otherwise the
// default model. See TheoryOfHandoffModel.
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
