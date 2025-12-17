package tailang

import (
	"bytes"
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
			OpLoadVar.With(0),
			OpLoadConst.With(1),
			OpLoadConst.With(2),
			OpCall.With(2),
			OpDefVar.With(3),
		},
	}

	vm := NewVM(main)
	vm.Def("add", NativeFunc{
		Name: "add",
		Func: func(vm *VM, args []any) (any, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("bad args")
			}
			a := args[0].(int)
			b := args[1].(int)
			return a + b, nil
		},
	})

	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	res, ok := vm.Get("res")
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
			OpLoadVar.With(0),
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
			OpLoadConst.With(1),
			OpDefVar.With(0),
			OpMakeClosure.With(2),
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
			OpMakeClosure.With(0),
			OpCall.With(0),
			OpDefVar.With(1),
			OpLoadVar.With(1),
			OpCall.With(0),
			OpDefVar.With(2),
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	res, ok := vm.Get("res")
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
			OpLoadConst.With(1),
			OpDefVar.With(0),

			// if false jump +2 (skip next 2 instructions)
			OpLoadConst.With(1),
			OpJumpFalse.With(2),
			// block 1
			OpLoadConst.With(2),
			OpDefVar.With(0),

			// if true jump +2
			OpLoadConst.With(2),
			OpJumpFalse.With(2),
			// block 2
			OpLoadConst.With(3),
			OpDefVar.With(0),
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	res, ok := vm.Get("res")
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
			OpLoadConst.With(1),
			OpDefVar.With(0),
			OpEnterScope,
			OpLoadConst.With(2),
			OpDefVar.With(0),
			OpLeaveScope,
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	val, ok := vm.Get("x")
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
			OpLoadConst.With(1),
			OpDefVar.With(0),
			OpLoadConst.With(2),
			OpSetVar.With(0),
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	val, ok := vm.Get("x")
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
			OpLoadConst.With(1),
			OpLoadConst.With(2),
			OpPop,
			OpDefVar.With(0),
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	val, ok := vm.Get("x")
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
			OpLoadConst.With(1),
			OpJump.With(2),
			OpLoadConst.With(2),
			OpPop,
			OpDefVar.With(0),
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	val, ok := vm.Get("res")
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
				OpLoadVar.With(0),
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
				OpLoadConst.With(1),
				OpSetVar.With(0),
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
				OpMakeClosure.With(0),
				OpCall.With(0),
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
			OpLoadConst.With(1),
			OpDefVar.With(0),
			OpEnterScope,
			OpLoadVar.With(0),
			OpPop,
			OpLoadConst.With(2),
			OpSetVar.With(0),
			OpLeaveScope,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	val, ok := vm.Get("x")
	if !ok {
		t.Fatal("x not found")
	}
	if val.(int) != 2 {
		t.Fatalf("expected 2, got %v", val)
	}
}

func TestVM_StackGrowth(t *testing.T) {
	code := make([]OpCode, 0, 1050)
	for range 1050 {
		code = append(code, OpLoadConst.With(0))
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
	if len(vm.OperandStack) <= 1024 {
		t.Fatal("stack did not grow")
	}
}

func TestVM_ErrorControlFlow(t *testing.T) {
	t.Run("IgnoreNativeError", func(t *testing.T) {
		main := &Function{
			Constants: []any{"f"},
			Code: []OpCode{
				OpLoadVar.With(0),
				OpCall.With(0),
				OpPop,
			},
		}
		vm := NewVM(main)
		vm.Def("f", NativeFunc{
			Name: "f",
			Func: func(*VM, []any) (any, error) {
				return nil, fmt.Errorf("foo")
			},
		})

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
				OpLoadVar.With(0),
				OpCall.With(0),
				OpLoadVar.With(0),
			},
		}
		vm := NewVM(main)
		callCount := 0
		vm.Def("f", NativeFunc{
			Name: "f",
			Func: func(*VM, []any) (any, error) {
				callCount++
				return nil, fmt.Errorf("foo")
			},
		})

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
				OpLoadConst.With(1),
				OpSetVar.With(0),
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
				OpLoadVar.With(0),
				OpLoadVar.With(0),
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
			OpCall.With(5),
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
	if vm.Scope.Parent != nil {
		t.Fatal("scope parent should be nil")
	}
}

func TestVM_TopLevelReturn(t *testing.T) {
	main := &Function{
		Constants: []any{42},
		Code: []OpCode{
			OpLoadConst.With(0),
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

func TestVM_PopEmpty(t *testing.T) {
	main := &Function{
		Code: []OpCode{OpPop},
	}
	for range NewVM(main).Run {
	}
}

func TestVM_StackResize(t *testing.T) {
	main := &Function{
		Code:      []OpCode{OpLoadConst.With(0)},
		Constants: []any{42},
	}
	vm := NewVM(main)
	vm.OperandStack = make([]any, 0)
	vm.SP = 0
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(vm.OperandStack) < 1 {
		t.Fatal("stack should have grown")
	}
	if vm.OperandStack[0].(int) != 42 {
		t.Fatal("wrong value on stack")
	}
}

func TestVM_JumpFalse_Variations(t *testing.T) {
	for _, val := range []any{nil, false, 0, ""} {
		t.Run(fmt.Sprintf("%v", val), func(t *testing.T) {
			main := &Function{
				Constants: []any{val},
				Code: []OpCode{
					OpLoadConst.With(0),
					OpJumpFalse.With(1),
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
			Code:      []OpCode{OpLoadVar.With(0)},
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
		if vm.OperandStack[0] != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("SetVar", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{"x", 1},
			Code:      []OpCode{OpLoadConst.With(1), OpSetVar.With(0)},
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
			Code:      []OpCode{OpMakeClosure.With(0), OpCall.With(0)},
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
				OpLoadConst.With(1),
				OpLoadConst.With(2),
				OpMakeList.With(2),
				OpDefVar.With(0),

				// res = list[1]
				OpLoadVar.With(0),
				OpLoadConst.With(1), // index 1 (value 2)
				OpGetIndex,
				OpDefVar.With(3),
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res, ok := vm.Get("res")
		if !ok || res.(int) != 2 {
			t.Fatalf("expected 2, got %v", res)
		}

		// Update
		main = &Function{
			Constants: []any{"l", 1, 42, "res"},
			Code: []OpCode{
				// list = [1, 2]
				OpLoadConst.With(1),
				OpLoadConst.With(1), // list=[1, 1]
				OpMakeList.With(2),
				OpDefVar.With(0),

				OpLoadVar.With(0),
				OpLoadConst.With(1), // index 1
				OpLoadConst.With(2), // value 42
				OpSetIndex,

				OpLoadVar.With(0),
				OpLoadConst.With(1), // index 1
				OpGetIndex,
				OpDefVar.With(3),
			},
		}
		vm = NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res, ok = vm.Get("res")
		if !ok || res.(int) != 42 {
			t.Fatalf("expected 42, got %v", res)
		}
	})

	t.Run("Map", func(t *testing.T) {
		main := &Function{
			Constants: []any{"m", "foo", 42, "bar", 0, "res"},
			Code: []OpCode{
				// m = {foo: 42, bar: 0}
				OpLoadConst.With(1), // foo
				OpLoadConst.With(2), // 42
				OpLoadConst.With(3), // bar
				OpLoadConst.With(4), // 0
				OpMakeMap.With(2),   // 2 pairs
				OpDefVar.With(0),

				// res = m.foo
				OpLoadVar.With(0),
				OpLoadConst.With(1), // foo
				OpGetIndex,
				OpDefVar.With(5),
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res, ok := vm.Get("res")
		if !ok || res.(int) != 42 {
			t.Fatalf("expected 42, got %v", res)
		}
	})
}

func TestVM_Swap(t *testing.T) {
	main := &Function{
		Constants: []any{1, 2, "res"},
		Code: []OpCode{
			OpLoadConst.With(0), // 1
			OpLoadConst.With(1), // 2
			OpSwap,              // Stack: [2, 1]
			OpPop,               // Pop 1. Stack: [2]
			OpDefVar.With(2),    // res = 2
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	res, ok := vm.Get("res")
	if !ok {
		t.Fatal("res not found")
	}
	if res.(int) != 2 {
		t.Fatalf("expected 2, got %v", res)
	}
}

func TestVM_Pipe(t *testing.T) {
	// 42 | sub(1) => sub(42, 1) = 41
	sub := NativeFunc{
		Name: "sub",
		Func: func(vm *VM, args []any) (any, error) {
			return args[0].(int) - args[1].(int), nil
		},
	}

	main := &Function{
		Constants: []any{42, "sub", 1, "res"},
		Code: []OpCode{
			OpLoadConst.With(0), // 42
			// Pipe to sub(1)
			OpLoadVar.With(1),   // sub. Stack: [42, sub]
			OpSwap,              // Stack: [sub, 42]
			OpLoadConst.With(2), // 1. Stack: [sub, 42, 1]
			OpCall.With(2),      // sub(42, 1) -> 41
			OpDefVar.With(3),    // res = 41
		},
	}

	vm := NewVM(main)
	vm.Def("sub", sub)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	res, ok := vm.Get("res")
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
			OpLoadConst.With(0), // Just 1 item
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
	root.DefSym(Symbol(1), 100)

	child := root.NewChild()
	// Def "z" to ensure child vars slice grows and includes slot for "x"
	// assuming z's symbol index > x's symbol index
	child.DefSym(Symbol(2), 200)

	// Get x from child (slot exists but is undefined, should fallback to parent)
	val, ok := child.GetSym(Symbol(1))
	if !ok || val.(int) != 100 {
		t.Errorf("Get fallback failed: got %v", val)
	}

	// Set x via child (slot exists but is undefined, should update parent)
	if !child.SetSym(Symbol(1), 101) {
		t.Error("Set returned false")
	}
	val, ok = root.GetSym(Symbol(1))
	if val.(int) != 101 {
		t.Errorf("Root x not updated: %v", val)
	}
}

func TestVM_TCO(t *testing.T) {
	// Native function to inspect the call stack
	check := NativeFunc{
		Name: "check",
		Func: func(vm *VM, args []any) (any, error) {
			// Expect call stack: Main -> C.
			// If TCO works, B's frame should have been replaced by C's frame.
			// Since C is the current function, it is not in vm.CallStack (which holds callers).
			// So CallStack should contain only Main.
			if len(vm.CallStack) != 1 {
				return nil, fmt.Errorf("expected call stack depth 1, got %d", len(vm.CallStack))
			}
			if vm.CallStack[0].Fun.Name != "main" {
				return nil, fmt.Errorf("expected caller to be main")
			}
			return 42, nil
		},
	}

	cFunc := &Function{
		Name:      "C",
		Constants: []any{"check"},
		Code: []OpCode{
			OpLoadVar.With(0),
			OpCall.With(0),
			OpReturn,
		},
	}

	bFunc := &Function{
		Name:      "B",
		Constants: []any{cFunc},
		Code: []OpCode{
			OpMakeClosure.With(0),
			OpCall.With(0), // Tail call to C
			OpReturn,
		},
	}

	main := &Function{
		Name:      "main",
		Constants: []any{bFunc, "check", "res"},
		Code: []OpCode{
			OpMakeClosure.With(0),
			OpCall.With(0),
			OpDefVar.With(2),
		},
	}

	vm := NewVM(main)
	vm.Def("check", check)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	res, ok := vm.Get("res")
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
			Code:   []OpCode{OpLoadVar.With(0)},
			Consts: []any{"undef"},
		},
		{
			Name:   "SetVarUndefined",
			Code:   []OpCode{OpLoadConst.With(0), OpSetVar.With(1)},
			Consts: []any{1, "undef"},
		},
		{
			Name: "MakeListStackUnderflow",
			Code: []OpCode{OpMakeList.With(5)},
		},
		{
			Name: "MakeMapStackUnderflow",
			Code: []OpCode{OpMakeMap.With(5)},
		},
		{
			Name: "CallStackUnderflow",
			Code: []OpCode{OpCall.With(5)},
		},
		{
			Name:   "CallNonFunction",
			Code:   []OpCode{OpLoadConst.With(0), OpCall.With(0)},
			Consts: []any{1},
		},
		{
			Name:   "ArityMismatch",
			Consts: []any{&Function{NumParams: 1}},
			Code:   []OpCode{OpMakeClosure.With(0), OpCall.With(0)},
		},
		{
			Name:   "IndexNil",
			Consts: []any{nil, 1},
			Code:   []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetIndex},
		},
		{
			Name:   "IndexSliceBadKey",
			Consts: []any{"bad"},
			Code:   []OpCode{OpMakeList.With(0), OpLoadConst.With(0), OpGetIndex},
		},
		{
			Name:   "IndexSliceOutOfBounds",
			Consts: []any{0},
			Code:   []OpCode{OpMakeList.With(0), OpLoadConst.With(0), OpGetIndex},
		},
		{
			Name:   "IndexUnindexable",
			Consts: []any{1},
			Code:   []OpCode{OpLoadConst.With(0), OpLoadConst.With(0), OpGetIndex},
		},
		{
			Name:   "SetIndexNil",
			Consts: []any{nil},
			Code:   []OpCode{OpLoadConst.With(0), OpLoadConst.With(0), OpLoadConst.With(0), OpSetIndex},
		},
		{
			Name:   "SetIndexSliceBadKey",
			Consts: []any{"bad", 1},
			Code:   []OpCode{OpMakeList.With(0), OpLoadConst.With(0), OpLoadConst.With(1), OpSetIndex},
		},
		{
			Name:   "SetIndexSliceOutOfBounds",
			Consts: []any{0, 1},
			Code:   []OpCode{OpMakeList.With(0), OpLoadConst.With(0), OpLoadConst.With(1), OpSetIndex},
		},
		{
			Name:   "SetIndexStringKey",
			Consts: []any{map[string]any{}, 1, 1},
			Code:   []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpSetIndex},
		},
		{
			Name:   "SetIndexUnassignable",
			Consts: []any{1},
			Code:   []OpCode{OpLoadConst.With(0), OpLoadConst.With(0), OpLoadConst.With(0), OpSetIndex},
		},
		{
			Name:   "SwapUnderflow",
			Consts: []any{1},
			Code:   []OpCode{OpLoadConst.With(0), OpSwap},
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
		vm := NewVM(&Function{Code: []OpCode{OpMakeMap.With(5)}})
		run(vm) // should not panic
	})

	t.Run("IndexNil", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{nil, 1},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetIndex},
		})
		run(vm)
	})

	t.Run("SetIndexNil", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{nil},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(0), OpLoadConst.With(0), OpSetIndex},
		})
		run(vm)
	})

	t.Run("SetIndexBadKey", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{[]any{}, "bad", 1},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpSetIndex},
		})
		run(vm)
	})
}

func TestVM_Snapshot(t *testing.T) {
	main := &Function{
		Name: "main",
		Constants: []any{
			"a", 1, "b", 2,
		},
		Code: []OpCode{
			// a = 1
			OpLoadConst.With(1),
			OpDefVar.With(0),

			OpSuspend,

			// b = 2
			OpLoadConst.With(3),
			OpDefVar.With(2),

			OpReturn,
		},
	}

	vm1 := NewVM(main)
	suspended := false
	for i, err := range vm1.Run {
		if err != nil {
			t.Fatal(err)
		}
		if i == InterruptSuspend {
			suspended = true
			break
		}
	}
	if !suspended {
		t.Fatal("expected suspend")
	}

	val, ok := vm1.Get("a")
	if !ok || val.(int) != 1 {
		t.Fatalf("expected a=1, got %v", val)
	}

	var buf bytes.Buffer
	if err := vm1.Snapshot(&buf); err != nil {
		t.Fatal(err)
	}

	vm2 := NewVM(nil)
	if err := vm2.Restore(&buf); err != nil {
		t.Fatal(err)
	}

	val, ok = vm2.Get("a")
	if !ok || val.(int) != 1 {
		t.Fatalf("restored: expected a=1, got %v", val)
	}
	if _, ok := vm2.Get("b"); ok {
		t.Fatal("restored: b should not be defined")
	}

	for _, err := range vm2.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	val, ok = vm2.Get("b")
	if !ok || val.(int) != 2 {
		t.Fatalf("finished: expected b=2, got %v", val)
	}
}
