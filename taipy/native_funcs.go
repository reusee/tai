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
		default:
			return nil, fmt.Errorf("object of type %T has no len()", v)
		}
	},
}
