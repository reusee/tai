package modes

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestForTest(t *testing.T) {
	mode := dscope.New(ForTest(t)).Get[Mode]()
	if mode != ModeDevelopment {
		t.Fatal()
	}
}
