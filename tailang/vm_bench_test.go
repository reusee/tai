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
			OpLoadVar, 0, 0, // i
			OpJumpFalse, 0, 18, // jump +18 if falsey (to 24)

			// 6: loop body
			OpLoadVar, 0, 2, // sub
			OpLoadVar, 0, 0, // i
			OpLoadConst, 0, 1, // 1
			OpCall, 0, 2, // sub(i, 1)
			OpSetVar, 0, 0, // i = result

			// 21: jump back
			OpJump, 0xff, 0xe8, // -24 (to 0)

			// 24: end
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
			OpLoadVar, 0, 0, // sub
			OpLoadVar, 0, 1, // n
			OpLoadConst, 0, 2, // 1
			OpCall, 0, 2, // sub(n, 1)
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
			OpLoadVar, 0, 0, // i
			OpJumpFalse, 0, 15, // jump +15 (to 21)

			// 6: body
			OpLoadVar, 0, 1, // dec
			OpLoadVar, 0, 0, // i
			OpCall, 0, 1, // dec(i)
			OpSetVar, 0, 0, // i = result

			// 18: jump back
			OpJump, 0xff, 0xeb, // -21 (to 0)

			// 21: end
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
