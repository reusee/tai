package tailang

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
			"i", 1, "sub",
		},
		Code: []OpCode{
			// 0: loop condition check
			OpLoadVar.With(0),   // i
			OpJumpFalse.With(6), // jump +6 (to 8)

			// 2: loop body
			OpLoadVar.With(2),   // sub
			OpLoadVar.With(0),   // i
			OpLoadConst.With(1), // 1
			OpCall.With(2),      // sub(i, 1)
			OpSetVar.With(0),    // i = result

			// 7: jump back
			OpJump.With(-7), // (to 0)

			// 8: end
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
		Constants:  []any{"sub", "n", 1},
		Code: []OpCode{
			OpLoadVar.With(0),   // sub
			OpLoadVar.With(1),   // n
			OpLoadConst.With(2), // 1
			OpCall.With(2),      // sub(n, 1)
			OpReturn,
		},
	}

	main := &Function{
		Name: "main",
		Constants: []any{
			"i", "dec",
		},
		Code: []OpCode{
			// 0: loop check
			OpLoadVar.With(0),   // i
			OpJumpFalse.With(5), // jump +5 (to 7)

			// 2: body
			OpLoadVar.With(1), // dec
			OpLoadVar.With(0), // i
			OpCall.With(1),    // dec(i)
			OpSetVar.With(0),  // i = result

			// 6: jump back
			OpJump.With(-6), // (to 0)

			// 7: end
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
