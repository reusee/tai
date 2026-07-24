package codes

import "github.com/reusee/tai/flags"

const TheoryOfFullThoughts = `
When thoughts are displayed, the default behavior uses ThoughtsSummarize to
condense reasoning traces into periodic summaries, enabling users to quickly
assess the model's thinking direction without being overwhelmed by raw
thought streams. The summarizer uses the fast model (configured via
fast_model or fast_model_name in tai.cue) via GetDefaultSummarizer, not the
main generation model, to minimize latency and cost. The -full-thoughts flag
opts into raw thought display, bypassing summarization for users who need the
complete reasoning trace. This flag only has effect when thoughts are already
enabled; when thoughts are disabled, -full-thoughts has no effect. The two
flags are orthogonal: -thoughts controls whether thoughts are shown at all,
while -full-thoughts controls the presentation format (summarized vs raw)
when they are shown.
`

// FullThoughts controls whether raw reasoning thoughts are displayed
// without summarization. When false (the default) and thoughts are
// enabled, ThoughtsSummarize condenses reasoning traces into periodic
// summaries. When true, raw thoughts are displayed directly.
// See TheoryOfFullThoughts.
type FullThoughts bool

func (Module) FullThoughts() FullThoughts {
	return false
}

var _ flags.Flag = FullThoughts(false)

func (f FullThoughts) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return FullThoughts(true), args, nil
}

func (f FullThoughts) Keys() map[string]string {
	return map[string]string{
		"-full-thoughts": "Show full reasoning thoughts without summarization",
	}
}
