package tailang

import (
	"fmt"
	"reflect"
)

type Set struct{}

var _ Function = Set{}

func (s Set) FunctionName() string {
	return "set"
}

func (s Set) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	tok, err := stream.Current()
	if err != nil {
		return nil, err
	}
	if tok.Kind != TokenIdentifier && tok.Kind != TokenUnquotedString {
		return nil, fmt.Errorf("expected identifier")
	}
	name := tok.Text
	stream.Consume()

	// find old value to get type
	found := false
	e := env
	var targetType reflect.Type
	for e != nil {
		if v, ok := e.Vars[name]; ok {
			found = true
			targetType = reflect.TypeOf(v)
			break
		}
		e = e.Parent
	}

	if !found {
		return nil, fmt.Errorf("variable not found: %s", name)
	}

	val, err := env.evalExpr(stream, targetType)
	if err != nil {
		return nil, err
	}

	if targetType != nil {
		val, err = PrepareAssign(val, targetType)
		if err != nil {
			return nil, err
		}
	}
	e.Vars[name] = val

	return val, nil
}
