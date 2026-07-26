package flags

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// SummarizeThoughts enables periodic summarization of model reasoning
// thoughts. When true, a dedicated summarizer condenses accumulated
// reasoning traces into concise summaries. It is controlled by the
// -summarize-thoughts flag or the "summarize_thoughts" config path.
type SummarizeThoughts bool

func (Module) SummarizeThoughts() SummarizeThoughts {
	return false
}

var _ Flag = SummarizeThoughts(false)

func (s SummarizeThoughts) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := SummarizeThoughts(true)
	return &ret, args, nil
}

func (s SummarizeThoughts) Keys() map[string]string {
	return map[string]string{
		"-summarize-thoughts": "Enable periodic summarization of model reasoning thoughts",
	}
}

// configs.Config implementation. See TheoryOfConfigFlagParity.

var _ configs.Config = SummarizeThoughts(false)

func (s SummarizeThoughts) ConfigPaths() []string {
	return []string{"summarize_thoughts"}
}

func (s SummarizeThoughts) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := SummarizeThoughts(b)
	return &ret, nil
}
