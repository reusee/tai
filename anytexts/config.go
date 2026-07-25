package anytexts

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// Debug configs.Config implementation for the anytexts module.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Debug(false)

func (d Debug) ConfigPaths() []string {
	return []string{"debug_anytexts"}
}

func (d Debug) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Debug(b), nil
}
