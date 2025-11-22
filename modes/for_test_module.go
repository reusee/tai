package modes

import (
	"testing"

	"github.com/reusee/dscope"
)

type ModuleForTest struct {
	dscope.Module
	t *testing.T
}

func ForTest(t *testing.T) ModuleForTest {
	return ModuleForTest{
		t: t,
	}
}

func (m ModuleForTest) T() *testing.T {
	return m.t
}

func (m ModuleForTest) Mode() Mode {
	return ModeDevelopment
}
