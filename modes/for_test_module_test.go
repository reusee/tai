package modes

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestForTest(t *testing.T) {
	dscope.New(ForTest(t)).Call(func(
		t *testing.T,
		mode Mode,
	) {
		if mode != ModeDevelopment {
			t.Fatal()
		}
	})
}
