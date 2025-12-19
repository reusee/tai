package taipy

import (
	"fmt"

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
