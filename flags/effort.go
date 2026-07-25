package flags

import (
	"fmt"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// Effort configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Effort("")

type Effort string

func (Module) Effort() (ret Effort) {
	return
}

var _ Flag = Effort("")

func (e Effort) Keys() map[string]string {
	return map[string]string{
		"-effort": "Set the reasoning effort level (e.g. low, medium, high)",
	}
}

func (e Effort) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	newValue = Effort(args[0])
	remainArgs = args[1:]
	return
}

func (e Effort) ConfigPaths() []string {
	return []string{"effort"}
}

func (e Effort) HandleConfig(path string, values []*cue.Value) (any, error) {
	var s string
	if err := values[0].Decode(&s); err != nil {
		return nil, err
	}
	return Effort(s), nil
}
