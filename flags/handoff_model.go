package flags

import (
	"fmt"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// HandoffModel configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = HandoffModel("")

// HandoffModel is the model used for handoff: the summarization of
// truncated or failed generation output before retry, and periodic
// thought summaries. When empty, the fast model (FastModelName) is used
// if configured; otherwise the default model (ModelName) is used.
// See states.TheoryOfHandoffModel.
type HandoffModel string

func (Module) HandoffModel() HandoffModel {
	return HandoffModel("")
}

var _ Flag = HandoffModel("")

func (m HandoffModel) Keys() map[string]string {
	return map[string]string{
		"-handoff-model": "Set the model used for handoff",
	}
}

func (m HandoffModel) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := HandoffModel(args[0])
	return &ret, args[1:], nil
}

func (m HandoffModel) ConfigPaths() []string {
	return []string{"handoff_model"}
}

func (m HandoffModel) HandleConfig(path string, values []*cue.Value) (any, error) {
	if err := values[0].Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}
