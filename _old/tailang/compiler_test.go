package tailang

import (
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
)

func TestCompiler(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		verify func(*taivm.VM)
	}{

		{
			name: "Def and Math",
			input: `
				Def a 10 
				Def b 20 
				Def c (Add a b)
				`,
			verify: func(vm *taivm.VM) {
				val, ok := vm.Get("c")
				if !ok {
					t.Error("c not defined")
				}
				if val.(int) != 30 {
					t.Errorf("expected 30, got %v", val)
				}
			},
		},

		{
			name:  "Pipe",
			input: `Def val (10 | Add 5)`,
			verify: func(vm *taivm.VM) {
				val, ok := vm.Get("val")
				if !ok || val.(int) != 15 {
					t.Errorf("expected val=15, got %v", val)
				}
			},
		},

		{
			name: "Control Flow",
			input: `
				Def res 0
				If 1 {
					Set res 10
				} Else {
					Set res 20
				}
			`,
			verify: func(vm *taivm.VM) {
				val, ok := vm.Get("res")
				if !ok || val.(int) != 10 {
					t.Errorf("expected 10, got %v", val)
				}
			},
		},

		{
			name:  "List",
			input: `Def l ([1 2 3] | Len)`,
			verify: func(vm *taivm.VM) {
				val, ok := vm.Get("l")
				if !ok || val.(int) != 3 {
					t.Errorf("expected 3, got %v", val)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fun, err := Compile("test", NewTokenizer(strings.NewReader(tt.input)))
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}

			vm := taivm.NewVM(fun)

			// Defines logic
			vm.Def("Add", taivm.NativeFunc{Name: "Add", Func: func(v *taivm.VM, args []any) (any, error) {
				return args[0].(int) + args[1].(int), nil
			}})
			vm.Def("Len", taivm.NativeFunc{Name: "Len", Func: func(v *taivm.VM, args []any) (any, error) {
				l := args[0].([]any)
				return int(len(l)), nil
			}})

			for range vm.Run {
			}
			tt.verify(vm)
		})
	}
}
