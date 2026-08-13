package states

import (
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

const TheoryOfSummarizeModel = `
tai summarization model theory:
- The summarization model is selected in order: SummarizeModel when
  configured, then the fast model (FastModelName), then the default model
  (ModelName). Selection is by configuration presence: an empty
  SummarizeModel falls through to the fast model, and an empty fast model
  falls through to the default model. A configured but unresolvable model
  fails with an error rather than silently falling back, so
  misconfiguration is surfaced.
- GetSummarizeGenerator provides the selection as a dscope provider. Both
  thought summarization (GetDefaultSummarizer) and retry summarization
  (codes.GenerateWithResultWithStats) consume it, so one flag configures
  every summarization path. Previously the main generation model was used
  for all summarization; the option to use a faster model reduces cost and
  latency when the summarizing model is capable enough.
`

// GetSummarizeGenerator returns the generator used for summarization. The
// selection is: SummarizeModel when configured, otherwise the fast model
// when configured, otherwise the default model. See
// TheoryOfSummarizeModel.
type GetSummarizeGenerator func() (generators.Generator, error)

func (Module) GetSummarizeGenerator(
	summarizeModel flags.SummarizeModel,
	fastModel flags.FastModelName,
	defaultGenerator generators.GetDefaultGenerator,
	getGenerator generators.GetGenerator,
) GetSummarizeGenerator {
	return func() (generators.Generator, error) {
		if summarizeModel != "" {
			return getGenerator(string(summarizeModel))
		}
		if fastModel != "" {
			return getGenerator(string(fastModel))
		}
		return defaultGenerator()
	}
}
