package anytexts

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

// Debug configs.Config implementation for the anytexts module.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Debug(false)

type Debug bool

func (Module) Debug() Debug {
	return false
}

var _ flags.Flag = Debug(false)

func (d Debug) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := Debug(true)
	return &ret, args, nil
}

func (d Debug) Keys() map[string]string {
	return map[string]string{
		"-debug-anytexts": "Enable debug logging for the anytexts module",
	}
}

func (d Debug) ConfigPaths() []string {
	return []string{"debug_anytexts"}
}

func (d Debug) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := Debug(b)
	return &ret, nil
}
