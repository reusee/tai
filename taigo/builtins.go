package taigo

import (
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/reusee/tai/taivm"
)

func registerBuiltins(vm *taivm.VM, options *Options) {
	registerIO(vm, options)
	registerPanic(vm)
	registerCollections(vm)
	registerMath(vm)
	registerConversions(vm)
	registerMemory(vm)
}

func registerIO(vm *taivm.VM, options *Options) {
	fprint := func(w io.Writer, args []any, newline bool) {
		for i, arg := range args {
			if i > 0 {
				fmt.Fprint(w, " ")
			}
			fmt.Fprint(w, arg)
		}
		if newline {
			fmt.Fprintln(w)
		}
	}

	stdout := func() io.Writer {
		if options != nil && options.Stdout != nil {
			return options.Stdout
		}
		return os.Stdout
	}

	vm.Def("print", taivm.NativeFunc{
		Name: "print",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			fprint(stdout(), args, false)
			return nil, nil
		},
	})

	vm.Def("println", taivm.NativeFunc{
		Name: "println",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			fprint(stdout(), args, true)
			return nil, nil
		},
	})
}

func registerPanic(vm *taivm.VM) {
	vm.Def("panic", taivm.NativeFunc{
		Name: "panic",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			val := any(nil)
			if len(args) > 0 {
				val = args[0]
			}
			vm.IsPanicking = true
			vm.PanicValue = val
			vm.IP = len(vm.CurrentFun.Code)
			return nil, fmt.Errorf("panic")
		},
	})

	vm.Def("recover", taivm.NativeFunc{
		Name: "recover",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if vm.IsPanicking {
				v := vm.PanicValue
				vm.IsPanicking = false
				vm.PanicValue = nil
				return v, nil
			}
			return nil, nil
		},
	})
}

func registerCollections(vm *taivm.VM) {
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
			default:
				rv := reflect.ValueOf(v)
				kind := rv.Kind()
				if kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array || kind == reflect.String || kind == reflect.Chan {
					return rv.Len(), nil
				}
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
			default:
				rv := reflect.ValueOf(v)
				kind := rv.Kind()
				if kind == reflect.Slice || kind == reflect.Array || kind == reflect.Chan {
					return rv.Cap(), nil
				}
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
				if s, ok := args[1].(string); ok && len(args) == 2 {
					for _, b := range []byte(s) {
						list.Elements = append(list.Elements, int(b))
					}
				} else {
					list.Elements = append(list.Elements, args[1:]...)
				}
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
			dst := args[0]
			src := args[1]
			if dst == nil || src == nil {
				return 0, nil
			}

			if s, ok := src.(string); ok {
				switch d := dst.(type) {
				case *taivm.List:
					if d.Immutable {
						return nil, fmt.Errorf("copy destination is immutable")
					}
					return copyAnyString(d.Elements, s), nil
				case []any:
					return copyAnyString(d, s), nil
				default:
					rv := reflect.ValueOf(dst)
					if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
						return copy(rv.Bytes(), s), nil
					}
				}
			}

			var dstSlice, srcSlice []any
			switch v := dst.(type) {
			case *taivm.List:
				if v.Immutable {
					return nil, fmt.Errorf("copy destination is immutable")
				}
				dstSlice = v.Elements
			case []any:
				dstSlice = v
			default:
				rvDst := reflect.ValueOf(dst)
				rvSrc := reflect.ValueOf(src)
				if rvDst.Kind() == reflect.Slice && rvSrc.Kind() == reflect.Slice {
					return reflect.Copy(rvDst, rvSrc), nil
				}
				return nil, fmt.Errorf("copy expects list or slice, got %T", dst)
			}
			switch v := src.(type) {
			case *taivm.List:
				srcSlice = v.Elements
			case []any:
				srcSlice = v
			default:
				return nil, fmt.Errorf("copy source must be list or slice, got %T", src)
			}
			return copy(dstSlice, srcSlice), nil
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
				rv := reflect.ValueOf(target)
				if rv.Kind() == reflect.Map {
					rk := reflect.ValueOf(key)
					if !rk.IsValid() {
						rk = reflect.Zero(rv.Type().Key())
					}
					if rk.Type().AssignableTo(rv.Type().Key()) {
						rv.SetMapIndex(rk, reflect.Value{})
						return nil, nil
					}
				}
				return nil, fmt.Errorf("delete expects map, got %T", target)
			}
			return nil, nil
		},
	})

	vm.Def("clear", taivm.NativeFunc{
		Name: "clear",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("clear expects 1 argument")
			}
			arg := args[0]
			if arg == nil {
				return nil, nil
			}
			switch v := arg.(type) {
			case *taivm.List:
				for i := range v.Elements {
					v.Elements[i] = nil
				}
			case map[any]any:
				for k := range v {
					delete(v, k)
				}
			case map[string]any:
				for k := range v {
					delete(v, k)
				}
			default:
				rv := reflect.ValueOf(arg)
				if rv.Kind() == reflect.Map {
					for _, k := range rv.MapKeys() {
						rv.SetMapIndex(k, reflect.Value{})
					}
				} else if rv.Kind() == reflect.Slice {
					for i := 0; i < rv.Len(); i++ {
						rv.Index(i).Set(reflect.Zero(rv.Type().Elem()))
					}
				}
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
}

func registerMath(vm *taivm.VM) {
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

	vm.Def("min", taivm.NativeFunc{
		Name: "min",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("min expects at least 1 argument")
			}
			min := args[0]
			for _, v := range args[1:] {
				if isLess(v, min) {
					min = v
				}
			}
			return min, nil
		},
	})

	vm.Def("max", taivm.NativeFunc{
		Name: "max",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("max expects at least 1 argument")
			}
			max := args[0]
			for _, v := range args[1:] {
				if isLess(max, v) {
					max = v
				}
			}
			return max, nil
		},
	})
}

func isLess(a, b any) bool {
	if i1, ok1 := taivm.ToInt64(a); ok1 {
		if i2, ok2 := taivm.ToInt64(b); ok2 {
			return i1 < i2
		}
		if f2, ok2 := taivm.ToFloat64(b); ok2 {
			return float64(i1) < f2
		}
	}
	if f1, ok1 := taivm.ToFloat64(a); ok1 {
		if f2, ok2 := taivm.ToFloat64(b); ok2 {
			return f1 < f2
		}
		if i2, ok2 := taivm.ToInt64(b); ok2 {
			return f1 < float64(i2)
		}
	}
	if s1, ok1 := a.(string); ok1 {
		if s2, ok2 := b.(string); ok2 {
			return s1 < s2
		}
	}
	return false
}

func registerConversions(vm *taivm.VM) {
	vm.Def("int", taivm.NativeFunc{
		Name: "int",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("int expects 1 argument")
			}
			if i, ok := taivm.ToInt64(args[0]); ok {
				return int(i), nil
			}
			if f, ok := taivm.ToFloat64(args[0]); ok {
				return int(f), nil
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
			arg := args[0]
			switch v := arg.(type) {
			case *taivm.List:
				var buf []byte
				for _, e := range v.Elements {
					if i, ok := taivm.ToInt64(e); ok {
						buf = append(buf, byte(i))
					}
				}
				return string(buf), nil
			case []byte:
				return string(v), nil
			case []rune:
				return string(v), nil
			}
			if i, ok := taivm.ToInt64(arg); ok {
				return string(rune(i)), nil
			}
			return fmt.Sprint(arg), nil
		},
	})
}

func registerMemory(vm *taivm.VM) {
	vm.Def("make", taivm.NativeFunc{
		Name: "make",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("make expects type argument")
			}
			t, ok := args[0].(reflect.Type)
			if !ok {
				return nil, fmt.Errorf("make expects reflect.Type as first argument, got %T", args[0])
			}
			switch t.Kind() {
			case reflect.Slice:
				return makeSlice(args, t)
			case reflect.Map:
				size := 0
				if len(args) >= 2 {
					if s, ok := taivm.ToInt64(args[1]); ok {
						size = int(s)
					}
				}
				return reflect.MakeMapWithSize(t, size).Interface(), nil
			case reflect.Chan:
				return nil, fmt.Errorf("channels not supported")
			default:
				return nil, fmt.Errorf("cannot make type %v", t)
			}
		},
	})

	vm.Def("new", taivm.NativeFunc{
		Name: "new",
		Func: func(vm *taivm.VM, args []any) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("new expects type argument")
			}
			t, ok := args[0].(reflect.Type)
			if !ok {
				return nil, fmt.Errorf("new expects reflect.Type")
			}
			return &taivm.Pointer{
				Target: &taivm.List{
					Elements: []any{getZeroValue(t)},
				},
				Key: 0,
			}, nil
		},
	})
}

func makeSlice(args []any, t reflect.Type) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("make slice expects length argument")
	}
	l, ok := taivm.ToInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("slice length must be integer")
	}
	size := int(l)
	if size < 0 {
		return nil, fmt.Errorf("negative slice length")
	}
	capacity := size
	if len(args) >= 3 {
		c, ok := taivm.ToInt64(args[2])
		if !ok {
			return nil, fmt.Errorf("slice capacity must be integer")
		}
		capacity = int(c)
	}
	if capacity < size {
		return nil, fmt.Errorf("len larger than cap in make([]T)")
	}
	elements := make([]any, size, capacity)
	zero := getZeroValue(t.Elem())
	if zero != nil {
		for i := range elements {
			elements[i] = zero
		}
	}
	return &taivm.List{Elements: elements}, nil
}

func getZeroValue(t reflect.Type) any {
	if t == nil {
		return nil
	}
	return reflect.Zero(t).Interface()
}

func copyAnyString(dst []any, src string) int {
	n := len(src)
	if len(dst) < n {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		dst[i] = int(src[i])
	}
	return n
}
