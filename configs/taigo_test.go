package configs

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/taigo"
)

type testInt int

var _ Configurable = testInt(0)

func (t testInt) TaigoConfigurable() {}

func TestTaigoDefs(t *testing.T) {
	scope := dscope.New(
		dscope.Provide(testInt(1)),
	)

	env := &taigo.Env{
		Globals: make(map[string]any),
		Source: `
		package main
		var x testInt = 42
		`,
	}
	for t := range scope.AllTypes() {
		if t.Implements(configurableType) {
			env.Globals[t.Name()] = t
		}
	}
	vm, err := env.RunVM()
	if err != nil {
		panic(err)
	}

	scope, err = TaigoFork(scope, vm.Scope)
	if err != nil {
		t.Fatal(err)
	}

	i := dscope.Get[testInt](scope)
	if i != 42 {
		t.Fatalf("got %v", i)
	}

}
