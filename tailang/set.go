package tailang

import "fmt"

type Set struct{}

var _ Function = Set{}

func (s Set) FunctionName() string {
	return "set"
}

func (s Set) Call(env *Env, stream TokenStream) (any, error) {
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
		if _, ok := e.Vars[name]; ok {
			e.Vars[name] = val
			found = true
			break
		}
		e = e.Parent
	}

	if !found {
		return nil, fmt.Errorf("variable not found: %s", name)
	}

	return val, nil
}
