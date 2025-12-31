package taigo

import "testing"

func TestGetFunc(t *testing.T) {
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

	makeAdder, err := Get[func(int) func(int) int](vm, "make_adder")
	if err != nil {
		t.Fatal(err)
	}
	adder := makeAdder(1)
	if adder(2) != 3 {
		t.Fatal()
	}
}
