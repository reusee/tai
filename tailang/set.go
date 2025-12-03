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

	val, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}

	// Update variable in the environment chain
	found := false
	e := env
	for e != nil {
		e.mu.Lock()
		if _, ok := e.Vars[name]; ok {
			e.Vars[name] = val
			e.mu.Unlock()
			found = true
			break
		}
		e.mu.Unlock()
		e = e.Parent
	}

	if !found {
		return nil, fmt.Errorf("variable not found: %s", name)
	}

	return val, nil
}
