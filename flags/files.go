package flags

import (
	"fmt"
	"maps"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// Files configs.Config implementation. Config values are a list of
// patterns; multiple config files are merged additively.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Files(nil)

type Files map[string]bool

func (Module) Files() (ret Files) {
	return
}

var _ Flag = Files(nil)

func (f Files) Keys() map[string]string {
	return map[string]string{
		"-file": "Add a file to the context by path or glob pattern",
	}
}

func (f Files) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	// Copy the existing map to preserve scope immutability; dscope.Get
	// returns the same map reference stored in the scope, so mutating it
	// in place would violate the immutable-scope contract.
	ret := make(Files, len(f)+1)
	maps.Copy(ret, f)
	ret[args[0]] = true
	newValue = ret
	remainArgs = args[1:]
	return
}

func (f Files) ConfigPaths() []string {
	return []string{"files"}
}

func (f Files) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(Files, len(f))
	maps.Copy(ret, f)
	for _, v := range values {
		var patterns []string
		if err := v.Decode(&patterns); err != nil {
			return nil, err
		}
		for _, p := range patterns {
			ret[p] = true
		}
	}
	return ret, nil
}
