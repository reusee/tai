package taivm

import "testing"

func BenchmarkVM_NativeCall(b *testing.B) {
	sub := NativeFunc{
		Name: "sub",
		Func: func(_ *VM, args []any) (any, error) {
			return args[0].(int) - args[1].(int), nil
		},
	}

	main := &Function{
		Name: "main",
		Constants: []any{
			"sub", "i", 1,
		},
		Code: []OpCode{
			// Setup locals
			OpLoadVar.With(0), // sub (Local 0)
			OpLoadVar.With(1), // i (Local 1)

			// 2: loop start
			OpGetLocal.With(1),  // i
			OpJumpFalse.With(6), // jump to 10

			// 4: body
			OpGetLocal.With(0),  // sub
			OpGetLocal.With(1),  // i
			OpLoadConst.With(2), // 1
			OpCall.With(2),      // sub(i, 1)
			OpSetLocal.With(1),  // i = result

			// 9: jump back
			OpJump.With(-8), // to 2

			// 10: end
			OpReturn,
		},
	}

	vm := NewVM(main)
	vm.Def("sub", sub)
	vm.Def("i", b.N)

	b.ResetTimer()
	for _, err := range vm.Run {
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVM_ClosureCall(b *testing.B) {
	sub := NativeFunc{
		Name: "sub",
		Func: func(_ *VM, args []any) (any, error) {
			return args[0].(int) - args[1].(int), nil
		},
	}

	dec := &Function{
		Name:       "dec",
		NumParams:  1,
		ParamNames: []string{"n"},
		Constants:  []any{"sub", 1},
		Code: []OpCode{
			OpLoadVar.With(0),   // sub
			OpGetLocal.With(0),  // n (argument 0)
			OpLoadConst.With(1), // 1
			OpCall.With(2),      // sub(n, 1)
			OpReturn,
		},
	}

	main := &Function{
		Name: "main",
		Constants: []any{
			"dec", "i",
		},
		Code: []OpCode{
			// Setup locals
			OpLoadVar.With(0), // dec (Local 0)
			OpLoadVar.With(1), // i (Local 1)

			// 2: loop start
			OpGetLocal.With(1),  // i
			OpJumpFalse.With(5), // jump to 9

			// 4: body
			OpGetLocal.With(0), // dec
			OpGetLocal.With(1), // i
			OpCall.With(1),     // dec(i)
			OpSetLocal.With(1), // i = result

			// 8: jump back
			OpJump.With(-7), // to 2

			// 9: end
			OpReturn,
		},
	}

	vm := NewVM(main)
	vm.Def("sub", sub)
	vm.Def("dec", &Closure{
		Fun: dec,
		Env: vm.Scope,
	})
	vm.Def("i", b.N)

	b.ResetTimer()
	for _, err := range vm.Run {
		if err != nil {
			b.Fatal(err)
		}
	}
}
