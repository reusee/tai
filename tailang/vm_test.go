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

func TestVM_StackResize(t *testing.T) {
	main := &Function{
		Code:      []OpCode{OpLoadConst, 0, 0},
		Constants: []any{42},
	}
	vm := NewVM(main)
	vm.State.OperandStack = make([]any, 0)
	vm.State.SP = 0
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(vm.State.OperandStack) < 1 {
		t.Fatal("stack should have grown")
	}
	if vm.State.OperandStack[0].(int) != 42 {
		t.Fatal("wrong value on stack")
	}
}

func TestVM_JumpFalse_Variations(t *testing.T) {
	for _, val := range []any{nil, false, 0, ""} {
		t.Run(fmt.Sprintf("%v", val), func(t *testing.T) {
			main := &Function{
				Constants: []any{val},
				Code: []OpCode{
					OpLoadConst, 0, 0,
					OpJumpFalse, 0, 2,
					OpSuspend,
				},
			}
			var count int
			for range NewVM(main).Run {
				count++
			}
			if count != 0 {
				t.Fatal("expected jump")
			}
		})
	}
}

func TestVM_ContinueOnError(t *testing.T) {
	t.Run("LoadVar", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{"x"},
			Code:      []OpCode{OpLoadVar, 0, 0},
		})
		var n int
		vm.Run(func(_ *Interrupt, err error) bool {
			if err != nil {
				n++
				return true
			}
			return false
		})
		if n != 1 {
			t.Fatal("expected error")
		}
		if vm.State.OperandStack[0] != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("SetVar", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{"x", 1},
			Code:      []OpCode{OpLoadConst, 0, 1, OpSetVar, 0, 0},
		})
		var n int
		vm.Run(func(_ *Interrupt, err error) bool {
			if err != nil {
				n++
				return true
			}
			return false
		})
		if n != 1 {
			t.Fatal("expected error")
		}
	})

	t.Run("ArityMismatch", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{&Function{NumParams: 1}},
			Code:      []OpCode{OpMakeClosure, 0, 0, OpCall, 0, 0},
		})
		var n int
		vm.Run(func(_ *Interrupt, err error) bool {
			if err != nil {
				n++
				return true
			}
			return false
		})
		if n != 1 {
			t.Fatal("expected error")
		}
	})
}

func TestVM_ListMap(t *testing.T) {
	t.Run("List", func(t *testing.T) {
		main := &Function{
			Constants: []any{"l", 1, 2, "res"},
			Code: []OpCode{
				// list = [1, 2]
				OpLoadConst, 0, 1,
				OpLoadConst, 0, 2,
				OpMakeList, 0, 2,
				OpDefVar, 0, 0,

				// res = list[1]
				OpLoadVar, 0, 0,
				OpLoadConst, 0, 1, // index 1 (value 2) (Wait, const 1 is value 1. Need index. Let's use value 1 as index 1)
				OpGetIndex,
				OpDefVar, 0, 3,
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res, ok := vm.State.Scope.Get("res")
		if !ok || res.(int) != 2 {
			t.Fatalf("expected 2, got %v", res)
		}

		// Update
		main = &Function{
			Constants: []any{"l", 1, 42, "res"},
			Code: []OpCode{
				// list = [1, 2] (reusing setup logic or just trust state if simple)
				// let's do fresh
				OpLoadConst, 0, 1,
				OpLoadConst, 0, 1, // list=[1, 1]
				OpMakeList, 0, 2,
				OpDefVar, 0, 0,

				OpLoadVar, 0, 0,
				OpLoadConst, 0, 1, // index 1
				OpLoadConst, 0, 2, // value 42
				OpSetIndex,

				OpLoadVar, 0, 0,
				OpLoadConst, 0, 1, // index 1
				OpGetIndex,
				OpDefVar, 0, 3,
			},
		}
		vm = NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res, ok = vm.State.Scope.Get("res")
		if !ok || res.(int) != 42 {
			t.Fatalf("expected 42, got %v", res)
		}
	})

	t.Run("Map", func(t *testing.T) {
		main := &Function{
			Constants: []any{"m", "foo", 42, "bar", 0, "res"},
			Code: []OpCode{
				// m = {foo: 42, bar: 0}
				OpLoadConst, 0, 1, // foo
				OpLoadConst, 0, 2, // 42
				OpLoadConst, 0, 3, // bar
				OpLoadConst, 0, 4, // 0
				OpMakeMap, 0, 2, // 2 pairs
				OpDefVar, 0, 0,

				// res = m.foo
				OpLoadVar, 0, 0,
				OpLoadConst, 0, 1, // foo
				OpGetIndex,
				OpDefVar, 0, 5,
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res, ok := vm.State.Scope.Get("res")
		if !ok || res.(int) != 42 {
			t.Fatalf("expected 42, got %v", res)
		}
	})
}

func TestVM_Swap(t *testing.T) {
	main := &Function{
		Constants: []any{1, 2, "res"},
		Code: []OpCode{
			OpLoadConst, 0, 0, // 1
			OpLoadConst, 0, 1, // 2
			OpSwap,         // Stack: [2, 1]
			OpPop,          // Pop 1. Stack: [2]
			OpDefVar, 0, 2, // res = 2
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

func TestVM_Pipe(t *testing.T) {
	// 42 | sub(1) => sub(42, 1) = 41
	sub := NativeFunc(func(vm *VM, args []any) (any, error) {
		return args[0].(int) - args[1].(int), nil
	})

	main := &Function{
		Constants: []any{42, "sub", 1, "res"},
		Code: []OpCode{
			OpLoadConst, 0, 0, // 42
			// Pipe to sub(1)
			OpLoadVar, 0, 1, // sub. Stack: [42, sub]
			OpSwap,            // Stack: [sub, 42]
			OpLoadConst, 0, 2, // 1. Stack: [sub, 42, 1]
			OpCall, 0, 2, // sub(42, 1) -> 41
			OpDefVar, 0, 3, // res = 41
		},
	}

	vm := NewVM(main)
	vm.State.Scope.Def("sub", sub)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	res, ok := vm.State.Scope.Get("res")
	if !ok {
		t.Fatal("res not found")
	}
	if res.(int) != 41 {
		t.Fatalf("expected 41, got %v", res)
	}
}

func TestVM_Pipe_Error(t *testing.T) {
	main := &Function{
		Code: []OpCode{
			OpLoadConst, 0, 0, // Just 1 item
			OpSwap,
		},
		Constants: []any{1},
	}
	vm := NewVM(main)
	var hasErr bool
	for _, err := range vm.Run {
		if err != nil {
			hasErr = true
			if err.Error() != "stack underflow during swap" {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}
	if !hasErr {
		t.Fatal("expected error")
	}
}

func TestVM_Env_GetSet_ChildGrowth(t *testing.T) {
	root := &Env{}
	root.Def("x", 100)

	child := root.NewChild()
	// Def "z" to ensure child vars slice grows and includes slot for "x"
	// assuming z's symbol index > x's symbol index
	child.Def("z", 200)

	// Get x from child (slot exists but is undefined, should fallback to parent)
	val, ok := child.Get("x")
	if !ok || val.(int) != 100 {
		t.Errorf("Get fallback failed: got %v", val)
	}

	// Set x via child (slot exists but is undefined, should update parent)
	if !child.Set("x", 101) {
		t.Error("Set returned false")
	}
	val, ok = root.Get("x")
	if val.(int) != 101 {
		t.Errorf("Root x not updated: %v", val)
	}
}

func TestVM_TCO(t *testing.T) {
	// Native function to inspect the call stack
	check := NativeFunc(func(vm *VM, args []any) (any, error) {
		// Expect call stack: Main -> C.
		// If TCO works, B's frame should have been replaced by C's frame.
		// Since C is the current function, it is not in vm.State.CallStack (which holds callers).
		// So CallStack should contain only Main.
		if len(vm.State.CallStack) != 1 {
			return nil, fmt.Errorf("expected call stack depth 1, got %d", len(vm.State.CallStack))
		}
		if vm.State.CallStack[0].Fun.Name != "main" {
			return nil, fmt.Errorf("expected caller to be main")
		}
		return 42, nil
	})

	cFunc := &Function{
		Name:      "C",
		Constants: []any{"check"},
		Code: []OpCode{
			OpLoadVar, 0, 0,
			OpCall, 0, 0,
			OpReturn,
		},
	}

	bFunc := &Function{
		Name:      "B",
		Constants: []any{cFunc},
		Code: []OpCode{
			OpMakeClosure, 0, 0,
			OpCall, 0, 0, // Tail call to C
			OpReturn,
		},
	}

	main := &Function{
		Name:      "main",
		Constants: []any{bFunc, "check", "res"},
		Code: []OpCode{
			OpMakeClosure, 0, 0,
			OpCall, 0, 0,
			OpDefVar, 0, 2,
		},
	}

	vm := NewVM(main)
	vm.State.Scope.Def("check", check)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	res, ok := vm.State.Scope.Get("res")
	if !ok || res.(int) != 42 {
		t.Fatalf("expected 42, got %v", res)
	}
}

func TestVM_OpErrors_Break(t *testing.T) {
	// This test iterates various error conditions and breaks on the first error.
	// This ensures the 'return' path (aborting VM) in the error handling blocks is covered.
	cases := []struct {
		Name   string
		Code   []OpCode
		Consts []any
	}{
		{
			Name:   "LoadVarUndefined",
			Code:   []OpCode{OpLoadVar, 0, 0},
			Consts: []any{"undef"},
		},
		{
			Name:   "SetVarUndefined",
			Code:   []OpCode{OpLoadConst, 0, 0, OpSetVar, 0, 1},
			Consts: []any{1, "undef"},
		},
		{
			Name: "MakeListStackUnderflow",
			Code: []OpCode{OpMakeList, 0, 5},
		},
		{
			Name: "MakeMapStackUnderflow",
			Code: []OpCode{OpMakeMap, 0, 5},
		},
		{
			Name: "CallStackUnderflow",
			Code: []OpCode{OpCall, 0, 5},
		},
		{
			Name:   "CallNonFunction",
			Code:   []OpCode{OpLoadConst, 0, 0, OpCall, 0, 0},
			Consts: []any{1},
		},
		{
			Name:   "ArityMismatch",
			Consts: []any{&Function{NumParams: 1}},
			Code:   []OpCode{OpMakeClosure, 0, 0, OpCall, 0, 0},
		},
		{
			Name:   "IndexNil",
			Consts: []any{nil, 1},
			Code:   []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 1, OpGetIndex},
		},
		{
			Name:   "IndexSliceBadKey",
			Consts: []any{"bad"},
			Code:   []OpCode{OpMakeList, 0, 0, OpLoadConst, 0, 0, OpGetIndex},
		},
		{
			Name:   "IndexSliceOutOfBounds",
			Consts: []any{0},
			Code:   []OpCode{OpMakeList, 0, 0, OpLoadConst, 0, 0, OpGetIndex},
		},
		{
			Name:   "IndexUnindexable",
			Consts: []any{1},
			Code:   []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 0, OpGetIndex},
		},
		{
			Name:   "SetIndexNil",
			Consts: []any{nil},
			Code:   []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 0, OpLoadConst, 0, 0, OpSetIndex},
		},
		{
			Name:   "SetIndexSliceBadKey",
			Consts: []any{"bad", 1},
			Code:   []OpCode{OpMakeList, 0, 0, OpLoadConst, 0, 0, OpLoadConst, 0, 1, OpSetIndex},
		},
		{
			Name:   "SetIndexSliceOutOfBounds",
			Consts: []any{0, 1},
			Code:   []OpCode{OpMakeList, 0, 0, OpLoadConst, 0, 0, OpLoadConst, 0, 1, OpSetIndex},
		},
		{
			Name:   "SetIndexStringKey",
			Consts: []any{map[string]any{}, 1, 1},
			Code:   []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 1, OpLoadConst, 0, 2, OpSetIndex},
		},
		{
			Name:   "SetIndexUnassignable",
			Consts: []any{1},
			Code:   []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 0, OpLoadConst, 0, 0, OpSetIndex},
		},
		{
			Name:   "SwapUnderflow",
			Consts: []any{1},
			Code:   []OpCode{OpLoadConst, 0, 0, OpSwap},
		},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			vm := NewVM(&Function{
				Code:      c.Code,
				Constants: c.Consts,
			})
			var hit bool
			for _, err := range vm.Run {
				if err != nil {
					hit = true
					break
				}
			}
			if !hit {
				t.Error("expected error")
			}
		})
	}
}

func TestVM_ContinueOnError_More(t *testing.T) {
	// These tests ensure that execution continues (skips the op) when the error is handled
	// by the yield function returning true.
	run := func(vm *VM) {
		vm.Run(func(_ *Interrupt, err error) bool {
			// Always continue on error
			return err != nil
		})
	}

	t.Run("MakeMapUnderflow", func(t *testing.T) {
		vm := NewVM(&Function{Code: []OpCode{OpMakeMap, 0, 5}})
		run(vm) // should not panic
	})

	t.Run("IndexNil", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{nil, 1},
			Code:      []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 1, OpGetIndex},
		})
		run(vm)
	})

	t.Run("SetIndexNil", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{nil},
			Code:      []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 0, OpLoadConst, 0, 0, OpSetIndex},
		})
		run(vm)
	})

	t.Run("SetIndexBadKey", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{[]any{}, "bad", 1},
			Code:      []OpCode{OpLoadConst, 0, 0, OpLoadConst, 0, 1, OpLoadConst, 0, 2, OpSetIndex},
		})
		run(vm)
	})
}
