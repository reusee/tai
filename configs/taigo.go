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
			var t reflect.Type
			if v.Type != nil && v.Type.External != nil {
				t = v.Type.External
			} else {
				t = reflect.TypeOf(v.Val)
			}
			if !configTypes[t] || seen[t] {
				continue
			}
			val := v.Val
			rv := reflect.ValueOf(val)
			if rv.Type() != t {
				if rv.Type().ConvertibleTo(t) {
					val = rv.Convert(t).Interface()
				} else {
					continue
				}
			}
			ptr := reflect.New(t)
			ptr.Elem().Set(reflect.ValueOf(val))
			defs = append(defs, ptr.Interface())
			seen[t] = true
		}
	}
	return scope.Fork(defs...), nil
}
