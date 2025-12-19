package taipy

import (
	"fmt"

	"github.com/reusee/tai/taivm"
)

var Concat = taivm.NativeFunc{
	Name: "concat",
	Func: func(vm *taivm.VM, args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("concat expects 2 arguments")
		}
		l1, ok1 := args[0].(*taivm.List)
		l2, ok2 := args[1].(*taivm.List)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("concat operands must be lists")
		}
		res := make([]any, 0, len(l1.Elements)+len(l2.Elements))
		res = append(res, l1.Elements...)
		res = append(res, l2.Elements...)
		return &taivm.List{
			Elements:  res,
			Immutable: l1.Immutable,
		}, nil
	},
}

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
		default:
			return nil, fmt.Errorf("object of type %T has no len()", v)
		}
	},
}
