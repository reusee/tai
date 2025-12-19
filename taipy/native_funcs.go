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
