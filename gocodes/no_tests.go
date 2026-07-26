package gocodes

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type NoTests bool

var _ flags.Flag = NoTests(true)

var _ configs.Config = NoTests(false)

func (n NoTests) ConfigPaths() []string {
	return []string{"go.no_tests"}
}

func (n NoTests) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := NoTests(b)
	return &ret, nil
}

func (n NoTests) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := NoTests(true)
	return &ret, args, nil
}

func (n NoTests) Keys() map[string]string {
	return map[string]string{
		"-no-tests": "Exclude test files from the context",
	}
}

func (Module) NoTests() NoTests {
	return false
}
