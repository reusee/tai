package taivm

import (
	"bytes"
	"encoding/gob"
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

			OpLoadConst.With(1),
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

func TestVM_FunctionReuse(t *testing.T) {
	// Function: global_var = 42
	sharedFunc := &Function{
		Constants: []any{
			42, "global_var",
		},
		Code: []OpCode{
			OpLoadConst.With(0),
			OpDefVar.With(1),
			OpReturn,
		},
	}

	// VM 1: Run the function.
	// This ensures that execution does not pollute sharedFunc with VM1's symbols.
	vm1 := NewVM(sharedFunc)
	for _, err := range vm1.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if v, ok := vm1.Get("global_var"); !ok || v.(int) != 42 {
		t.Fatal("vm1 failed to set global_var")
	}

	// VM 2: A fresh environment.
	// We occupy the first symbol slot with a different variable to strictly overlap
	// with where "global_var" would be if it were cached from VM1.
	vm2 := NewVM(sharedFunc)
	vm2.Def("other_var", 999)

	for _, err := range vm2.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	// Verification:
	// "other_var" (Symbol 0) should remain 999.
	// "global_var" (should be resolved to Symbol 1 in VM2) is set to 42.
	if val, ok := vm2.Get("other_var"); !ok || val.(int) != 999 {
		t.Fatalf("VM isolation failed: other_var corrupted. Expected 999, got %v", val)
	}

	if val, ok := vm2.Get("global_var"); !ok || val.(int) != 42 {
		t.Fatalf("VM isolation failed: global_var not set correctly in VM2. Got %v", val)
	}
}

func TestVM_TCO_Leak(t *testing.T) {
	// Use a native function to inspect the "garbage" area of the stack
	checkStack := NativeFunc{
		Name: "checkStack",
		Func: func(vm *VM, args []any) (any, error) {
			// vm.SP points to the next free slot.
			// In a clean VM, slots >= vm.SP should be nil.
			// In TCO, if we shrunk the stack, the old slots must be nilled.
			for i := vm.SP; i < len(vm.OperandStack); i++ {
				if vm.OperandStack[i] != nil {
					return nil, fmt.Errorf("stack leak at %d: %v", i, vm.OperandStack[i])
				}
			}
			return nil, nil
		},
	}

	// Callee: just call verify
	target := &Function{
		Name:      "target",
		Constants: []any{"checkStack"},
		Code: []OpCode{
			OpLoadVar.With(0),
			OpCall.With(0),
			OpReturn,
		},
	}

	// Caller: Push many values, then tail-call target.
	// target takes 0 args, so stack should shrink significantly.
	caller := &Function{
		Name:      "caller",
		Constants: []any{target, "garbage"},
		Code: []OpCode{
			// Fill stack with garbage
			OpLoadConst.With(1), // "garbage"
			OpLoadConst.With(1),
			OpLoadConst.With(1),
			OpLoadConst.With(1),
			OpLoadConst.With(1), // Stack has 5 items

			// Load target closure
			OpMakeClosure.With(0),

			// Tail call: stack moves from ~6 items down to ~1 item (target frame)
			// The garbage slots 1..5 should be cleared.
			OpCall.With(0),
			OpReturn,
		},
	}

	main := &Function{
		Constants: []any{caller, "checkStack"},
		Code: []OpCode{
			OpMakeClosure.With(0),
			OpCall.With(0),
		},
	}

	vm := NewVM(main)
	vm.Def("checkStack", checkStack)

	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestVM_ArityMismatch_Continue(t *testing.T) {
	// Function expecting 1 arg
	foo := &Function{
		NumParams: 1,
		Code:      []OpCode{OpReturn},
	}
	// Call it with 0 args
	main := &Function{
		Constants: []any{foo, "res"},
		Code: []OpCode{
			OpMakeClosure.With(0),
			OpCall.With(0), // Arity mismatch
			OpLoadConst.With(0),
			OpPop,
		},
	}
	vm := NewVM(main)
	var errReported bool
	vm.Run(func(_ *Interrupt, err error) bool {
		if err != nil {
			errReported = true
			return true // Continue execution
		}
		return false
	})
	if !errReported {
		t.Fatal("expected error to be reported")
	}
	// If continue worked, the VM reached the end of main's code
	if vm.IP != len(main.Code) {
		t.Fatalf("expected VM to finish, at IP %d/%d", vm.IP, len(main.Code))
	}
}

func TestSymbolTable_SnapshotRestore(t *testing.T) {
	st := NewSymbolTable()
	st.Intern("a")
	st.Intern("b")
	snap := st.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2, got %d", len(snap))
	}

	st2 := NewSymbolTable()
	st2.Restore(snap)
	if st2.Intern("a") != st.Intern("a") {
		t.Fatal("symbol mismatch")
	}
	if st2.Intern("b") != st.Intern("b") {
		t.Fatal("symbol mismatch")
	}
	if st2.Intern("c") == st.Intern("a") {
		t.Fatal("new symbol collision")
	}
}

func TestNativeFunc_Gob(t *testing.T) {
	nf := NativeFunc{
		Name: "test",
		Func: func(vm *VM, args []any) (any, error) {
			return 42, nil
		},
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(nf); err != nil {
		t.Fatal(err)
	}

	var nf2 NativeFunc
	dec := gob.NewDecoder(&buf)
	if err := dec.Decode(&nf2); err != nil {
		t.Fatal(err)
	}

	if nf2.Name != "test" {
		t.Fatalf("expected name test, got %s", nf2.Name)
	}

	if nf2.Func == nil {
		t.Fatal("Func is nil")
	}

	_, err := nf2.Func(nil, nil)
	if err == nil {
		t.Fatal("expected error calling restored native func")
	}
	if err.Error() != "native function test is missing" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVM_Drop_EdgeCases(t *testing.T) {
	vm := NewVM(&Function{})
	vm.push(1)
	vm.push(2)

	// drop 0 should do nothing
	vm.drop(0)
	if vm.SP != 2 {
		t.Fatalf("expected SP 2, got %d", vm.SP)
	}

	// drop negative should do nothing
	vm.drop(-1)
	if vm.SP != 2 {
		t.Fatalf("expected SP 2, got %d", vm.SP)
	}

	// drop more than SP should empty stack
	vm.drop(10)
	if vm.SP != 0 {
		t.Fatalf("expected SP 0, got %d", vm.SP)
	}
}

func TestVM_Index_Int64(t *testing.T) {
	t.Run("Get", func(t *testing.T) {
		main := &Function{
			Constants: []any{
				10, 20, // List contents
				int64(1), // Index as int64
				"res",
			},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpMakeList.With(2),
				OpLoadConst.With(2),
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
		if !ok || res.(int) != 20 {
			t.Fatalf("expected 20, got %v", res)
		}
	})

	t.Run("Set", func(t *testing.T) {
		main := &Function{
			Constants: []any{
				10, 20, // List
				int64(1), // Index
				30,       // New value
				"l",
			},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpMakeList.With(2), // [10, 20]
				OpDefVar.With(4),

				OpLoadVar.With(4),
				OpLoadConst.With(2), // index 1 (int64)
				OpLoadConst.With(3), // value 30
				OpSetIndex,
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		l, _ := vm.Get("l")
		sl := l.([]any)
		if sl[1].(int) != 30 {
			t.Fatalf("expected 30, got %v", sl[1])
		}
	})
}

func TestVM_FallOffEnd(t *testing.T) {
	main := &Function{
		Code: []OpCode{
			OpLoadConst.With(0),
		},
		Constants: []any{1},
	}
	vm := NewVM(main)
	// This should run one instruction then return because IP >= len(Code)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if vm.SP != 1 {
		t.Fatal("expected 1 item on stack")
	}
}

func TestVM_PrecomputedSymbols(t *testing.T) {
	vm := NewVM(nil)
	sym := vm.Intern("y") // Symbol(0)

	main := &Function{
		Constants: []any{sym, 456},
		Code: []OpCode{
			OpLoadConst.With(1),
			OpDefVar.With(0),
			OpLoadVar.With(0),
		},
	}
	vm.CurrentFun = main

	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	if val, _ := vm.Get("y"); val.(int) != 456 {
		t.Fatal("failed")
	}
}

// @@ai FIXME vm_test.go:1592: calling non-function: string
func TestVM_Locals(t *testing.T) {
	// fun(a) { a = a + 1; return a }
	fun := &Function{
		NumParams:  1,
		ParamNames: []string{"a"},
		Constants:  []any{"add", 1},
		Code: []OpCode{
			OpGetLocal.With(0),  // a
			OpLoadConst.With(1), // 1
			OpLoadVar.With(0),   // add
			OpCall.With(2),      // add(a, 1)
			OpSetLocal.With(0),  // a = result
			OpGetLocal.With(0),  // a
			OpReturn,
		},
	}

	main := &Function{
		Constants: []any{fun, "res"},
		Code: []OpCode{
			OpMakeClosure.With(0),
			OpLoadConst.With(1), // 1 (arg for fun)
			OpCall.With(1),
			OpDefVar.With(1),
		},
	}

	vm := NewVM(main)
	vm.Def("add", NativeFunc{
		Name: "add",
		Func: func(_ *VM, args []any) (any, error) {
			return args[0].(int) + args[1].(int), nil
		},
	})

	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	res, ok := vm.Get("res")
	if !ok || res.(int) != 2 {
		t.Fatalf("expected 2, got %v", res)
	}
}

// FIXME vm_test.go:1649: expected nil, got <nil>
func TestVM_MapStringAny(t *testing.T) {
	m := map[string]any{
		"foo": 42,
	}
	getMap := NativeFunc{
		Name: "getMap",
		Func: func(_ *VM, _ []any) (any, error) {
			return m, nil
		},
	}

	t.Run("Get", func(t *testing.T) {
		main := &Function{
			Constants: []any{"getMap", "foo", 123, "res", "res2"},
			Code: []OpCode{
				// m = getMap()
				OpLoadVar.With(0),
				OpCall.With(0),

				// res = m["foo"]
				OpLoadConst.With(1),
				OpGetIndex,
				OpDefVar.With(3),

				// m = getMap()
				OpLoadVar.With(0),
				OpCall.With(0),

				// res2 = m[123] -> nil
				OpLoadConst.With(2),
				OpGetIndex,
				OpDefVar.With(4),
			},
		}
		vm := NewVM(main)
		vm.Def("getMap", getMap)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}

		if v, ok := vm.Get("res"); !ok || v.(int) != 42 {
			t.Fatalf("expected 42, got %v", v)
		}
		if v, ok := vm.Get("res2"); !ok || v != nil {
			t.Fatalf("expected nil, got %v", v)
		}
	})

	t.Run("Set", func(t *testing.T) {
		// Set string key
		main := &Function{
			Constants: []any{"getMap", "bar", 99},
			Code: []OpCode{
				OpLoadVar.With(0),
				OpCall.With(0),
				OpLoadConst.With(1),
				OpLoadConst.With(2),
				OpSetIndex,
			},
		}
		vm := NewVM(main)
		vm.Def("getMap", getMap)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if m["bar"] != 99 {
			t.Fatal("map update failed")
		}

		// Set int key (error)
		main = &Function{
			Constants: []any{"getMap", 123, 99},
			Code: []OpCode{
				OpLoadVar.With(0),
				OpCall.With(0),
				OpLoadConst.With(1),
				OpLoadConst.With(2),
				OpSetIndex,
			},
		}
		vm = NewVM(main)
		vm.Def("getMap", getMap)
		var hasErr bool
		for _, err := range vm.Run {
			if err != nil {
				hasErr = true
				break
			}
		}
		if !hasErr {
			t.Fatal("expected error")
		}
	})
}

func TestVM_SetVarSymbol(t *testing.T) {
	vm := NewVM(nil)
	sym := vm.Intern("x")
	main := &Function{
		Constants: []any{sym, 123},
		Code: []OpCode{
			OpLoadConst.With(1),
			OpDefVar.With(0), // Define x=123 using Symbol

			OpLoadConst.With(1),
			OpSetVar.With(0), // Set x=123 using Symbol
		},
	}
	vm.CurrentFun = main
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if v, ok := vm.Get("x"); !ok || v.(int) != 123 {
		t.Fatalf("expected 123, got %v", v)
	}
}

func TestVM_Loop(t *testing.T) {
	// i = 5; while i { i = dec(i) }; return i
	main := &Function{
		Constants: []any{5, "i", "dec"},
		Code: []OpCode{
			// 0: i = 5
			OpLoadConst.With(0),
			OpDefVar.With(1),

			// 2: Loop start
			// if i == 0 (falsey) jump to end
			OpLoadVar.With(1),
			OpJumpFalse.With(5), // 3 -> 9

			// 4: body
			// i = dec(i)
			OpLoadVar.With(2),
			OpLoadVar.With(1),
			OpCall.With(1),
			OpSetVar.With(1),

			// 8: jump back to 2
			OpJump.With(-7), // 9 -> 2

			// 9: End
			OpLoadVar.With(1),
			OpReturn,
		},
	}

	vm := NewVM(main)
	vm.Def("dec", NativeFunc{
		Name: "dec",
		Func: func(_ *VM, args []any) (any, error) {
			return args[0].(int) - 1, nil
		},
	})

	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	v, ok := vm.Get("i")
	if !ok || v.(int) != 0 {
		t.Fatalf("expected 0, got %v", v)
	}
}

func TestVM_Coverage_Extras(t *testing.T) {
	// Helper to run VM in "continue on error" mode
	runContinue := func(vm *VM) {
		vm.Run(func(_ *Interrupt, err error) bool {
			return err != nil // Continue if error
		})
	}

	t.Run("SetIndexMapAny", func(t *testing.T) {
		// Map any keys
		m := make(map[any]any)
		vm := NewVM(&Function{
			Constants: []any{m, 1, 2},
			Code: []OpCode{
				OpLoadConst.With(0), // map
				OpLoadConst.With(1), // key 1
				OpLoadConst.With(2), // val 2
				OpSetIndex,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if m[1].(int) != 2 {
			t.Fatal("map set failed")
		}
	})

	t.Run("SetIndexSliceContinue", func(t *testing.T) {
		sl := []any{10}
		vm := NewVM(&Function{
			Constants: []any{sl, "bad", 0, 99}, // sl, bad_index, good_index, val
			Code: []OpCode{
				// Bad index type
				OpLoadConst.With(0),
				OpLoadConst.With(1), // "bad"
				OpLoadConst.With(3), // 99
				OpSetIndex,

				// Out of bounds
				OpLoadConst.With(0),
				OpLoadConst.With(3), // 99 (index)
				OpLoadConst.With(3), // 99 (val)
				OpSetIndex,
			},
		})
		runContinue(vm)
		// Verify slice unchanged
		if sl[0].(int) != 10 {
			t.Fatal("slice modified")
		}
		if len(sl) != 1 {
			t.Fatal("slice length changed")
		}
	})

	t.Run("GetIndexSliceContinue", func(t *testing.T) {
		sl := []any{10}
		vm := NewVM(&Function{
			Constants: []any{sl, "bad", 99},
			Code: []OpCode{
				// Bad index type -> pushes nil
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpGetIndex,

				// Out of bounds -> pushes nil
				OpLoadConst.With(0),
				OpLoadConst.With(2),
				OpGetIndex,
			},
		})
		runContinue(vm)
		if vm.SP != 2 {
			t.Fatalf("expected 2 items on stack, got %d", vm.SP)
		}
		if vm.OperandStack[0] != nil || vm.OperandStack[1] != nil {
			t.Fatal("expected nils")
		}
	})

	t.Run("GetIndexUnindexableContinue", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1, 0},
			Code: []OpCode{
				OpLoadConst.With(0), // 1 (target)
				OpLoadConst.With(1), // 0 (key)
				OpGetIndex,
			},
		})
		runContinue(vm)
		if vm.SP != 1 {
			t.Fatalf("expected 1 item, got %d", vm.SP)
		}
		if vm.OperandStack[0] != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("SetIndexUnassignableContinue", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1},
			Code: []OpCode{
				OpLoadConst.With(0), // target
				OpLoadConst.With(0), // key
				OpLoadConst.With(0), // val
				OpSetIndex,
			},
		})
		runContinue(vm)
		if vm.SP != 0 {
			t.Fatal("stack not empty")
		}
	})

	t.Run("CallNonFunctionContinue", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{123},
			Code: []OpCode{
				OpLoadConst.With(0), // 123
				OpCall.With(0),      // call 123() -> pushes nil
			},
		})
		runContinue(vm)
		if vm.SP != 1 {
			t.Fatalf("SP %d", vm.SP)
		}
		if vm.OperandStack[0] != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("CallStackUnderflowContinue", func(t *testing.T) {
		vm := NewVM(&Function{
			Code: []OpCode{
				OpCall.With(5),
			},
		})
		runContinue(vm)
		if vm.SP != 0 {
			t.Fatal("stack modified")
		}
	})

	t.Run("SwapUnderflowContinue", func(t *testing.T) {
		vm := NewVM(&Function{
			Code: []OpCode{
				OpLoadConst.With(0),
				OpSwap, // Underflow -> continue -> next op
			},
			Constants: []any{1},
		})
		runContinue(vm)
		// Stack should contain the loaded const
		if vm.SP != 1 {
			t.Fatal("stack corrupted")
		}
	})

	t.Run("SuspendResume", func(t *testing.T) {
		vm := NewVM(&Function{
			Code: []OpCode{
				OpSuspend,
				OpLoadConst.With(0),
			},
			Constants: []any{1},
		})
		vm.Run(func(intr *Interrupt, err error) bool {
			return intr == InterruptSuspend // Resume on suspend
		})
		if vm.SP != 1 {
			t.Fatal("did not resume")
		}
	})

	t.Run("TCO_TopLevel", func(t *testing.T) {
		// Top level tail call. v.BP is 0.
		// main calling target. target returns 42.
		target := &Function{
			Code:      []OpCode{OpLoadConst.With(0), OpReturn},
			Constants: []any{42},
		}
		vm := NewVM(&Function{
			Constants: []any{target},
			Code: []OpCode{
				OpMakeClosure.With(0),
				OpCall.With(0), // Tail call
				OpReturn,
			},
		})
		// We verify that it runs without panic and returns correct value
		// Also this hits the dst=0 path in TCO
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.SP != 1 || vm.OperandStack[0].(int) != 42 {
			t.Fatal("result mismatch")
		}
	})

	t.Run("LoadVarSymbol", func(t *testing.T) {
		vm := NewVM(nil)
		sym := vm.Intern("x")
		vm.CurrentFun = &Function{
			Constants: []any{sym, 42},
			Code: []OpCode{
				OpLoadConst.With(1),
				OpDefVar.With(0),  // x = 42
				OpLoadVar.With(0), // load x
			},
		}
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.pop().(int) != 42 {
			t.Fatal("failed to load var by symbol")
		}
	})

	t.Run("API_Set", func(t *testing.T) {
		vm := NewVM(&Function{})
		vm.Def("x", 1)
		if !vm.Set("x", 2) {
			t.Fatal("Set failed")
		}
		if v, _ := vm.Get("x"); v.(int) != 2 {
			t.Fatal("Set didn't update")
		}
		if vm.Set("y", 3) {
			t.Fatal("Set y should fail")
		}
	})

	t.Run("MakeListMapUnderflowContinue", func(t *testing.T) {
		vm := NewVM(&Function{
			Code: []OpCode{
				OpMakeList.With(5),
				OpMakeMap.With(5),
			},
		})
		runContinue(vm)
		if vm.SP != 0 {
			t.Fatal("stack modified")
		}
	})
}
