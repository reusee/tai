package tailang

import (
	"fmt"
	"testing"
)

func TestVM_NativeFunc(t *testing.T) {
	main := &Function{
		Name: "main",
		Constants: []any{
			"add",
			1,
			2,
			"res",
		},
		Code: []OpCode{
			OpLoadVar, 0, 0,
			OpLoadConst, 0, 1,
			OpLoadConst, 0, 2,
			OpCall, 0, 2,
			OpDefVar, 0, 3,
		},
	}

	vm := NewVM(main)
	vm.State.Scope.Def("add", NativeFunc(func(vm *VM, args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("bad args")
		}
		a := args[0].(int)
		b := args[1].(int)
		return a + b, nil
	}))

	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	res, ok := vm.State.Scope.Get("res")
	if !ok {
		t.Fatal("res not found")
	}
	if res.(int) != 3 {
		t.Fatalf("expected 3, got %v", res)
	}
}

func TestVM_Closure(t *testing.T) {
	inner := &Function{
		Name: "inner",
		Constants: []any{
			"x",
		},
		Code: []OpCode{
			OpLoadVar, 0, 0,
			OpReturn,
		},
	}

	outer := &Function{
		Name: "outer",
		Constants: []any{
			"x",
			42,
			inner,
		},
		Code: []OpCode{
			OpLoadConst, 0, 1,
			OpDefVar, 0, 0,
			OpMakeClosure, 0, 2,
			OpReturn,
		},
	}

	main := &Function{
		Name: "main",
		Constants: []any{
			outer,
			"f",
			"res",
		},
		Code: []OpCode{
			OpMakeClosure, 0, 0,
			OpCall, 0, 0,
			OpDefVar, 0, 1,
			OpLoadVar, 0, 1,
			OpCall, 0, 0,
			OpDefVar, 0, 2,
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	res, ok := vm.State.Scope.Get("res")
	if !ok {
		t.Fatal("res not found")
	}
	if res.(int) != 42 {
		t.Fatalf("expected 42, got %v", res)
	}
}

func TestVM_Jump(t *testing.T) {
	main := &Function{
		Name: "main",
		Constants: []any{
			"res",
			0, // falsey
			1, // truthy (also used as value)
			2,
		},
		Code: []OpCode{
			// res = 0
			OpLoadConst, 0, 1,
			OpDefVar, 0, 0,

			// if false jump +6
			OpLoadConst, 0, 1,
			OpJumpFalse, 0, 6,
			// block 1
			OpLoadConst, 0, 2,
			OpDefVar, 0, 0,

			// if true jump +6
			OpLoadConst, 0, 2,
			OpJumpFalse, 0, 6,
			// block 2
			OpLoadConst, 0, 3,
			OpDefVar, 0, 0,
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	res, ok := vm.State.Scope.Get("res")
	if !ok {
		t.Fatal("res not found")
	}
	if res.(int) != 2 {
		t.Fatalf("expected 2, got %v", res)
	}
}
