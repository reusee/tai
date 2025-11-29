package tailang

import "reflect"

type Function interface {
	FunctionName() string
	Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error)
}
