package configs

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/taigo"
)

type testInt int

var _ Configurable = testInt(0)

func (t testInt) ConfigExpr() string {
	return "testInt"
}

func TestTaigoDefs(t *testing.T) {
	scope := dscope.New(
		dscope.Provide(testInt(1)),
	)

	env := &taigo.Env{
		Source: `
		package main
		var testInt = 42
		`,
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
