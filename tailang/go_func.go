package tailang

import "reflect"

type GoFunc struct {
	Name string
	Func any
}

var _ Function = GoFunc{}

func (g GoFunc) FunctionName() string {
	return g.Name
}

func (g GoFunc) Call(env *Env, stream TokenStream) (any, error) {
	return env.callFunc(stream, reflect.ValueOf(g.Func), g.Name)
}
