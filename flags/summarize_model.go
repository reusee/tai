package flags

import (
	"fmt"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// SummarizeModel configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = SummarizeModel("")

// SummarizeModel is the model used for summarization: periodic thought
// summaries and retry summarization of incomplete output. When empty, the
// fast model (FastModelName) is used if configured; otherwise the default
// model (ModelName) is used. See states.TheoryOfSummarizeModel.
type SummarizeModel string

func (Module) SummarizeModel() SummarizeModel {
	return SummarizeModel("")
}

var _ Flag = SummarizeModel("")

func (m SummarizeModel) Keys() map[string]string {
	return map[string]string{
		"-summarize-model": "Set the model used for summarization",
	}
}

func (m SummarizeModel) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := SummarizeModel(args[0])
	return &ret, args[1:], nil
}

func (m SummarizeModel) ConfigPaths() []string {
	return []string{"summarize_model"}
}

func (m SummarizeModel) HandleConfig(path string, values []*cue.Value) (any, error) {
	if err := values[0].Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}
