package modes

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestModuleForProduction(t *testing.T) {
	mode := dscope.New(new(ModuleForProduction)).Get[Mode]()
	if mode != ModeProduction {
		t.Fatal()
	}
}
