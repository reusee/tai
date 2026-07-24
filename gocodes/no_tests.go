package gocodes

import (
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type NoTests bool

var _ configs.Configurable = NoTests(true)

func (n NoTests) TaigoConfigurable() {}

var _ flags.Flag = NoTests(true)

func (n NoTests) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return NoTests(true), args, nil
}

func (n NoTests) Keys() map[string]string {
	return map[string]string{
		"-no-tests": "Exclude test files from the context",
	}
}

func (Module) NoTests(
	loader configs.Loader,
) NoTests {
	return configs.First[NoTests](loader, "go.no_tests")
}
