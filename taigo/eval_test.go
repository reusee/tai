package taigo

import (
	"reflect"
	"testing"
)

func TestEvalFunc(t *testing.T) {
	env := &Env{
		Source: `
		package main
		func make_adder(n int) func(int) int {
			return func(x int) int {
				return n + x
			}
		}
		`,
	}
	vm, err := env.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	makeAdder, err := Eval[func(int) func(int) int](vm.Scope, "make_adder")
	if err != nil {
		t.Fatal(err)
	}
	adder := makeAdder(1)
	if adder(2) != 3 {
		t.Fatal()
	}
}

func TestTypedEvalDynamicType(t *testing.T) {
	type MyData struct {
		ID   int
		Name string
	}
	rt := reflect.TypeFor[MyData]()

	env := &Env{
		Globals: map[string]any{
			"MyType": rt,
		},
		Source: `
		package main
		func create() MyType {
			var d MyType
			d.ID = 100
			d.Name = "tai"
			return d
		}
		`,
	}

	vm, err := env.RunVM()
	if err != nil {
		t.Fatal(err)
	}

	// Use TypedEval to get the value back as MyData struct
	val, err := TypedEval(vm.Scope, "create()", rt)
	if err != nil {
		t.Fatal(err)
	}

	data, ok := val.(MyData)
	if !ok {
		t.Fatalf("expected MyData, got %T", val)
	}
	if data.ID != 100 || data.Name != "tai" {
		t.Fatalf("data mismatch: %+v", data)
	}
}
