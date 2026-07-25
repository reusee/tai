package generators

import (
	"github.com/reusee/tai/flags"
)

// SummarizeThoughts enables the ThoughtsSummarize state layer when true.
// It is controlled by the -summarize-thoughts command-line flag.
// When false (the default), the ThoughtsSummarize layer is a no-op.
type SummarizeThoughts bool

func (Module) SummarizeThoughts() SummarizeThoughts {
	return false
}

var _ flags.Flag = SummarizeThoughts(false)

func (s SummarizeThoughts) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return SummarizeThoughts(true), args, nil
}

func (s SummarizeThoughts) Keys() map[string]string {
	return map[string]string{
		"-summarize-thoughts": "Enable periodic summarization of model reasoning thoughts",
	}
}
