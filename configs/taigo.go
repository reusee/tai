package configs

import (
	"reflect"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/taigo"
	"github.com/reusee/tai/taivm"
)

func TaigoFork(scope dscope.Scope, env *taivm.Env) (ret dscope.Scope, err error) {
	var defs []any
	for t := range scope.AllTypes() {
		if !t.Implements(configurableType) {
			continue
		}
		v := reflect.New(t).Elem().Interface().(Configurable)
		expr := v.ConfigExpr()
		value, err := taigo.TypedEval(env, expr, t)
		if err != nil {
			continue
		}
		ptr := reflect.New(t)
		ptr.Elem().Set(reflect.ValueOf(value))
		defs = append(defs, ptr.Interface())
	}
	return scope.Fork(defs...), nil
}
