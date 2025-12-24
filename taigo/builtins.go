package taigo

import (
	"fmt"

	"github.com/reusee/tai/taivm"
)

func registerBuiltins(vm *taivm.VM) {

	vm.Def("print", taivm.NativeFunc{
		Name: "print",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			for i, arg := range args {
				if i > 0 {
					fmt.Print(" ")
				}
				fmt.Print(arg)
			}
			return nil, nil
		},
	})

	vm.Def("println", taivm.NativeFunc{
		Name: "println",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			for i, arg := range args {
				if i > 0 {
					fmt.Print(" ")
				}
				fmt.Print(arg)
			}
			fmt.Println()
			return nil, nil
		},
	})

	vm.Def("panic", taivm.NativeFunc{
		Name: "panic",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("panic")
			}
			return nil, fmt.Errorf("%v", args[0])
		},
	})

	vm.Def("len", taivm.NativeFunc{
		Name: "len",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("len expects 1 argument")
			}
			switch v := args[0].(type) {
			case string:
				return len(v), nil
			case *taivm.List:
				return len(v.Elements), nil
			case []any:
				return len(v), nil
			case map[any]any:
				return len(v), nil
			case map[string]any:
				return len(v), nil
			case *taivm.Range:
				return v.Len(), nil
			case nil:
				return 0, nil
			}
			return nil, fmt.Errorf("invalid argument type for len: %T", args[0])
		},
	})

	vm.Def("cap", taivm.NativeFunc{
		Name: "cap",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("cap expects 1 argument")
			}
			switch v := args[0].(type) {
			case *taivm.List:
				return cap(v.Elements), nil
			case []any:
				return cap(v), nil
			case nil:
				return 0, nil
			}
			return nil, fmt.Errorf("invalid argument type for cap: %T", args[0])
		},
	})

	vm.Def("append", taivm.NativeFunc{
		Name: "append",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("append expects at least 1 argument")
			}
			var list *taivm.List
			if args[0] == nil {
				list = &taivm.List{}
			} else {
				var ok bool
				list, ok = args[0].(*taivm.List)
				if !ok {
					return nil, fmt.Errorf("first argument to append must be list or nil")
				}
				if list.Immutable {
					return nil, fmt.Errorf("cannot append to immutable list")
				}
			}
			if len(args) > 1 {
				list.Elements = append(list.Elements, args[1:]...)
			}
			return list, nil
		},
	})

	vm.Def("copy", taivm.NativeFunc{
		Name: "copy",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("copy expects 2 arguments")
			}
			var dst []any
			switch v := args[0].(type) {
			case *taivm.List:
				if v.Immutable {
					return nil, fmt.Errorf("copy destination is immutable")
				}
				dst = v.Elements
			case []any:
				dst = v
			default:
				return nil, fmt.Errorf("copy expects list or slice as first argument, got %T", args[0])
			}

			var src []any
			switch v := args[1].(type) {
			case *taivm.List:
				src = v.Elements
			case []any:
				src = v
			default:
				return nil, fmt.Errorf("copy expects list or slice as second argument, got %T", args[1])
			}

			return copy(dst, src), nil
		},
	})

	vm.Def("delete", taivm.NativeFunc{
		Name: "delete",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("delete expects 2 arguments")
			}
			target := args[0]
			key := args[1]
			if target == nil {
				return nil, nil
			}
			switch m := target.(type) {
			case map[any]any:
				delete(m, key)
			case map[string]any:
				if k, ok := key.(string); ok {
					delete(m, k)
				}
			default:
				return nil, fmt.Errorf("delete expects map, got %T", target)
			}
			return nil, nil
		},
	})

	vm.Def("close", taivm.NativeFunc{
		Name: "close",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			return nil, fmt.Errorf("channels not supported")
		},
	})

	vm.Def("complex", taivm.NativeFunc{
		Name: "complex",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("complex expects 2 arguments")
			}
			r, ok1 := taivm.ToFloat64(args[0])
			i, ok2 := taivm.ToFloat64(args[1])
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("complex arguments must be numbers")
			}
			return complex(r, i), nil
		},
	})

	vm.Def("real", taivm.NativeFunc{
		Name: "real",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("real expects 1 argument")
			}
			switch c := args[0].(type) {
			case complex128:
				return real(c), nil
			case complex64:
				return float64(real(c)), nil
			}
			if f, ok := taivm.ToFloat64(args[0]); ok {
				return f, nil
			}
			return nil, fmt.Errorf("real expects numeric argument")
		},
	})

	vm.Def("imag", taivm.NativeFunc{
		Name: "imag",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("imag expects 1 argument")
			}
			switch c := args[0].(type) {
			case complex128:
				return imag(c), nil
			case complex64:
				return float64(imag(c)), nil
			}
			if _, ok := taivm.ToFloat64(args[0]); ok {
				return 0.0, nil
			}
			return nil, fmt.Errorf("imag expects numeric argument")
		},
	})

	vm.Def("int", taivm.NativeFunc{
		Name: "int",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("int expects 1 argument")
			}
			if i, ok := taivm.ToInt64(args[0]); ok {
				return i, nil
			}
			if f, ok := taivm.ToFloat64(args[0]); ok {
				return int64(f), nil
			}
			return nil, fmt.Errorf("cannot convert %T to int", args[0])
		},
	})

	vm.Def("float64", taivm.NativeFunc{
		Name: "float64",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("float64 expects 1 argument")
			}
			if f, ok := taivm.ToFloat64(args[0]); ok {
				return f, nil
			}
			return nil, fmt.Errorf("cannot convert %T to float64", args[0])
		},
	})

	vm.Def("bool", taivm.NativeFunc{
		Name: "bool",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("bool expects 1 argument")
			}
			val := args[0]
			if val == nil {
				return false, nil
			}
			switch v := val.(type) {
			case bool:
				return v, nil
			case string:
				return v != "", nil
			}
			if i, ok := taivm.ToInt64(val); ok {
				return i != 0, nil
			}
			if f, ok := taivm.ToFloat64(val); ok {
				return f != 0, nil
			}
			return true, nil
		},
	})

	vm.Def("string", taivm.NativeFunc{
		Name: "string",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("string expects 1 argument")
			}
			return fmt.Sprint(args[0]), nil
		},
	})

}
