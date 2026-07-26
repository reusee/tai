package flags

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// Focus configs.Config implementation. Config values are a list of
// aspects; multiple config files are merged additively with the
// existing flag-accumulated value.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Focus(nil)

type Focus []string

func (Module) Focus() (ret Focus) {
	return
}

var _ Flag = Focus(nil)

func (f Focus) Keys() map[string]string {
	return map[string]string{
		"-focus": "Focus on a specific aspect of the task",
	}
}

func (f Focus) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := append(slices.Clone(f), args[0])
	return &ret, args[1:], nil
}

func (f Focus) ConfigPaths() []string {
	return []string{"focus"}
}

func (f Focus) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(Focus, len(f))
	copy(ret, f)
	for _, v := range values {
		var items []string
		if err := v.Decode(&items); err != nil {
			return nil, err
		}
		ret = append(ret, items...)
	}
	return &ret, nil
}
