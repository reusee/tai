package apps

import "github.com/reusee/dscope"

type App[Main any] struct {
	Name    Name
	Modules []dscope.Module
	Defs    []any
}

func (a App[Main]) Run() {
	var defs []any
	for _, mod := range a.Modules {
		defs = append(defs, mod)
	}
	scope := dscope.New(defs...)
	scope = scope.Fork(&a.Name)
	scope = scope.Fork(a.Defs...)
	scope.Call(scope.Get[Main]())
}
