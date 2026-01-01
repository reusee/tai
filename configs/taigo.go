package configs

import (
	"reflect"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/taivm"
)

func TaigoFork(scope dscope.Scope, env *taivm.Env) (ret dscope.Scope, err error) {
	var defs []any
	configTypes := make(map[reflect.Type]bool)
	for t := range scope.AllTypes() {
		if t.Implements(configurableType) {
			configTypes[t] = true
		}
	}
	seen := make(map[reflect.Type]bool)
	for e := env; e != nil; e = e.Parent {
		// Traverse variables in reverse order to find the latest definition in the same environment
		for i := len(e.Vars) - 1; i >= 0; i-- {
			v := e.Vars[i]
			if v.Val == nil {
				continue
			}
			t := reflect.TypeOf(v.Val)
			if configTypes[t] && !seen[t] {
				defs = append(defs, v.Val)
				seen[t] = true
			}
		}
	}
	return scope.Fork(defs...), nil
}
