package codes

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestModule(t *testing.T) {
	dscope.New(
		new(Module),
		modes.ForTest(t),
	)
}
