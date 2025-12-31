package gocodes

import (
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
)

var noTestsFlag = cmds.Switch("-no-tests")

type NoTests bool

var _ configs.Configurable = NoTests(true)

func (n NoTests) ConfigExpr() string {
	return "Go.NoTests"
}

func (Module) NoTests(
	loader configs.Loader,
) NoTests {
	return NoTests(*noTestsFlag) ||
		configs.First[NoTests](loader, "go.no_tests")
}
