package taivm

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"reflect"
	"strings"
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

	t.Run("NativeFuncError", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{"f"},
			Code: []OpCode{
				OpLoadVar.With(0),
				OpCall.With(0),
			},
		})
		vm.Def("f", NativeFunc{
			Name: "f",
			Func: func(*VM, []any) (any, error) {
				return nil, fmt.Errorf("native error")
			},
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
		if vm.SP != 1 {
			t.Fatal("expected 1 item on stack (nil)")
		}
		if vm.pop() != nil {
			t.Fatal("expected nil")
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
	root.Def("x", 100)

	child := root.NewChild()
	child.Def("z", 200)

	val, ok := child.Get("x")
	if !ok || val.(int) != 100 {
		t.Errorf("Get fallback failed: got %v", val)
	}

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

func TestVM_Snapshot_StackCleanup(t *testing.T) {
	main := &Function{
		Name:      "main",
		Constants: []any{42},
		Code: []OpCode{
			OpLoadConst.With(0),
			OpSuspend,
			OpPop,
			OpReturn,
		},
	}
	vm := NewVM(main)
	// Run to suspend
	vm.Run(func(i *Interrupt, e error) bool { return false })

	if vm.SP != 1 || vm.OperandStack[0] != 42 {
		t.Fatal("unexpected state")
	}

	var buf bytes.Buffer
	if err := vm.Snapshot(&buf); err != nil {
		t.Fatal(err)
	}

	vm2 := NewVM(nil)
	if err := vm2.Restore(&buf); err != nil {
		t.Fatal(err)
	}

	if vm2.SP != 1 || vm2.OperandStack[0] != 42 {
		t.Fatal("restored stack mismatch")
	}

	// Verify garbage area is clean
	for i := vm2.SP; i < len(vm2.OperandStack); i++ {
		if vm2.OperandStack[i] != nil {
			t.Errorf("stack not cleaned at index %d", i)
		}
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

func TestVM_OpErrors_Break(t *testing.T) {
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
			Name: "SwapUnderflow",
			Code: []OpCode{OpSwap},
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

	if !nf2.IsMissing() {
		t.Fatal("expected missing")
	}

	_, err := nf2.Call(nil, nil)
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
		sl := l.(*List).Elements
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

func TestVM_Locals(t *testing.T) {
	// fun(a) { a = a + 1; return a }
	fun := &Function{
		NumParams:  1,
		ParamNames: []string{"a"},
		Constants:  []any{"add", 1},
		Code: []OpCode{
			OpLoadVar.With(0),   // add
			OpGetLocal.With(0),  // a
			OpLoadConst.With(1), // 1
			OpCall.With(2),      // add(a, 1)
			OpSetLocal.With(0),  // a = result
			OpGetLocal.With(0),  // a
			OpReturn,
		},
	}

	main := &Function{
		Constants: []any{fun, 1, "res"},
		Code: []OpCode{
			OpMakeClosure.With(0),
			OpLoadConst.With(1), // 1 (arg for fun)
			OpCall.With(1),
			OpDefVar.With(2),
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
		vm.CurrentFun = &Function{
			Constants: []any{"x", 42},
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

func TestVM_SnapshotRestore_Error(t *testing.T) {
	vm := NewVM(&Function{})
	vm.Def("foo", 1)

	err := vm.Snapshot(faultyWriter{err: fmt.Errorf("write error")})
	if err == nil || err.Error() != "write error" {
		t.Fatalf("expected write error, got %v", err)
	}

	err = vm.Restore(faultyReader{err: fmt.Errorf("read error")})
	if err == nil || err.Error() != "read error" {
		t.Fatalf("expected read error, got %v", err)
	}
}

type faultyWriter struct {
	err error
}

func (f faultyWriter) Write(p []byte) (n int, err error) {
	return 0, f.err
}

type faultyReader struct {
	err error
}

func (f faultyReader) Read(p []byte) (n int, err error) {
	return 0, f.err
}

func TestVM_Variadic(t *testing.T) {
	f := &Function{
		NumParams:  2,
		ParamNames: []string{"x", "rest"},
		Variadic:   true,
		Code: []OpCode{
			OpGetLocal.With(1), // rest
			OpReturn,
		},
	}

	main := &Function{
		Constants: []any{
			f,      // 0
			"f",    // 1
			"res0", // 2
			"res1", // 3
			"res2", // 4
			1,      // 5
			2,      // 6
			3,      // 7
		},
		Code: []OpCode{
			// f = closure
			OpMakeClosure.With(0),
			OpDefVar.With(1),

			// res0 = f(1)
			OpLoadVar.With(1),
			OpLoadConst.With(5),
			OpCall.With(1),
			OpDefVar.With(2),

			// res1 = f(1, 2)
			OpLoadVar.With(1),
			OpLoadConst.With(5),
			OpLoadConst.With(6),
			OpCall.With(2),
			OpDefVar.With(3),

			// res2 = f(1, 2, 3)
			OpLoadVar.With(1),
			OpLoadConst.With(5),
			OpLoadConst.With(6),
			OpLoadConst.With(7),
			OpCall.With(3),
			OpDefVar.With(4),
		},
	}

	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	check := func(name string, expectedLen int) {
		val, ok := vm.Get(name)
		if !ok {
			t.Fatalf("%s not found", name)
		}
		list, ok := val.(*List)
		if !ok {
			t.Fatalf("%s not list, got %T", name, val)
		}
		if !list.Immutable {
			t.Fatalf("%s not immutable", name)
		}
		slice := list.Elements
		if len(slice) != expectedLen {
			t.Fatalf("%s len %d, expected %d", name, len(slice), expectedLen)
		}
		if expectedLen > 0 {
			if slice[0].(int) != 2 {
				t.Fatalf("expected 2, got %v", slice[0])
			}
		}
	}

	check("res0", 0)
	check("res1", 1)
	check("res2", 2)
}

func TestVM_DumpTrace(t *testing.T) {
	t.Run("SingleFrame", func(t *testing.T) {
		main := &Function{
			Name: "main",
			Code: []OpCode{
				OpDumpTrace,
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
		expected := "main:0"
		if err.Error() != expected {
			t.Fatalf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("MultiFrame", func(t *testing.T) {
		func2 := &Function{
			Name: "func2",
			Code: []OpCode{
				OpDumpTrace,
				OpReturn,
			},
		}
		func1 := &Function{
			Name: "func1",
			Constants: []any{
				func2,
			},
			Code: []OpCode{
				OpMakeClosure.With(0),
				OpCall.With(0),
				OpJump.With(0), // Prevent TCO
				OpReturn,
			},
		}
		main := &Function{
			Name: "main",
			Constants: []any{
				func1,
			},
			Code: []OpCode{
				OpMakeClosure.With(0),
				OpCall.With(0),
				OpJump.With(0), // Prevent TCO
				OpReturn,
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
		expected := "main:2\nfunc1:2\nfunc2:0"
		if err.Error() != expected {
			t.Fatalf("expected %q, got %q", expected, err.Error())
		}
	})
}

func TestVM_Bitwise(t *testing.T) {
	cases := []struct {
		Name     string
		Op       OpCode
		Operands []any
		Expected any
	}{
		{"And", OpBitAnd, []any{3, 1}, 1},
		{"Or", OpBitOr, []any{3, 1}, 3},
		{"Xor", OpBitXor, []any{3, 1}, 2},
		{"Not", OpBitNot, []any{1}, -2}, // ^1 in Go is -2 (two's complement)
		{"Lsh", OpBitLsh, []any{1, 1}, 2},
		{"Rsh", OpBitRsh, []any{2, 1}, 1},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			code := []OpCode{}
			for i := range c.Operands {
				code = append(code, OpLoadConst.With(i))
			}
			code = append(code, c.Op)
			// For testing result, we'll return it
			code = append(code, OpReturn)

			main := &Function{
				Name:      "main",
				Constants: c.Operands,
				Code:      code,
			}

			vm := NewVM(main)
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}

			if vm.SP != 1 {
				t.Fatalf("expected stack size 1, got %d", vm.SP)
			}
			res := vm.pop()
			if res != c.Expected {
				t.Fatalf("expected %v, got %v", c.Expected, res)
			}
		})
	}
}

func TestVM_BitwiseErrors(t *testing.T) {
	run := func(vm *VM) error {
		var lastErr error
		vm.Run(func(_ *Interrupt, err error) bool {
			lastErr = err
			return false // stop
		})
		return lastErr
	}

	t.Run("TypeMismatch", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1, "bad"},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpBitAnd},
		})
		err := run(vm)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("NegativeShift", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1, -1},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpBitLsh},
		})
		err := run(vm)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("NotTypeMismatch", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{"bad"},
			Code:      []OpCode{OpLoadConst.With(0), OpBitNot},
		})
		err := run(vm)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestVM_Math(t *testing.T) {
	cases := []struct {
		Name     string
		Op       OpCode
		Operands []any
		Expected any
	}{
		{"Add", OpAdd, []any{2, 3}, 5},
		{"Sub", OpSub, []any{5, 2}, 3},
		{"Mul", OpMul, []any{3, 4}, 12},
		{"Div", OpDiv, []any{12, 3}, 4},
		{"Mod", OpMod, []any{5, 2}, 1},
		{"AddString", OpAdd, []any{"foo", "bar"}, "foobar"},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			code := []OpCode{}
			for i := range c.Operands {
				code = append(code, OpLoadConst.With(i))
			}
			code = append(code, c.Op, OpReturn)

			vm := NewVM(&Function{
				Constants: c.Operands,
				Code:      code,
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			if vm.SP != 1 {
				t.Fatalf("expected stack 1, got %d", vm.SP)
			}
			if res := vm.pop(); res != c.Expected {
				t.Fatalf("expected %v, got %v", c.Expected, res)
			}
		})
	}
}

func TestVM_MathErrors(t *testing.T) {
	run := func(vm *VM) error {
		var lastErr error
		vm.Run(func(_ *Interrupt, err error) bool {
			lastErr = err
			return false
		})
		return lastErr
	}

	t.Run("DivZero", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1, 0},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpDiv},
		})
		err := run(vm)
		if err == nil || err.Error() != "division by zero" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("TypeMismatch", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1, "foo"},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpSub},
		})
		err := run(vm)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestVM_Comparison(t *testing.T) {
	cases := []struct {
		Name     string
		Op       OpCode
		Operands []any
		Expected bool
	}{
		{"EqInt", OpEq, []any{1, 1}, true},
		{"NeInt", OpNe, []any{1, 2}, true},
		{"EqString", OpEq, []any{"a", "a"}, true},
		{"LtInt", OpLt, []any{1, 2}, true},
		{"LeInt", OpLe, []any{2, 2}, true},
		{"GtInt", OpGt, []any{2, 1}, true},
		{"GeInt", OpGe, []any{2, 2}, true},
		{"LtString", OpLt, []any{"a", "b"}, true},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			code := []OpCode{}
			for i := range c.Operands {
				code = append(code, OpLoadConst.With(i))
			}
			code = append(code, c.Op, OpReturn)

			vm := NewVM(&Function{
				Constants: c.Operands,
				Code:      code,
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			if res := vm.pop(); res != c.Expected {
				t.Fatalf("expected %v, got %v", c.Expected, res)
			}
		})
	}
}

func TestVM_Logical(t *testing.T) {
	cases := []struct {
		Val      any
		Expected bool
	}{
		{true, false},
		{false, true},
		{nil, true},
		{0, true},
		{1, false},
		{"", true},
		{"foo", false},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%v", c.Val), func(t *testing.T) {
			vm := NewVM(&Function{
				Constants: []any{c.Val},
				Code:      []OpCode{OpLoadConst.With(0), OpNot, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			if res := vm.pop(); res != c.Expected {
				t.Fatalf("expected %v, got %v", c.Expected, res)
			}
		})
	}
}

func TestVM_Bitwise_TypePreservation(t *testing.T) {
	cases := []struct {
		Val      any
		Expected any
	}{
		{int(1), int(^1)},
		{int8(1), int8(^1)},
		{int16(1), int16(^1)},
		{int32(1), int32(^1)},
		{int64(1), int64(^1)},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%T", c.Val), func(t *testing.T) {
			vm := NewVM(&Function{
				Constants: []any{c.Val},
				Code:      []OpCode{OpLoadConst.With(0), OpBitNot, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			res := vm.pop()
			if res != c.Expected {
				t.Fatalf("expected %v (%T), got %v (%T)", c.Expected, c.Expected, res, res)
			}
		})
	}
}

func TestVM_Iter(t *testing.T) {
	t.Run("List", func(t *testing.T) {
		// sum = 0; for x in [1, 2, 3] { sum += x }
		main := &Function{
			Constants: []any{
				0,       // 0: sum init
				"sum",   // 1
				1, 2, 3, // 2, 3, 4: list elements
			},
			Code: []OpCode{
				// sum = 0
				OpLoadConst.With(0),
				OpDefVar.With(1),

				// make list [1, 2, 3]
				OpLoadConst.With(2),
				OpLoadConst.With(3),
				OpLoadConst.With(4),
				OpMakeList.With(3),

				// get iter
				OpGetIter,

				// Loop start
				OpNextIter.With(4),

				// Body
				OpLoadVar.With(1), // sum
				OpAdd,             // val + sum
				OpSetVar.With(1),  // sum = result

				OpJump.With(-5), // Jump back to OpNextIter
			},
		}

		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}

		val, ok := vm.Get("sum")
		if !ok || val.(int) != 6 {
			t.Fatalf("expected 6, got %v", val)
		}
		if vm.SP != 0 {
			t.Fatalf("expected empty stack, got %d", vm.SP)
		}
	})

	t.Run("Map", func(t *testing.T) {
		// keys = ""; for k in {"a": 1, "b": 2} { keys += k }
		// keys should be "ab" because map iter sorts string keys
		main := &Function{
			Constants: []any{
				"", "keys", // 0, 1
				"a", 1, "b", 2, // 2, 3, 4, 5
			},
			Code: []OpCode{
				// keys = ""
				OpLoadConst.With(0),
				OpDefVar.With(1),

				// make map
				OpLoadConst.With(2),
				OpLoadConst.With(3),
				OpLoadConst.With(4),
				OpLoadConst.With(5),
				OpMakeMap.With(2),

				OpGetIter,

				// Loop
				OpNextIter.With(5),

				// Body
				OpLoadVar.With(1), // keys
				OpSwap,            // keys, k -> k, keys
				OpAdd,             // keys + k
				OpSetVar.With(1),

				OpJump.With(-6), // back to NextIter
			},
		}

		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}

		val, ok := vm.Get("keys")
		if !ok || val.(string) != "ab" {
			t.Fatalf("expected 'ab', got %v", val)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		main := &Function{
			Code: []OpCode{
				OpMakeList.With(0),
				OpGetIter,
				OpNextIter.With(2), // Jump to End
				OpSuspend,          // Should not reach
				OpJump.With(-3),
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.SP != 0 {
			t.Fatal("stack not empty")
		}
	})

	t.Run("NextIterNotIterator", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpNextIter.With(1),
			},
		})
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
}

func TestVM_Coverage_Extended(t *testing.T) {
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
		vm.CurrentFun = &Function{
			Constants: []any{"x", 42},
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

	// 1. Break on Error
	// This ensures that when an error occurs, the VM loop terminates (op returns false).
	t.Run("BreakOnError", func(t *testing.T) {
		breakOnError := func(code []OpCode, consts []any) {
			vm := NewVM(&Function{
				Code:      code,
				Constants: consts,
			})
			for _, err := range vm.Run {
				if err != nil {
					break
				}
			}
		}

		cases := []struct {
			Code   []OpCode
			Consts []any
		}{
			{[]OpCode{OpLoadVar.With(0)}, []any{"undef"}},
			{[]OpCode{OpLoadConst.With(0), OpSetVar.With(1)}, []any{1, "undef"}},
			{[]OpCode{OpDup}, nil},
			{[]OpCode{OpDup2}, nil},
			{[]OpCode{OpCall.With(1)}, nil}, // Stack underflow
			{[]OpCode{OpMakeList.With(1)}, nil},
			{[]OpCode{OpMakeMap.With(1)}, nil},
			{[]OpCode{OpLoadConst.With(0), OpGetIndex}, []any{nil}}, // index nil
			{[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpSetIndex}, []any{nil, 1, 1}}, // set index nil
			{[]OpCode{OpSwap}, nil},
			{[]OpCode{OpDumpTrace}, nil},
			{[]OpCode{OpBitNot}, nil},           // underflow
			{[]OpCode{OpAdd}, nil},              // underflow
			{[]OpCode{OpEq}, nil},               // underflow
			{[]OpCode{OpNot}, nil},              // underflow
			{[]OpCode{OpGetIter}, nil},          // underflow
			{[]OpCode{OpNextIter.With(0)}, nil}, // underflow
			{[]OpCode{OpMakeTuple.With(1)}, nil},
			{[]OpCode{OpGetSlice}, nil}, // underflow
			{[]OpCode{OpSetSlice}, nil}, // underflow
			{[]OpCode{OpGetAttr}, nil},  // underflow
			{[]OpCode{OpSetAttr}, nil},  // underflow
		}
		for _, c := range cases {
			breakOnError(c.Code, c.Consts)
		}
	})

	// 2. Numeric Types & Conversions
	t.Run("NumericTypes", func(t *testing.T) {
		types := []any{
			int(1), int8(1), int16(1), int32(1), int64(1),
			uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
			float32(1), float64(1),
			complex64(1 + 1i), complex128(1 + 1i),
		}

		for _, v := range types {
			// Test IsZero via OpNot
			vm := NewVM(&Function{
				Constants: []any{v},
				Code:      []OpCode{OpLoadConst.With(0), OpNot, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatalf("OpNot failed for %T: %v", v, err)
				}
			}
			if vm.pop().(bool) {
				t.Fatalf("%v should not be zero", v)
			}

			// Test toFloat64 via OpAdd with float
			if _, ok := v.(complex64); !ok {
				if _, ok := v.(complex128); !ok {
					vm = NewVM(&Function{
						Constants: []any{v, 1.0},
						Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd, OpReturn},
					})
					for _, err := range vm.Run {
						if err != nil {
							t.Fatalf("Add float failed for %T: %v", v, err)
						}
					}
				}
			}
		}
	})

	// 3. Slice Indices Logic
	t.Run("SliceIndices", func(t *testing.T) {
		runSlice := func(code []OpCode, consts []any) error {
			vm := NewVM(&Function{Code: code, Constants: consts})
			var lastErr error
			for _, err := range vm.Run {
				if err != nil {
					lastErr = err
					break
				}
			}
			return lastErr
		}

		// List for slicing
		l := []any{0, 1, 2, 3, 4}

		// Step 0
		if err := runSlice(
			[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpGetSlice},
			[]any{l, 0, 5, 0}, // start, stop, step=0
		); err == nil {
			t.Fatal("expected step 0 error")
		}

		// Step not int
		if err := runSlice(
			[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpGetSlice},
			[]any{l, 0, 5, "bad"},
		); err == nil {
			t.Fatal("expected step type error")
		}

		// Start not int
		if err := runSlice(
			[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpGetSlice},
			[]any{l, "bad", 5, 1},
		); err == nil {
			t.Fatal("expected start type error")
		}

		// Stop not int
		if err := runSlice(
			[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpGetSlice},
			[]any{l, 0, "bad", 1},
		); err == nil {
			t.Fatal("expected stop type error")
		}

		// Valid Slice with nil/defaults
		vm := NewVM(&Function{
			Constants: []any{l, nil, nil, nil}, // defaults
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpGetSlice},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res := vm.pop().([]any)
		if len(res) != 5 {
			t.Fatal("expected full slice")
		}

		// String slicing
		vm = NewVM(&Function{
			Constants: []any{"hello", 1, 4, 1},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpGetSlice},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res := vm.pop(); res.(string) != "ell" {
			t.Fatalf("string slice failed: %v", res)
		}

		// List slicing
		vm = NewVM(&Function{
			Constants: []any{1, 2, 3, nil},
			Code: []OpCode{
				OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpMakeList.With(3),
				OpLoadConst.With(3), OpLoadConst.With(3), OpLoadConst.With(3), OpGetSlice, // full slice
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		lst := vm.pop().(*List)
		if len(lst.Elements) != 3 {
			t.Fatal("list slice failed")
		}
	})

	// 4. SetSlice Errors
	t.Run("SetSliceErrors", func(t *testing.T) {
		l := []any{1, 2}
		vm := NewVM(&Function{
			Constants: []any{l, 0, 1, 1, []any{9, 9}}, // replace 1 item with 2
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpLoadConst.With(2),
				OpLoadConst.With(3),
				OpLoadConst.With(4),
				OpSetSlice,
			},
		})
		found := false
		for _, err := range vm.Run {
			if err != nil {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected resize error")
		}
	})

	// 5. Attributes
	t.Run("Attributes", func(t *testing.T) {
		s := &Struct{Fields: map[string]any{"a": 1}}
		vm := NewVM(&Function{
			Constants: []any{s, "a", "b", 2},
			Code: []OpCode{
				OpLoadConst.With(0), OpLoadConst.With(1), OpGetAttr,
				OpPop,
				OpLoadConst.With(0), OpLoadConst.With(2), OpLoadConst.With(3), OpSetAttr,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if s.Fields["b"] != 2 {
			t.Fatal("setattr failed")
		}

		runErr := func(code []OpCode, consts []any) {
			vm := NewVM(&Function{Code: code, Constants: consts})
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
		}

		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetAttr}, []any{s, 123})                           // Bad name
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetAttr}, []any{nil, "a"})                         // Nil target
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetAttr}, []any{1, "a"})                           // Bad target
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetAttr}, []any{s, "missing"})                     // Missing field
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpSetAttr}, []any{s, 123, 1})   // Bad name
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpSetAttr}, []any{nil, "a", 1}) // Nil target
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpSetAttr}, []any{1, "a", 1})   // Bad target
	})

	// 6. Complex Math
	t.Run("Complex", func(t *testing.T) {
		c1 := 1 + 2i
		c2 := 3 + 4i
		vm := NewVM(&Function{
			Constants: []any{c1, c2},
			Code: []OpCode{
				OpLoadConst.With(0), OpLoadConst.With(1), OpAdd,
				OpLoadConst.With(0), OpLoadConst.With(1), OpSub,
				OpLoadConst.With(0), OpLoadConst.With(1), OpMul,
				OpLoadConst.With(0), OpLoadConst.With(1), OpDiv,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.SP != 4 {
			t.Fatal("stack mismatch")
		}
	})

	// 7. GetIter map keys not all strings
	t.Run("MapIterNonString", func(t *testing.T) {
		m := map[any]any{1: 1, 2: 2}
		vm := NewVM(&Function{
			Constants: []any{m},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpGetIter,
				OpNextIter.With(2),
				OpPop,
				OpJump.With(-3),
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	// 8. OpJumpFalse with nil (empty stack logic)
	t.Run("JumpFalseNil", func(t *testing.T) {
		vm := NewVM(&Function{
			Code: []OpCode{OpJumpFalse.With(1)},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.IP != 2 {
			t.Fatalf("expected IP 2, got %d", vm.IP)
		}
	})

	// 9. MakeTuple
	t.Run("Tuple", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1},
			Code:      []OpCode{OpLoadConst.With(0), OpMakeTuple.With(1)},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		l := vm.pop().(*List)
		if !l.Immutable {
			t.Fatal("tuple should be immutable")
		}
	})
}

func TestVM_Coverage_More(t *testing.T) {
	runErr := func(name string, code []OpCode, consts []any, expectedErr string) {
		t.Run(name, func(t *testing.T) {
			vm := NewVM(&Function{Code: code, Constants: consts})
			var lastErr error
			// Use explicit yield to ensure we return false and stop VM
			vm.Run(func(_ *Interrupt, err error) bool {
				lastErr = err
				return false
			})
			if lastErr == nil {
				t.Error("expected error, got nil")
				return
			}
			if !strings.Contains(lastErr.Error(), expectedErr) {
				t.Errorf("expected error containing %q, got %v", expectedErr, lastErr)
			}
		})
	}

	runErr("TupleSetIndex",
		[]OpCode{OpLoadConst.With(0), OpMakeTuple.With(1), OpLoadConst.With(1), OpLoadConst.With(0), OpSetIndex},
		[]any{1, 0},
		"tuple is immutable",
	)

	runErr("TupleSetSlice",
		[]OpCode{
			OpLoadConst.With(0), OpMakeTuple.With(1),
			OpLoadConst.With(1), OpLoadConst.With(1), OpLoadConst.With(1), OpLoadConst.With(2), // lo, hi, step, val
			OpSetSlice,
		},
		[]any{1, 0, []any{1}},
		"tuple is immutable",
	)

	runErr("ComplexCompare",
		[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLt},
		[]any{1i, 2i},
		"complex numbers are not ordered",
	)

	runErr("ComplexDivZero",
		[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpDiv},
		[]any{1i, 0i},
		"division by zero",
	)

	runErr("FloatDivZero",
		[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpDiv},
		[]any{1.0, 0.0},
		"division by zero",
	)

	runErr("IterNil",
		[]OpCode{OpLoadConst.With(0), OpGetIter},
		[]any{nil},
		"not iterable: nil",
	)

	runErr("ExtendedSliceSizeMismatch",
		[]OpCode{
			OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpMakeList.With(4), // [1,2,3,4]
			OpLoadConst.With(4), OpLoadConst.With(5), OpLoadConst.With(6), OpLoadConst.With(7), // lo, hi, step, val
			OpSetSlice,
		},
		[]any{1, 2, 3, 4, 0, 4, 2, []any{9}}, // list[0:4:2] -> len 2. val [9] len 1.
		"attempt to assign sequence of size",
	)
}

func TestVM_StringCompare(t *testing.T) {
	cases := []struct {
		Op       OpCode
		A, B     string
		Expected bool
	}{
		{OpLe, "a", "b", true},
		{OpLe, "b", "a", false},
		{OpLe, "a", "a", true},
		{OpGt, "b", "a", true},
		{OpGt, "a", "b", false},
		{OpGe, "b", "a", true},
		{OpGe, "a", "b", false},
		{OpGe, "a", "a", true},
	}

	for _, c := range cases {
		vm := NewVM(&Function{
			Constants: []any{c.A, c.B},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), c.Op, OpReturn},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res := vm.pop().(bool); res != c.Expected {
			t.Errorf("%q %v %q: expected %v, got %v", c.A, c.Op, c.B, c.Expected, res)
		}
	}
}

func TestVM_Slice_NegativeStep(t *testing.T) {
	// [0, 1, 2, 3, 4]
	l := &List{Elements: []any{0, 1, 2, 3, 4}}

	cases := []struct {
		Lo, Hi, Step any
		Expected     []any
	}{
		{nil, nil, -1, []any{4, 3, 2, 1, 0}},
		{4, 1, -1, []any{4, 3, 2}},
		{1, 4, -1, []any{}},
	}

	for i, c := range cases {
		vm := NewVM(&Function{
			Constants: []any{l, c.Lo, c.Hi, c.Step},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpGetSlice},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res := vm.pop().(*List).Elements
		if len(res) != len(c.Expected) {
			t.Errorf("case %d: len mismatch: %v vs %v", i, res, c.Expected)
			continue
		}
		for j := range res {
			if res[j] != c.Expected[j] {
				t.Errorf("case %d: mismatch at %d: %v vs %v", i, j, res[j], c.Expected[j])
			}
		}
	}
}

func TestVM_Bitwise_Uint(t *testing.T) {
	cases := []struct {
		Val      any
		Expected any
	}{
		{uint(1), ^uint(1)},
		{uint8(1), ^uint8(1)},
		{uint16(1), ^uint16(1)},
		{uint32(1), ^uint32(1)},
		{uint64(1), ^uint64(1)},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%T", c.Val), func(t *testing.T) {
			vm := NewVM(&Function{
				Constants: []any{c.Val},
				Code:      []OpCode{OpLoadConst.With(0), OpBitNot, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			res := vm.pop()
			if res != c.Expected {
				t.Fatalf("expected %v (%T), got %v (%T)", c.Expected, c.Expected, res, res)
			}
		})
	}
}

func TestVM_Coverage_GapFilling(t *testing.T) {
	// 1. SetSlice on invalid target
	t.Run("SetSliceInvalidTarget", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{123, 0, 1, 1, []any{9}}, // Target is int(123)
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpLoadConst.With(4),
				OpSetSlice,
			},
		})
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
		if !strings.Contains(err.Error(), "does not support slice assignment") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// 2. Mod Zero
	t.Run("ModZero", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{5, 0},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpMod},
		})
		var err error
		for _, e := range vm.Run {
			if e != nil {
				err = e
				break
			}
		}
		if err == nil || err.Error() != "division by zero" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// 3. Float Comparison
	t.Run("FloatCompare", func(t *testing.T) {
		cases := []struct {
			Op       OpCode
			A, B     float64
			Expected bool
		}{
			{OpLt, 1.0, 2.0, true},
			{OpLt, 2.0, 1.0, false},
			{OpLe, 1.0, 1.0, true},
			{OpGt, 2.0, 1.0, true},
			{OpGe, 1.0, 1.0, true},
		}
		for _, c := range cases {
			vm := NewVM(&Function{
				Constants: []any{c.A, c.B},
				Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), c.Op, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			if res := vm.pop().(bool); res != c.Expected {
				t.Errorf("%v %v %v: expected %v, got %v", c.A, c.Op, c.B, c.Expected, res)
			}
		}
	})

	// 4. Variadic Underflow
	t.Run("VariadicUnderflow", func(t *testing.T) {
		f := &Function{
			NumParams: 2, // 1 fixed + rest
			Variadic:  true,
			Code:      []OpCode{OpReturn},
		}
		vm := NewVM(&Function{
			Constants: []any{f},
			Code: []OpCode{
				OpMakeClosure.With(0),
				OpCall.With(0), // Provide 0 args, need at least 1
			},
		})
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
		if !strings.Contains(err.Error(), "arity mismatch") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// 5. Iterate []any
	t.Run("IterSlice", func(t *testing.T) {
		sl := []any{10, 20}
		vm := NewVM(&Function{
			Constants: []any{sl},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpGetIter,
				OpNextIter.With(2),
				OpReturn, // Should return 10
				OpPop,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res := vm.pop().(int); res != 10 {
			t.Fatalf("expected 10, got %v", res)
		}
	})

	// 6. GetIndex on []any
	t.Run("GetIndexSliceAny", func(t *testing.T) {
		sl := []any{42}
		vm := NewVM(&Function{
			Constants: []any{sl, 0},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpGetIndex,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res := vm.pop().(int); res != 42 {
			t.Fatalf("expected 42, got %v", res)
		}
	})
}

func TestVM_KeywordArgs(t *testing.T) {
	// func(a, b=10) { return a - b }
	f := &Function{
		NumParams:   2,
		ParamNames:  []string{"a", "b"},
		NumDefaults: 1,
		Code: []OpCode{
			OpGetLocal.With(0),
			OpGetLocal.With(1),
			OpSub,
			OpReturn,
		},
	}

	t.Run("Mixed", func(t *testing.T) {
		// f(20, b=5) -> 20 - 5 = 15
		main := &Function{
			Constants: []any{f, 10, 20, "b", 5, "res"},
			Code: []OpCode{
				OpLoadConst.With(1),   // default 10
				OpMakeClosure.With(0), // f

				// Positional: [20]
				OpLoadConst.With(2),
				OpMakeList.With(1),

				// Keyword: {b: 5}
				OpLoadConst.With(3),
				OpLoadConst.With(4),
				OpMakeMap.With(1),

				OpCallKw,
				OpDefVar.With(5),
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res, ok := vm.Get("res"); !ok || res.(int) != 15 {
			t.Fatalf("expected 15, got %v", res)
		}
	})

	t.Run("Defaults", func(t *testing.T) {
		// f(a=20) -> 20 - 10 = 10
		main := &Function{
			Constants: []any{f, 10, "a", 20, "res"},
			Code: []OpCode{
				OpLoadConst.With(1),   // default 10
				OpMakeClosure.With(0), // f

				// Positional: []
				OpMakeList.With(0),

				// Keyword: {a: 20}
				OpLoadConst.With(2),
				OpLoadConst.With(3),
				OpMakeMap.With(1),

				OpCallKw,
				OpDefVar.With(4),
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res, ok := vm.Get("res"); !ok || res.(int) != 10 {
			t.Fatalf("expected 10, got %v", res)
		}
	})

	t.Run("Errors", func(t *testing.T) {
		runErr := func(pos []any, kw map[any]any, expected string) {
			main := &Function{
				Constants: []any{f, 10, pos, kw},
				Code: []OpCode{
					OpLoadConst.With(1),
					OpMakeClosure.With(0),
					OpLoadConst.With(2), // pos
					OpLoadConst.With(3), // kw
					OpCallKw,
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
				t.Errorf("expected error %q, got nil", expected)
				return
			}
			if !strings.Contains(err.Error(), expected) {
				t.Errorf("expected error %q, got %v", expected, err)
			}
		}

		// Unknown keyword
		runErr([]any{1}, map[any]any{"z": 1}, "unexpected keyword argument 'z'")
		// Multiple values
		runErr([]any{1}, map[any]any{"a": 1}, "multiple values for argument 'a'")
		// Missing arg
		runErr([]any{}, map[any]any{}, "missing argument 'a'")
	})
}

func TestVM_Unpack(t *testing.T) {
	t.Run("List", func(t *testing.T) {
		// a, b = [1, 2]
		main := &Function{
			Constants: []any{1, 2, "a", "b"},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpMakeList.With(2),
				OpUnpack.With(2),
				OpDefVar.With(2), // a (stack top is 1)
				OpDefVar.With(3), // b (stack under is 2)
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if v, _ := vm.Get("a"); v.(int) != 1 {
			t.Fatalf("a: expected 1, got %v", v)
		}
		if v, _ := vm.Get("b"); v.(int) != 2 {
			t.Fatalf("b: expected 2, got %v", v)
		}
	})

	t.Run("Range", func(t *testing.T) {
		// a, b = range(0, 2)
		r := &Range{Start: 0, Stop: 2, Step: 1}
		main := &Function{
			Constants: []any{r, "a", "b"},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpUnpack.With(2),
				OpDefVar.With(1), // a
				OpDefVar.With(2), // b
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if v, _ := vm.Get("a"); v.(int64) != 0 {
			t.Fatalf("a: expected 0, got %v", v)
		}
		if v, _ := vm.Get("b"); v.(int64) != 1 {
			t.Fatalf("b: expected 1, got %v", v)
		}
	})

	t.Run("ErrorCount", func(t *testing.T) {
		main := &Function{
			Constants: []any{[]any{1}},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpUnpack.With(2),
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
		if err == nil || !strings.Contains(err.Error(), "expected 2 values, got 1") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestVM_MapMerge(t *testing.T) {
	// m1 = {a: 1}, m2 = {b: 2}
	// m3 = m1 | m2
	m1 := map[any]any{"a": 1}
	m2 := map[any]any{"b": 2}

	main := &Function{
		Constants: []any{m1, m2, "res"},
		Code: []OpCode{
			OpLoadConst.With(0),
			OpLoadConst.With(1),
			OpBitOr,
			OpDefVar.With(2),
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	res, _ := vm.Get("res")
	m3 := res.(map[any]any)
	if len(m3) != 2 {
		t.Fatal("map merge failed")
	}
	if m3["a"].(int) != 1 || m3["b"].(int) != 2 {
		t.Fatal("map values incorrect")
	}
}

func TestVM_FloorDiv(t *testing.T) {
	cases := []struct {
		A, B     any
		Expected any
	}{
		{5, 2, 2},
		{-5, 2, -3},
		{5.0, 2.0, 2.0},
		{-5.0, 2.0, -3.0},
	}
	for _, c := range cases {
		vm := NewVM(&Function{
			Constants: []any{c.A, c.B},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpFloorDiv, OpReturn},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res := vm.pop()
		if res != c.Expected {
			t.Errorf("%v // %v: expected %v, got %v", c.A, c.B, c.Expected, res)
		}
	}
}

func TestVM_Contains(t *testing.T) {
	cases := []struct {
		Container any
		Item      any
		Expected  bool
	}{
		{"abc", "a", true},
		{"abc", "d", false},
		{[]any{1, 2}, 1, true},
		{[]any{1, 2}, 3, false},
		{&List{Elements: []any{1, 2}}, 1, true},
		{map[any]any{"a": 1}, "a", true},
		{map[string]any{"a": 1}, "a", true},
		{&Range{Start: 0, Stop: 5, Step: 1}, int64(2), true},
		{&Range{Start: 0, Stop: 5, Step: 1}, int64(5), false},
	}

	for _, c := range cases {
		vm := NewVM(&Function{
			Constants: []any{c.Container, c.Item},
			Code:      []OpCode{OpLoadConst.With(1), OpLoadConst.With(0), OpContains, OpReturn},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res := vm.pop().(bool); res != c.Expected {
			t.Errorf("%v in %v: expected %v, got %v", c.Item, c.Container, c.Expected, res)
		}
	}
}

func TestVM_Struct(t *testing.T) {
	s := &Struct{}
	main := &Function{
		Constants: []any{s, "x", 42, "y"},
		Code: []OpCode{
			// s.x = 42
			OpLoadConst.With(0),
			OpLoadConst.With(1),
			OpLoadConst.With(2),
			OpSetAttr,

			// y = s.x
			OpLoadConst.With(0),
			OpLoadConst.With(1),
			OpGetAttr,
			OpDefVar.With(3),
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if v, _ := vm.Get("y"); v.(int) != 42 {
		t.Fatalf("expected 42, got %v", v)
	}
}

func TestVM_ListAppendMethod(t *testing.T) {
	// l = [1]
	// l.append(2)
	main := &Function{
		Constants: []any{1, 2, "append", "l"},
		Code: []OpCode{
			// l = [1]
			OpLoadConst.With(0),
			OpMakeList.With(1),
			OpDefVar.With(3),

			// l.append(2) -> l.append (BoundMethod), call(2)
			OpLoadVar.With(3),
			OpLoadConst.With(2),
			OpGetAttr,
			OpLoadConst.With(1),
			OpCall.With(1),
			OpPop, // Discard return value (nil)
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	l, _ := vm.Get("l")
	elems := l.(*List).Elements
	if len(elems) != 2 || elems[1].(int) != 2 {
		t.Fatal("append failed")
	}
}

func TestVM_Range(t *testing.T) {
	// r = range(0, 5, 2) -> 0, 2, 4
	// len(r) == 3
	r := &Range{Start: 0, Stop: 5, Step: 2}
	if r.Len() != 3 {
		t.Fatalf("expected len 3, got %d", r.Len())
	}

	// r = range(5, 0, -2) -> 5, 3, 1
	// len(r) == 3
	r = &Range{Start: 5, Stop: 0, Step: -2}
	if r.Len() != 3 {
		t.Fatalf("expected len 3, got %d", r.Len())
	}

	// Iteration
	// res = []
	// for x in range(0, 3): res.append(x)
	main := &Function{
		Constants: []any{
			&Range{Start: 0, Stop: 3, Step: 1},
			"res", "append",
		},
		Code: []OpCode{
			// res = []
			OpMakeList.With(0),
			OpDefVar.With(1),

			// iter
			OpLoadConst.With(0),
			OpGetIter,

			// loop
			OpNextIter.With(7), // jump to end

			// body: res.append(x)
			OpLoadVar.With(1),
			OpLoadConst.With(2),
			OpGetAttr,
			OpSwap, // method, x
			OpCall.With(1),
			OpPop,

			OpJump.With(-8),

			// End
			OpPop, // iter
			OpReturn,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	res, _ := vm.Get("res")
	elems := res.(*List).Elements
	if len(elems) != 3 {
		t.Fatal("range iter failed")
	}
	if elems[0].(int64) != 0 || elems[2].(int64) != 2 {
		t.Fatal("range values incorrect")
	}
}

func TestVM_ExtendedSlice(t *testing.T) {
	// l = [0, 0, 0]
	// l[0:3:2] = [1, 2] -> [1, 0, 2]
	main := &Function{
		Constants: []any{
			0, 1, 2, 3, "l",
		},
		Code: []OpCode{
			OpLoadConst.With(0), OpLoadConst.With(0), OpLoadConst.With(0),
			OpMakeList.With(3),
			OpDefVar.With(4),

			OpLoadVar.With(4),                                            // target
			OpLoadConst.With(0),                                          // start 0
			OpLoadConst.With(3),                                          // stop 3
			OpLoadConst.With(2),                                          // step 2
			OpLoadConst.With(1), OpLoadConst.With(2), OpMakeList.With(2), // val [1, 2]
			OpSetSlice,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	l, _ := vm.Get("l")
	elems := l.(*List).Elements
	if elems[0].(int) != 1 || elems[2].(int) != 2 {
		t.Fatalf("extended slice assign failed: %v", elems)
	}
}

func TestVM_Coverage_GapFilling_2(t *testing.T) {
	// List Concatenation
	t.Run("ListConcat", func(t *testing.T) {
		l1 := &List{Elements: []any{1}}
		l2 := &List{Elements: []any{2}}
		vm := NewVM(&Function{
			Constants: []any{l1, l2},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd, OpReturn},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res := vm.pop().(*List)
		if len(res.Elements) != 2 || res.Elements[0].(int) != 1 || res.Elements[1].(int) != 2 {
			t.Fatal("list concat failed")
		}
	})

	// Complex Math
	t.Run("ComplexMath", func(t *testing.T) {
		c1 := 1 + 2i
		c2 := 3 + 4i
		// (1+2i)*(3+4i) = 3 + 4i + 6i - 8 = -5 + 10i
		vm := NewVM(&Function{
			Constants: []any{c1, c2},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpMul, OpReturn},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res := vm.pop().(complex128)
		if res != -5+10i {
			t.Fatalf("expected -5+10i, got %v", res)
		}
	})

	// Mod/FloorDiv Signs
	t.Run("ModFloorDivSigns", func(t *testing.T) {
		// 5 // -2 = -3
		// 5 % -2 = -1 (Python style: 5 - (-3)*(-2) = 5 - 6 = -1)
		vm := NewVM(&Function{
			Constants: []any{5, -2},
			Code: []OpCode{
				OpLoadConst.With(0), OpLoadConst.With(1), OpFloorDiv,
				OpLoadConst.With(0), OpLoadConst.With(1), OpMod,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		mod, _ := ToInt64(vm.pop())
		div, _ := ToInt64(vm.pop())
		if div != -3 {
			t.Errorf("expected div -3, got %v", div)
		}
		if mod != -1 {
			t.Errorf("expected mod -1, got %v", mod)
		}
	})

	// BoundMethod with KwArgs
	t.Run("BoundMethodKw", func(t *testing.T) {
		// func method(self, val) { return [self, val] }
		method := &Function{
			NumParams:  2,
			ParamNames: []string{"self", "val"},
			Code: []OpCode{
				OpGetLocal.With(0), OpGetLocal.With(1), OpMakeList.With(2), OpReturn,
			},
		}
		// bound = BoundMethod(self="obj", fun=method)
		bm := &BoundMethod{Receiver: "obj", Fun: &Closure{Fun: method, Env: &Env{}}}

		// Call bound(val=42)
		vm := NewVM(&Function{
			Constants: []any{bm, "val", 42},
			Code: []OpCode{
				OpLoadConst.With(0),                                         // bound method
				OpMakeList.With(0),                                          // pos args
				OpLoadConst.With(1), OpLoadConst.With(2), OpMakeMap.With(1), // kw args {val: 42}
				OpCallKw,
				OpReturn,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		res := vm.pop().(*List)
		if res.Elements[0] != "obj" || res.Elements[1].(int) != 42 {
			t.Fatalf("bound method call failed: %v", res.Elements)
		}
	})

	// Map Unpack Order
	t.Run("MapUnpackOrder", func(t *testing.T) {
		m := map[any]any{"b": 2, "a": 1}
		vm := NewVM(&Function{
			Constants: []any{m},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpUnpack.With(2),
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.pop().(string) != "a" {
			t.Fatal("expected 'a' at top")
		}
		if vm.pop().(string) != "b" {
			t.Fatal("expected 'b' next")
		}
	})

	// Slice Defaults
	t.Run("SliceDefaults", func(t *testing.T) {
		// [0,1,2,3][::] -> [0,1,2,3]
		// [0,1,2,3][::-1] -> [3,2,1,0]
		l := &List{Elements: []any{0, 1, 2, 3}}
		vm := NewVM(&Function{
			Constants: []any{l, nil, -1},
			Code: []OpCode{
				// [::]
				OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(1), OpLoadConst.With(1), OpGetSlice,
				// [::-1]
				OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(1), OpLoadConst.With(2), OpGetSlice,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		rev := vm.pop().(*List).Elements
		fwd := vm.pop().(*List).Elements
		if len(rev) != 4 || rev[0].(int) != 3 {
			t.Fatal("reverse slice failed")
		}
		if len(fwd) != 4 || fwd[0].(int) != 0 {
			t.Fatal("forward slice failed")
		}
	})

	// Closure with Defaults (Positional)
	t.Run("ClosureDefaults", func(t *testing.T) {
		// func f(a, b=2) { return a+b }
		f := &Function{
			NumParams:   2,
			NumDefaults: 1,
			Code: []OpCode{
				OpGetLocal.With(0), OpGetLocal.With(1), OpAdd, OpReturn,
			},
		}
		vm := NewVM(&Function{
			Constants: []any{f, 2, 10},
			Code: []OpCode{
				OpLoadConst.With(1), // default val 2
				OpMakeClosure.With(0),
				OpLoadConst.With(2), // arg 10
				OpCall.With(1),      // f(10) -> 10+2 = 12
				OpReturn,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if res := vm.pop().(int); res != 12 {
			t.Fatalf("expected 12, got %v", res)
		}
	})

	// Range Contains Negative Step
	t.Run("RangeContainsNegative", func(t *testing.T) {
		// range(5, 0, -2) -> 5, 3, 1
		r := &Range{Start: 5, Stop: 0, Step: -2}
		vm := NewVM(&Function{
			Constants: []any{r, 3, 4},
			Code: []OpCode{
				// 3 in r -> true
				OpLoadConst.With(1), OpLoadConst.With(0), OpContains,
				// 4 in r -> false
				OpLoadConst.With(2), OpLoadConst.With(0), OpContains,
			},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.pop().(bool) {
			t.Fatal("4 should not be in range")
		}
		if !vm.pop().(bool) {
			t.Fatal("3 should be in range")
		}
	})
}

func TestVM_Coverage_More_3(t *testing.T) {
	// ListAppend: Immutable
	t.Run("ListAppend_Immutable", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1, "append", 2},
			Code: []OpCode{
				OpLoadConst.With(0), OpMakeTuple.With(1), // Tuple (1,)
				OpLoadConst.With(1), OpGetAttr, // .append
				OpLoadConst.With(2), OpCall.With(1), // .append(2)
			},
		})
		var found bool
		for _, err := range vm.Run {
			if err != nil && err.Error() == "list is immutable" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected immutable error")
		}
	})

	// ListAppend: Arg count mismatch
	t.Run("ListAppend_Args", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{1, "append"},
			Code: []OpCode{
				OpLoadConst.With(0), OpMakeList.With(1),
				OpLoadConst.With(1), OpGetAttr,
				OpCall.With(0), // No args, expects 1 (plus receiver)
			},
		})
		var found bool
		for _, err := range vm.Run {
			if err != nil && err.Error() == "append expects 1 argument" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected argument count error")
		}
	})

	// ListAppend: Receiver not list
	t.Run("ListAppend_Receiver", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{"append", 1},
			Code: []OpCode{
				OpLoadVar.With(0),
				OpLoadConst.With(1),
				OpLoadConst.With(1),
				OpCall.With(2),
			},
		})
		vm.Def("append", NativeFunc{Name: "append", Func: ListAppend})
		var found bool
		for _, err := range vm.Run {
			if err != nil && err.Error() == "receiver must be list" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected receiver error")
		}
	})

	// Range Len
	t.Run("Range_Len", func(t *testing.T) {
		if (&Range{Start: 10, Stop: 0, Step: 1}).Len() != 0 {
			t.Error("expected 0")
		}
		if (&Range{Start: 0, Stop: 10, Step: -1}).Len() != 0 {
			t.Error("expected 0")
		}
		if (&Range{Step: 0}).Len() != 0 {
			t.Error("expected 0")
		}
	})

	// Int64 Arithmetic & Bitwise
	t.Run("Int64_Ops", func(t *testing.T) {
		ops := []struct {
			Op  OpCode
			B   int64
			Res int64
		}{
			{OpAdd, 3, 13},
			{OpSub, 3, 7},
			{OpMul, 3, 30},
			{OpDiv, 3, 3},
			{OpMod, 3, 1},
			{OpFloorDiv, 3, 3},
			{OpBitAnd, 3, 2},
			{OpBitOr, 3, 11},
			{OpBitXor, 3, 9},
			{OpBitLsh, 1, 20},
			{OpBitRsh, 1, 5},
		}
		for _, op := range ops {
			vm := NewVM(&Function{
				Constants: []any{int64(10), op.B},
				Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), op.Op, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatalf("Op %v failed: %v", op.Op, err)
				}
			}
			res := vm.pop()
			if res != op.Res {
				t.Errorf("Op %v: expected %v, got %v", op.Op, op.Res, res)
			}
		}
	})

	// Int64 Div/Mod Zero
	t.Run("Int64_DivZero", func(t *testing.T) {
		ops := []OpCode{OpDiv, OpMod, OpFloorDiv}
		for _, op := range ops {
			vm := NewVM(&Function{
				Constants: []any{int64(1), int64(0)},
				Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), op},
			})
			var found bool
			for _, err := range vm.Run {
				if err != nil && err.Error() == "division by zero" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Op %v expected zero error", op)
			}
		}
	})

	// Int64 Negative Shift
	t.Run("Int64_ShiftNeg", func(t *testing.T) {
		ops := []OpCode{OpBitLsh, OpBitRsh}
		for _, op := range ops {
			vm := NewVM(&Function{
				Constants: []any{int64(1), int64(-1)},
				Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), op},
			})
			var found bool
			for _, err := range vm.Run {
				if err != nil && strings.Contains(err.Error(), "negative shift") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Op %v expected negative shift error", op)
			}
		}
	})

	// Float64 Math
	t.Run("Float64_Math", func(t *testing.T) {
		ops := []struct {
			Op  OpCode
			Res float64
		}{
			{OpAdd, 12.5},
			{OpSub, 7.5},
			{OpMul, 25.0},
			{OpDiv, 4.0},
			{OpFloorDiv, 4.0},
		}
		for _, op := range ops {
			vm := NewVM(&Function{
				Constants: []any{10.0, 2.5},
				Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), op.Op, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			res := vm.pop()
			if res != op.Res {
				t.Errorf("Op %v: expected %v, got %v", op.Op, op.Res, res)
			}
		}
	})

	// Float64 Div Zero
	t.Run("Float64_DivZero", func(t *testing.T) {
		ops := []OpCode{OpDiv, OpFloorDiv}
		for _, op := range ops {
			vm := NewVM(&Function{
				Constants: []any{1.0, 0.0},
				Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), op},
			})
			var found bool
			for _, err := range vm.Run {
				if err != nil && err.Error() == "division by zero" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Op %v expected zero error", op)
			}
		}
	})

	// Native Func KwArgs
	t.Run("Native_KwArgs", func(t *testing.T) {
		vm := NewVM(&Function{
			Constants: []any{"f", "k", 1},
			Code: []OpCode{
				OpLoadVar.With(0),
				OpMakeList.With(0),
				OpLoadConst.With(1), OpLoadConst.With(2), OpMakeMap.With(1),
				OpCallKw,
			},
		})
		vm.Def("f", NativeFunc{Name: "f", Func: func(_ *VM, _ []any) (any, error) { return nil, nil }})
		var found bool
		for _, err := range vm.Run {
			if err != nil && strings.Contains(err.Error(), "native functions do not support keyword arguments") {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected error")
		}
	})

	// ToInt64 Helpers
	t.Run("ToInt64_Helper", func(t *testing.T) {
		vals := []any{
			int(1), int8(1), int16(1), int32(1), int64(1),
			uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		}
		for _, v := range vals {
			if i, ok := ToInt64(v); !ok || i != 1 {
				t.Errorf("failed for %T", v)
			}
		}
		if _, ok := ToInt64("s"); ok {
			t.Error("should fail for string")
		}
	})

	// isZero via OpNot
	t.Run("IsZero", func(t *testing.T) {
		zeros := []any{
			int(0), int64(0), float64(0), complex128(0),
			"", false, nil,
		}
		for _, v := range zeros {
			vm := NewVM(&Function{
				Constants: []any{v},
				Code:      []OpCode{OpLoadConst.With(0), OpNot, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			if !vm.pop().(bool) {
				t.Errorf("isZero failed for %v", v)
			}
		}
	})

	// OpCode With
	t.Run("OpCode_With", func(t *testing.T) {
		op := OpLoadConst
		if op.With(255) != op|(255<<8) {
			t.Error("OpCode.With failed")
		}
	})

	// Int BitNot Types
	t.Run("BitNot_Types", func(t *testing.T) {
		vals := []any{
			int(1), int8(1), int16(1), int32(1), int64(1),
			uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		}
		for _, v := range vals {
			vm := NewVM(&Function{
				Constants: []any{v},
				Code:      []OpCode{OpLoadConst.With(0), OpBitNot, OpReturn},
			})
			for _, err := range vm.Run {
				if err != nil {
					t.Fatal(err)
				}
			}
			vm.pop()
		}
	})
}

func TestVM_Coverage_Final(t *testing.T) {
	runContinue := func(code []OpCode, consts []any) {
		vm := NewVM(&Function{Code: code, Constants: consts})
		vm.Run(func(_ *Interrupt, err error) bool {
			return err != nil
		})
	}

	t.Run("Complex64_Ops", func(t *testing.T) {
		c := complex64(1 + 2i)
		vm := NewVM(&Function{
			Constants: []any{c},
			Code:      []OpCode{OpLoadConst.With(0), OpNot, OpReturn},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.pop().(bool) {
			t.Fatal("expected false")
		}
	})

	t.Run("ComplexMix", func(t *testing.T) {
		// Float + Complex
		vm := NewVM(&Function{
			Constants: []any{1.0, 2i},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.pop() != 1+2i {
			t.Fatal("float+complex failed")
		}

		// Int + Complex
		vm = NewVM(&Function{
			Constants: []any{1, 2i},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if vm.pop() != 1+2i {
			t.Fatal("int+complex failed")
		}
	})

	t.Run("SliceStep0", func(t *testing.T) {
		runContinue(
			[]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(1), OpLoadConst.With(2), OpGetSlice},
			[]any{[]any{1}, 0, 0},
		)
	})

	t.Run("ContinueOnError", func(t *testing.T) {
		// Unpack
		runContinue([]OpCode{OpLoadConst.With(0), OpUnpack.With(1)}, []any{1})        // not iterable
		runContinue([]OpCode{OpLoadConst.With(0), OpUnpack.With(2)}, []any{[]any{1}}) // size mismatch

		// Compare
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLt}, []any{1i, 2i})        // complex
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLt}, []any{struct{}{}, 1}) // uncomparable
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpLt}, []any{struct{}{}, struct{}{}})

		// Div Zero
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpDiv}, []any{1, 0})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpDiv}, []any{int64(1), int64(0)})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpDiv}, []any{1.0, 0.0})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpDiv}, []any{1i, 0i})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpMod}, []any{int64(1), int64(0)})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpFloorDiv}, []any{1.0, 0.0})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpFloorDiv}, []any{int64(1), int64(0)})

		// Bitwise
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpBitLsh}, []any{1, -1})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpBitRsh}, []any{int64(1), int64(-1)})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpBitAnd}, []any{1, "s"})

		// List/Map
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpListAppend}, []any{1, 2})                      // not list
		runContinue([]OpCode{OpLoadConst.With(0), OpMakeTuple.With(1), OpLoadConst.With(1), OpListAppend}, []any{1, 2}) // immutable

		// Contains
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpContains}, []any{1, 2})   // not iterable
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpContains}, []any{"s", 1}) // string contains int

		// Math
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd}, []any{complex128(1), "s"})
		runContinue([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd}, []any{1.0, "s"})

		// Iter
		runContinue([]OpCode{OpLoadConst.With(0), OpGetIter}, []any{1})
		runContinue([]OpCode{OpLoadConst.With(0), OpNextIter.With(0)}, []any{1})
	})
}

func TestVM_Coverage_Deep(t *testing.T) {
	// Helper for continue-on-error tests
	runContinue := func(code []OpCode, consts []any) {
		vm := NewVM(&Function{Code: code, Constants: consts})
		vm.Run(func(_ *Interrupt, err error) bool {
			return err != nil
		})
	}

	// Stack underflow continue tests
	runContinue([]OpCode{OpMakeList.With(1)}, nil)
	runContinue([]OpCode{OpMakeMap.With(1)}, nil)
	runContinue([]OpCode{OpNextIter.With(0)}, nil)
	runContinue([]OpCode{OpMakeTuple.With(1)}, nil)
	runContinue([]OpCode{OpGetSlice}, nil)
	runContinue([]OpCode{OpSetSlice}, nil)
	runContinue([]OpCode{OpGetAttr}, nil)
	runContinue([]OpCode{OpSetAttr}, nil)
	runContinue([]OpCode{OpCallKw}, nil)
	runContinue([]OpCode{OpUnpack.With(1)}, nil)

	// OpSetSlice errors continue
	// Immutable
	runContinue([]OpCode{
		OpLoadConst.With(0), OpMakeTuple.With(1), // Tuple
		OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpLoadConst.With(4),
		OpSetSlice,
	}, []any{1, 0, 1, 1, 1})

	// Resize extended
	runContinue([]OpCode{
		OpLoadConst.With(0), OpLoadConst.With(1), OpLoadConst.With(2), OpMakeList.With(3), // [1, 2, 3]
		OpLoadConst.With(0), // lo 0
		OpLoadConst.With(1), // hi 2
		OpLoadConst.With(3), // step 2
		OpLoadConst.With(4), // val [1, 2]
		OpSetSlice,
	}, []any{1, 2, 3, 2, []any{1, 2}})

	// OpSetAttr errors continue
	// Name not string
	runContinue([]OpCode{
		OpLoadConst.With(0), // target
		OpLoadConst.With(1), // name (int)
		OpLoadConst.With(2), // val
		OpSetAttr,
	}, []any{&Struct{}, 123, 1})
	// Target nil
	runContinue([]OpCode{
		OpLoadConst.With(0), // target nil
		OpLoadConst.With(1), // name
		OpLoadConst.With(2), // val
		OpSetAttr,
	}, []any{nil, "a", 1})

	// ToInt64 coverage (uint types)
	// We can force this via OpBitNot on uint types, ensuring we use all uint variants
	types := []any{
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
	}
	for _, v := range types {
		runContinue([]OpCode{OpLoadConst.With(0), OpBitNot, OpReturn}, []any{v})
	}
}

func TestVM_Range_Index(t *testing.T) {
	// r = range(0, 10, 2) -> 0, 2, 4, 6, 8. Len = 5
	r := &Range{Start: 0, Stop: 10, Step: 2}

	cases := []struct {
		Idx      any
		Expected int64
	}{
		{0, 0},
		{1, 2},
		{4, 8},
		{-1, 8},
		{-5, 0},
		{int64(1), 2},
	}

	for _, c := range cases {
		vm := NewVM(&Function{
			Constants: []any{r, c.Idx},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetIndex, OpReturn},
		})
		for _, err := range vm.Run {
			if err != nil {
				t.Errorf("idx %v: %v", c.Idx, err)
			}
		}
		if res := vm.pop().(int64); res != c.Expected {
			t.Errorf("idx %v: expected %v, got %v", c.Idx, c.Expected, res)
		}
	}

	// Out of bounds
	runErr := func(idx any) {
		vm := NewVM(&Function{
			Constants: []any{r, idx},
			Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetIndex},
		})
		var found bool
		for _, err := range vm.Run {
			if err != nil && strings.Contains(err.Error(), "index out of bounds") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("idx %v: expected out of bounds error", idx)
		}
	}
	runErr(5)
	runErr(-6)
	runErr(int64(5))

	// Invalid type
	vm := NewVM(&Function{
		Constants: []any{r, "bad"},
		Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpGetIndex},
	})
	var found bool
	for _, err := range vm.Run {
		if err != nil && strings.Contains(err.Error(), "range index must be integer") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected invalid type error")
	}
}

func TestVM_Bitwise_Mixed(t *testing.T) {
	// int(3) & int64(1) -> 1
	vm := NewVM(&Function{
		Constants: []any{3, int64(1)},
		Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpBitAnd, OpReturn},
	})
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if res := vm.pop().(int64); res != 1 {
		t.Fatalf("expected 1, got %v (%T)", res, res)
	}

	// int(1) << int64(1) -> 2
	vm = NewVM(&Function{
		Constants: []any{1, int64(1)},
		Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpBitLsh, OpReturn},
	})
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if res := vm.pop().(int64); res != 2 {
		t.Fatalf("expected 2, got %v (%T)", res, res)
	}
}

func TestVM_Math_Mixed(t *testing.T) {
	// int(1) + float64(2.5) -> 3.5
	vm := NewVM(&Function{
		Constants: []any{1, 2.5},
		Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd, OpReturn},
	})
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if res := vm.pop().(float64); res != 3.5 {
		t.Fatalf("expected 3.5, got %v", res)
	}

	// int(5) + int64(2) -> 7 (int64)
	vm = NewVM(&Function{
		Constants: []any{5, int64(2)},
		Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd, OpReturn},
	})
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if res := vm.pop().(int64); res != 7 {
		t.Fatalf("expected 7, got %v", res)
	}

	// complex + int
	vm = NewVM(&Function{
		Constants: []any{1 + 1i, 1},
		Code:      []OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAdd, OpReturn},
	})
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if res := vm.pop().(complex128); res != 2+1i {
		t.Fatalf("expected 2+1i, got %v", res)
	}
}

func TestVM_Repro_SliceResizing_RawSlice(t *testing.T) {
	// Create a native function that returns a raw []any
	makeRawSlice := NativeFunc{
		Name: "makeRawSlice",
		Func: func(v *VM, args []any) (any, error) {
			return []any{1, 2, 3}, nil
		},
	}

	main := &Function{
		Constants: []any{
			makeRawSlice,
			int64(0), // start
			int64(1), // stop
			// step (nil) is passed as a nil constant? No, we need to handle nil.
			// The VM doesn't have a specific OpLoadNil.
			// We can put nil in constants.
			nil, // step
			&List{Elements: []any{8, 9}, Immutable: false}, // replacement (length 2 vs length 1 slice)
		},
		Code: []OpCode{
			OpLoadConst.With(0), // Load NativeFunc
			OpCall.With(0),      // Call it -> returns []any{1,2,3}
			OpLoadConst.With(1), // Load 0 (start)
			OpLoadConst.With(2), // Load 1 (stop) (exclusive)
			OpLoadConst.With(3), // Load nil (step)
			OpLoadConst.With(4), // Load replacement value (list of 2 items)
			OpSetSlice,
			OpReturn,
		},
	}

	vm := NewVM(main)

	// We expect an error because raw slices cannot be resized in place
	vm.Run(func(i *Interrupt, err error) bool {
		if err != nil {
			if err.Error() == "cannot resize raw slice, use List instead" {
				// This is the expected behavior for now
				return false // Stop VM
			}
			t.Errorf("Unexpected error: %v", err)
			return false
		}
		return true
	})
}

func TestVM_Repro_IntPowPrecision(t *testing.T) {
	// Test 3^35, which fits in int64 but might lose precision in float64?
	// 3^35 = 50031545098999707

	main := &Function{
		Constants: []any{
			int64(3),
			int64(35),
		},
		Code: []OpCode{
			OpLoadConst.With(0),
			OpLoadConst.With(1),
			OpPow,
			OpReturn,
		},
	}

	vm := NewVM(main)

	vm.Run(func(i *Interrupt, err error) bool {
		if err != nil {
			t.Fatalf("VM error: %v", err)
		}
		return true
	})

	res := vm.OperandStack[0]
	if resInt, ok := res.(int64); ok {
		expected := int64(50031545098999707)
		if resInt != expected {
			t.Errorf("Precision loss detected: got %d, want %d", resInt, expected)
		}
	} else {
		t.Errorf("Result type mismatch: got %T", res)
	}
}

func TestVM_Repro_SliceStepZero(t *testing.T) {
	main := &Function{
		Constants: []any{
			&List{Elements: []any{1, 2, 3}},
			int64(0), // start
			int64(2), // stop
			int64(0), // step (INVALID)
		},
		Code: []OpCode{
			OpLoadConst.With(0),
			OpLoadConst.With(1),
			OpLoadConst.With(2),
			OpLoadConst.With(3),
			OpGetSlice,
			OpReturn,
		},
	}

	vm := NewVM(main)

	vm.Run(func(i *Interrupt, err error) bool {
		if err != nil {
			if err.Error() == "slice step cannot be zero" {
				return false
			}
			t.Errorf("Unexpected error: %v", err)
			return false
		}
		return true
	})
}

func TestVM_ClosureSymbolIsolation(t *testing.T) {
	sharedFunc := &Function{
		NumParams:  2,
		ParamNames: []string{"x", "y"},
		Constants:  []any{"x", "y"},
		Code: []OpCode{
			OpLoadVar.With(0),
			OpLoadVar.With(1),
			OpAdd,
			OpReturn,
		},
	}

	vm1 := NewVM(&Function{})
	vm1.Def("unrelated_var1", 100)
	vm1.Def("unrelated_var2", 200)

	closure1 := &Closure{
		Fun: sharedFunc,
		Env: vm1.Scope,
	}

	vm2 := NewVM(&Function{})
	vm2.Def("different_var1", 300)
	vm2.Def("different_var2", 400)
	vm2.Def("different_var3", 500)

	closure2 := &Closure{
		Fun: sharedFunc,
		Env: vm2.Scope,
	}

	testCall := func(vm *VM, closure *Closure, arg1, arg2, expected int) {
		vm.CurrentFun = &Function{
			Constants: []any{closure, arg1, arg2},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpLoadConst.With(2),
				OpCall.With(2),
				OpReturn,
			},
		}
		vm.IP = 0
		vm.SP = 0

		for _, err := range vm.Run {
			if err != nil {
				t.Fatalf("VM error: %v", err)
			}
		}

		result := vm.OperandStack[0].(int)
		if result != expected {
			t.Errorf("Expected %d, got %d", expected, result)
		}
	}

	testCall(vm1, closure1, 10, 20, 30)
	testCall(vm2, closure2, 5, 15, 20)
	testCall(vm1, closure1, 100, 200, 300)
}

func TestVM_Pointer(t *testing.T) {
	t.Run("Variable", func(t *testing.T) {
		main := &Function{
			Constants: []any{"x", 10, 20, "y", "p"},
			Code: []OpCode{
				OpLoadConst.With(1), OpDefVar.With(0), // x = 10
				OpAddrOf.With(0), OpDefVar.With(4), // p = &x
				OpLoadVar.With(4), OpDeref, OpDefVar.With(3), // y = *p
				OpLoadVar.With(4), OpLoadConst.With(2), OpSetDeref, // *p = 20
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if x, _ := vm.Get("x"); x.(int) != 20 {
			t.Errorf("x: expected 20, got %v", x)
		}
		if y, _ := vm.Get("y"); y.(int) != 10 {
			t.Errorf("y: expected 10, got %v", y)
		}
	})

	t.Run("ListIndex", func(t *testing.T) {
		main := &Function{
			Constants: []any{"l", 1, 2, 3, 1, 42, "y", "p"},
			Code: []OpCode{
				OpLoadConst.With(1), OpLoadConst.With(2), OpLoadConst.With(3), OpMakeList.With(3),
				OpDefVar.With(0),                                                        // l = [1, 2, 3]
				OpLoadVar.With(0), OpLoadConst.With(4), OpAddrOfIndex, OpDefVar.With(7), // p = &l[1]
				OpLoadVar.With(7), OpDeref, OpDefVar.With(6), // y = *p
				OpLoadVar.With(7), OpLoadConst.With(5), OpSetDeref, // *p = 42
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		l, _ := vm.Get("l")
		elems := l.(*List).Elements
		if elems[1].(int) != 42 {
			t.Errorf("l[1]: expected 42, got %v", elems[1])
		}
		if y, _ := vm.Get("y"); y.(int) != 2 {
			t.Errorf("y: expected 2, got %v", y)
		}
	})

	t.Run("StructAttr", func(t *testing.T) {
		s := &Struct{Fields: map[string]any{"a": 1}}
		main := &Function{
			Constants: []any{s, "a", 42, "y", "p"},
			Code: []OpCode{
				OpLoadConst.With(0), OpLoadConst.With(1), OpAddrOfAttr, OpDefVar.With(4), // p = &s.a
				OpLoadVar.With(4), OpDeref, OpDefVar.With(3), // y = *p
				OpLoadVar.With(4), OpLoadConst.With(2), OpSetDeref, // *p = 42
			},
		}
		vm := NewVM(main)
		for _, err := range vm.Run {
			if err != nil {
				t.Fatal(err)
			}
		}
		if s.Fields["a"].(int) != 42 {
			t.Errorf("s.a: expected 42, got %v", s.Fields["a"])
		}
		if y, _ := vm.Get("y"); y.(int) != 1 {
			t.Errorf("y: expected 1, got %v", y)
		}
	})

	t.Run("Errors", func(t *testing.T) {
		runErr := func(code []OpCode, consts []any, expected string) {
			vm := NewVM(&Function{Code: code, Constants: consts})
			var lastErr error
			vm.Run(func(_ *Interrupt, err error) bool {
				lastErr = err
				return false
			})
			if lastErr == nil || !strings.Contains(lastErr.Error(), expected) {
				t.Errorf("expected error %q, got %v", expected, lastErr)
			}
		}

		runErr([]OpCode{OpLoadConst.With(0), OpDeref}, []any{1}, "not a pointer")
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(0), OpSetDeref}, []any{1}, "not a pointer")
		runErr([]OpCode{OpAddrOf.With(0)}, []any{"undef"}, "undefined variable")
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAddrOfIndex}, []any{nil, 0}, "indexing nil")
		runErr([]OpCode{OpLoadConst.With(0), OpLoadConst.With(1), OpAddrOfAttr}, []any{nil, "a"}, "getattr on nil")
	})
}

func TestVM_IndexBounds(t *testing.T) {
	t.Run("SetIndex_ListOutOfBounds", func(t *testing.T) {
		main := &Function{
			Constants: []any{
				&List{Elements: []any{1}},
				2,  // Out of bounds index
				42, // Value
			},
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpLoadConst.With(2),
				OpSetIndex,
			},
		}
		vm := NewVM(main)
		var err error
		vm.Run(func(_ *Interrupt, e error) bool {
			err = e
			return false
		})
		if err == nil || !strings.Contains(err.Error(), "index out of bounds") {
			t.Fatalf("expected index out of bounds error, got %v", err)
		}
	})

	t.Run("GetIndex_PointerOutOfBounds", func(t *testing.T) {
		l := &List{Elements: []any{1, 2, 3}}
		p := &Pointer{Target: l, Key: 1, ArrayType: FromReflectType(reflect.TypeOf([2]any{}))} // &l[1] (l[1], l[2])
		main := &Function{
			Constants: []any{p, 2}, // Accessing index 2 of pointer (offset 1+2=3), which is out of bounds for l
			Code: []OpCode{
				OpLoadConst.With(0),
				OpLoadConst.With(1),
				OpGetIndex,
			},
		}
		vm := NewVM(main)
		var err error
		vm.Run(func(_ *Interrupt, e error) bool {
			err = e
			return false
		})
		if err == nil || !strings.Contains(err.Error(), "index out of bounds") {
			t.Fatalf("expected index out of bounds error, got %v", err)
		}
	})
}

func TestType_String(t *testing.T) {
	cases := []struct {
		typ  *Type
		want string
	}{
		{FromReflectType(reflect.TypeOf(0)), "int"},
		{FromReflectType(reflect.TypeOf([]int{})), "[]int"},
		{FromReflectType(reflect.TypeOf([2]int{})), "[2]int"},
		{FromReflectType(reflect.TypeOf(map[string]int{})), "map[string]int"},
		{FromReflectType(reflect.TypeOf(func(int, ...string) (bool, error) { return false, nil })), "func(int, ...string) (bool, error)"},
		{FromReflectType(reflect.TypeOf((*interface {
			Foo(int) string
			Bar()
		})(nil)).Elem()), "interface { Bar(); Foo(int) string }"},
		{&Type{Kind: KindInt}, "int"},
		{&Type{Name: "MyInt", Kind: KindInt}, "MyInt"},
	}
	for _, c := range cases {
		if got := c.typ.String(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestVM_TickYield(t *testing.T) {
	main := &Function{
		Name: "main",
		Code: []OpCode{
			OpPop,
			OpPop,
			OpPop,
			OpPop,
		},
	}
	vm := NewVM(main)
	vm.push(1)
	vm.push(2)
	vm.push(3)
	vm.push(4)
	vm.YieldTicks = 2

	yieldCount := 0
	vm.Run(func(intr *Interrupt, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		if intr == InterruptYield {
			yieldCount++
		}
		return true
	})

	if yieldCount != 2 {
		t.Fatalf("expected 2 yields, got %d", yieldCount)
	}
	if vm.SP != 0 {
		t.Fatalf("expected SP 0, got %d", vm.SP)
	}
}

func TestVM_TickYieldResume(t *testing.T) {
	main := &Function{
		Name: "main",
		Code: []OpCode{
			OpPop,
			OpPop,
			OpPop,
			OpPop,
		},
	}
	vm := NewVM(main)
	vm.push(1)
	vm.push(2)
	vm.push(3)
	vm.push(4)
	vm.YieldTicks = 3

	// First run should yield after 3 pops
	vm.Run(func(intr *Interrupt, err error) bool {
		if intr == InterruptYield {
			return false // Suspend on yield
		}
		return true
	})

	if vm.IP != 3 {
		t.Fatalf("expected IP 3, got %d", vm.IP)
	}
	if vm.SP != 1 {
		t.Fatalf("expected SP 1, got %d", vm.SP)
	}

	// Second run should finish
	vm.Run(func(intr *Interrupt, err error) bool {
		return true
	})

	if vm.IP != 4 {
		t.Fatalf("expected IP 4, got %d", vm.IP)
	}
	if vm.SP != 0 {
		t.Fatalf("expected SP 0, got %d", vm.SP)
	}
}

func TestVM_ExternalTypeAssertNil(t *testing.T) {
	errorType := reflect.TypeFor[error]()
	main := &Function{
		Constants: []any{
			nil,
			errorType,
		},
		Code: []OpCode{
			OpLoadConst.With(0), // nil
			OpLoadConst.With(1), // error type
			OpTypeAssert,
			OpReturn,
		},
	}
	vm := NewVM(main)
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	if vm.pop() != nil {
		t.Fatal("expected nil")
	}
}

func TestVM_EnvVarType(t *testing.T) {
	intType := FromReflectType(reflect.TypeFor[int]())
	main := &Function{
		Constants: []any{
			"x",
			intType,
		},
		Code: []OpCode{
			OpAddrOf.With(0),
			OpReturn,
		},
	}
	vm := NewVM(main)
	vm.DefWithType("x", 42, intType)

	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}

	ptr := vm.pop().(*Pointer)
	vr, ok := ptr.Target.(*Env).GetVar("x")
	if !ok {
		t.Fatal("x not found")
	}
	if vr.Type != intType {
		t.Fatal("type mismatch")
	}
}

func TestTypeEquality(t *testing.T) {
	intType1 := FromReflectType(reflect.TypeFor[int]())
	intType2 := FromReflectType(reflect.TypeFor[int]())
	if intType1 != intType2 {
		t.Error("basic types should be equal")
	}

	slice1 := SliceOf(intType1)
	slice2 := SliceOf(intType2)
	if slice1 != slice2 {
		t.Error("identical slices should be equal")
	}

	ptr1 := PointerTo(slice1)
	ptr2 := PointerTo(slice2)
	if ptr1 != ptr2 {
		t.Error("identical pointers should be equal")
	}

	map1 := MapOf(intType1, FromReflectType(reflect.TypeFor[string]()))
	map2 := MapOf(intType2, FromReflectType(reflect.TypeFor[string]()))
	if map1 != map2 {
		t.Error("identical maps should be equal")
	}

	func1 := FuncOf([]*Type{intType1}, []*Type{intType1}, false)
	func2 := FuncOf([]*Type{intType2}, []*Type{intType2}, false)
	if func1 != func2 {
		t.Error("identical funcs should be equal")
	}

	struct1 := StructOf([]StructField{{Name: "A", Type: intType1}})
	struct2 := StructOf([]StructField{{Name: "A", Type: intType2}})
	if struct1 != struct2 {
		t.Error("identical structs should be equal")
	}
}