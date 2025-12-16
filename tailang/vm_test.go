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

func TestVM_Scope(t *testing.T) {
	main := &Function{
		Name: "main",
		Constants: []any{
			"x",
			1,
			2,
		},
		Code: []OpCode{
			OpLoadConst, 0, 1,
			OpDefVar, 0, 0,
			OpEnterScope,
			OpLoadConst, 0, 2,
			OpDefVar, 0, 0,
			OpLeaveScope,
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	val, ok := vm.State.Scope.Get("x")
	if !ok {
		t.Fatal("x not found")
	}
	if val.(int) != 1 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestVM_SetVar(t *testing.T) {
	main := &Function{
		Name: "main",
		Constants: []any{
			"x",
			1,
			2,
		},
		Code: []OpCode{
			OpLoadConst, 0, 1,
			OpDefVar, 0, 0,
			OpLoadConst, 0, 2,
			OpSetVar, 0, 0,
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	val, ok := vm.State.Scope.Get("x")
	if !ok {
		t.Fatal("x not found")
	}
	if val.(int) != 2 {
		t.Fatalf("expected 2, got %v", val)
	}
}

func TestVM_Pop(t *testing.T) {
	main := &Function{
		Name: "main",
		Constants: []any{
			"x",
			1,
			2,
		},
		Code: []OpCode{
			OpLoadConst, 0, 1,
			OpLoadConst, 0, 2,
			OpPop,
			OpDefVar, 0, 0,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	val, ok := vm.State.Scope.Get("x")
	if !ok || val.(int) != 1 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestVM_UnconditionalJump(t *testing.T) {
	main := &Function{
		Name: "main",
		Constants: []any{
			"res",
			1,
			2,
		},
		Code: []OpCode{
			OpLoadConst, 0, 1,
			OpJump, 0, 4,
			OpLoadConst, 0, 2,
			OpPop,
			OpDefVar, 0, 0,
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	val, ok := vm.State.Scope.Get("res")
	if !ok {
		t.Fatal("res not found")
	}
	if val.(int) != 1 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestVM_Suspend(t *testing.T) {
	main := &Function{
		Name: "main",
		Code: []OpCode{
			OpSuspend,
		},
	}

	vm := NewVM(main)
	var suspended bool
	for i, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
		if i == InterruptSuspend {
			suspended = true
		}
	}

	if !suspended {
		t.Fatal("expected suspend")
	}
}

func TestVM_Errors(t *testing.T) {
	t.Run("UndefinedVar", func(t *testing.T) {
		main := &Function{
			Name: "main",
			Constants: []any{
				"x",
			},
			Code: []OpCode{
				OpLoadVar, 0, 0,
			},
		}
		vm := NewVM(main)
		var err error
		for _, e := range vm.Run {
			if e != nil {
				err = e
			}
		}
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("SetUndefinedVar", func(t *testing.T) {
		main := &Function{
			Name: "main",
			Constants: []any{
				"x",
				1,
			},
			Code: []OpCode{
				OpLoadConst, 0, 1,
				OpSetVar, 0, 0,
			},
		}
		vm := NewVM(main)
		var err error
		for _, e := range vm.Run {
			if e != nil {
				err = e
			}
		}
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("ArityMismatch", func(t *testing.T) {
		foo := &Function{
			Name:      "foo",
			NumParams: 1,
			Code: []OpCode{
				OpReturn,
			},
		}
		main := &Function{
			Name: "main",
			Constants: []any{
				foo,
			},
			Code: []OpCode{
				OpMakeClosure, 0, 0,
				OpCall, 0, 0,
			},
		}
		vm := NewVM(main)
		var err error
		for _, e := range vm.Run {
			if e != nil {
				err = e
			}
		}
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestVM_ParentScopeAccess(t *testing.T) {
	main := &Function{
		Constants: []any{
			"x", 1, 2,
		},
		Code: []OpCode{
			OpLoadConst, 0, 1,
			OpDefVar, 0, 0,
			OpEnterScope,
			OpLoadVar, 0, 0,
			OpPop,
			OpLoadConst, 0, 2,
			OpSetVar, 0, 0,
			OpLeaveScope,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	val, ok := vm.State.Scope.Get("x")
	if !ok {
		t.Fatal("x not found")
	}
	if val.(int) != 2 {
		t.Fatalf("expected 2, got %v", val)
	}
}

func TestVM_StackGrowth(t *testing.T) {
	code := make([]OpCode, 0, 3100)
	for range 1050 {
		code = append(code, OpLoadConst, 0, 0)
	}
	main := &Function{
		Constants: []any{1},
		Code:      code,
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(vm.State.OperandStack) <= 1024 {
		t.Fatal("stack did not grow")
	}
}

func TestVM_ErrorControlFlow(t *testing.T) {
	t.Run("IgnoreNativeError", func(t *testing.T) {
		main := &Function{
			Constants: []any{"f"},
			Code: []OpCode{
				OpLoadVar, 0, 0,
				OpCall, 0, 0,
				OpPop,
			},
		}
		vm := NewVM(main)
		vm.State.Scope.Def("f", NativeFunc(func(*VM, []any) (any, error) {
			return nil, fmt.Errorf("foo")
		}))

		errCount := 0
		for _, err := range vm.Run {
			if err != nil {
				errCount++
			}
		}
		if errCount != 1 {
			t.Fatalf("expected 1 error, got %d", errCount)
		}
	})

	t.Run("StopOnNativeError", func(t *testing.T) {
		main := &Function{
			Constants: []any{"f"},
			Code: []OpCode{
				OpLoadVar, 0, 0,
				OpCall, 0, 0,
				OpLoadVar, 0, 0,
			},
		}
		vm := NewVM(main)
		callCount := 0
		vm.State.Scope.Def("f", NativeFunc(func(*VM, []any) (any, error) {
			callCount++
			return nil, fmt.Errorf("foo")
		}))

		for _, err := range vm.Run {
			if err != nil {
				break
			}
		}
		if callCount != 1 {
			t.Fatal("func called wrong number of times")
		}
	})

	t.Run("StopOnSuspend", func(t *testing.T) {
		main := &Function{Code: []OpCode{OpSuspend, OpSuspend}}
		vm := NewVM(main)
		count := 0
		for range vm.Run {
			count++
			break
		}
		if count != 1 {
			t.Fatalf("expected 1, got %d", count)
		}
	})

	t.Run("StopOnSetVarError", func(t *testing.T) {
		main := &Function{
			Constants: []any{"x", 1},
			Code: []OpCode{
				OpLoadConst, 0, 1,
				OpSetVar, 0, 0,
			},
		}
		vm := NewVM(main)
		var err error
		for _, e := range vm.Run {
			if e != nil {
				err = e
				break
			}
		}
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("StopOnLoadVarError", func(t *testing.T) {
		main := &Function{
			Constants: []any{"x"},
			Code: []OpCode{
				OpLoadVar, 0, 0,
				OpLoadVar, 0, 0,
			},
		}
		vm := NewVM(main)
		count := 0
		for _, err := range vm.Run {
			if err != nil {
				count++
				break
			}
		}
		if count != 1 {
			t.Fatal("expected 1 error")
		}
	})
}

func TestVM_CallStackUnderflow(t *testing.T) {
	main := &Function{
		Code: []OpCode{
			OpCall, 0, 5,
		},
	}
	vm := NewVM(main)

	handled := false
	for _, err := range vm.Run {
		if err != nil {
			handled = true
			if err.Error() != "stack underflow during call" {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}
	if !handled {
		t.Fatal("expected error")
	}
}

func TestVM_TopLevelScopeLeave(t *testing.T) {
	main := &Function{
		Code: []OpCode{
			OpLeaveScope,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if vm.State.Scope.Parent != nil {
		t.Fatal("scope parent should be nil")
	}
}

func TestVM_TopLevelReturn(t *testing.T) {
	main := &Function{
		Constants: []any{42},
		Code: []OpCode{
			OpLoadConst, 0, 0,
			OpReturn,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestVM_TruncatedRead(t *testing.T) {
	main := &Function{
		Constants: []any{"safe"},
		Code: []OpCode{
			OpLoadConst,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestVM_PopEmpty(t *testing.T) {
	main := &Function{
		Code: []OpCode{OpPop},
	}
	for range NewVM(main).Run {
	}
}
