package modes

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestModuleForProduction(t *testing.T) {
	dscope.New(new(ModuleForProduction)).Call(func(
		t *testing.T,
		mode Mode,
	) {
		if mode != ModeProduction {
			t.Fatal()
		}
	})
}
