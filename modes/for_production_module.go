package modes

import (
	"testing"

	"github.com/reusee/dscope"
)

type ModuleForProduction struct {
	dscope.Module
}

func ForProduction() ModuleForProduction {
	return ModuleForProduction{}
}

func (ModuleForProduction) T() *testing.T {
	return nil
}

func (ModuleForProduction) Mode() Mode {
	return ModeProduction
}
