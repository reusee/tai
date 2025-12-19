package taipy

import (
	"fmt"
	"maps"
	"math"

	"github.com/reusee/tai/taivm"
)

var Len = taivm.NativeFunc{
	Name: "len",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("len expects 1 argument")
		}
		switch v := args[0].(type) {
		case string:
			return int64(len([]rune(v))), nil
		case *taivm.List:
			return int64(len(v.Elements)), nil
		case []any:
			return int64(len(v)), nil
		case map[any]any:
			return int64(len(v)), nil
		case *taivm.Range:
			return v.Len(), nil
		default:
			return nil, fmt.Errorf("object of type %T has no len()", v)
		}
	},
}

var Range = taivm.NativeFunc{
	Name: "range",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		var start, stop, step int64
		step = 1

		switch len(args) {
		case 1:
			s, ok := taivm.ToInt64(args[0])
			if !ok {
				return nil, fmt.Errorf("range argument must be integer")
			}
			stop = s
		case 2:
			s1, ok1 := taivm.ToInt64(args[0])
			s2, ok2 := taivm.ToInt64(args[1])
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("range arguments must be integers")
			}
			start = s1
			stop = s2
		case 3:
			s1, ok1 := taivm.ToInt64(args[0])
			s2, ok2 := taivm.ToInt64(args[1])
			s3, ok3 := taivm.ToInt64(args[2])
			if !ok1 || !ok2 || !ok3 {
				return nil, fmt.Errorf("range arguments must be integers")
			}
			start = s1
			stop = s2
			step = s3
		default:
			return nil, fmt.Errorf("range expects 1 to 3 arguments")
		}

		if step == 0 {
			return nil, fmt.Errorf("range step cannot be zero")
		}

		// Validation to prevent infinite loops in VM due to integer overflow
		var count int64
		if step > 0 {
			if start < stop {
				count = (stop - start + step - 1) / step
			}
		} else {
			if start > stop {
				count = (start - stop - step - 1) / -step
			}
		}

		if count > 0 {
			last := start + (count-1)*step
			next := last + step
			// Check if 'next' wraps around and re-enters the loop condition
			if step > 0 {
				if next < last && next < stop {
					return nil, fmt.Errorf("range overflows")
				}
			} else {
				if next > last && next > stop {
					return nil, fmt.Errorf("range overflows")
				}
			}
		}

		return &taivm.Range{
			Start: start,
			Stop:  stop,
			Step:  step,
		}, nil
	},
}

var Print = taivm.NativeFunc{
	Name: "print",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		fmt.Println(args...)
		return nil, nil
	},
}

var Struct = taivm.NativeFunc{
	Name: "struct",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		fields := make(map[string]any)
		if len(args) > 0 {
			switch m := args[0].(type) {
			case map[any]any:
				for k, v := range m {
					if s, ok := k.(string); ok {
						fields[s] = v
					}
				}
			case map[string]any:
				maps.Copy(fields, m)
			default:
				return nil, fmt.Errorf("unknown struct argument type: %T", m)
			}
		}
		return &taivm.Struct{Fields: fields}, nil
	},
}

var Pow = taivm.NativeFunc{
	Name: "pow",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("pow expects 2 arguments")
		}
		a := args[0]
		b := args[1]

		if isFloat(a) || isFloat(b) {
			f1, ok1 := taivm.ToFloat64(a)
			f2, ok2 := taivm.ToFloat64(b)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("invalid arguments for pow: %T, %T", a, b)
			}
			return math.Pow(f1, f2), nil
		}

		i1, ok1 := taivm.ToInt64(a)
		i2, ok2 := taivm.ToInt64(b)
		if ok1 && ok2 {
			if i2 < 0 {
				return math.Pow(float64(i1), float64(i2)), nil
			}

			// Integer exponentiation
			base := i1
			exp := i2
			result := int64(1)
			for exp > 0 {
				if exp&1 == 1 {
					result *= base
				}
				base *= base
				exp >>= 1
			}
			return result, nil
		}

		return nil, fmt.Errorf("unsupported argument types for pow: %T, %T", a, b)
	},
}

func isFloat(v any) bool {
	switch v.(type) {
	case float32, float64:
		return true
	}
	return false
}
