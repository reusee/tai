package flags

import (
	"fmt"
	"reflect"

	"github.com/reusee/dscope"
)

var flagType = reflect.TypeFor[Flag]()

func Parse(scope dscope.Scope, args []string) (dscope.Scope, error) {
	flagMap := make(map[string]Flag)
	for t := range scope.AllTypes() {
		if !t.Implements(flagType) {
			continue
		}
		flagValue, ok := scope.Get(t)
		if !ok {
			return dscope.Scope{}, fmt.Errorf("flag type not found in scope: %v", t)
		}
		flag := flagValue.Interface().(Flag)
		key := flag.Key()
		flagMap[key] = flag
	}

	for len(args) > 0 {
		key := args[0]
		flag, ok := flagMap[key]
		if !ok {
			return dscope.Scope{}, fmt.Errorf("unknown flag: %s", key)
		}
		newValue, remainArgs, err := flag.Handle(args[1:])
		if err != nil {
			return dscope.Scope{}, err
		}
		args = remainArgs
		scope = scope.Fork(
			reflect.ValueOf(newValue).Pointer(),
		)
	}

	return scope, nil
}
