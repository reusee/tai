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

	var targetEnv *Env
	var exprType reflect.Type

	e := env
	for e != nil {
		if val, ok := e.Vars[name]; ok {
			targetEnv = e
			if val != nil {
				exprType = reflect.TypeOf(val)
			}
			break
		}
		e = e.Parent
	}

	if targetEnv == nil {
		return nil, fmt.Errorf("variable not found: %s", name)
	}

	val, err := env.evalExpr(stream, exprType)
	if err != nil {
		return nil, err
	}

	if exprType != nil {
		valV, err := PrepareAssign(val, exprType)
		if err != nil {
			return nil, fmt.Errorf("cannot assign to variable %s: %w", name, err)
		}
		val = valV.Interface()
	}

	targetEnv.Vars[name] = val

	return val, nil
}
