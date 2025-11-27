package tailang

import (
	"fmt"
)

type FuncDef struct{}

var _ Function = FuncDef{}

func (f FuncDef) FunctionName() string {
	return "func"
}

func (f FuncDef) Call(env *Env, stream TokenStream) (any, error) {
	// Name
	tok, err := stream.Current()
	if err != nil {
		return nil, err
	}
	if tok.Kind != TokenIdentifier {
		return nil, fmt.Errorf("expected func name")
	}
	name := tok.Text
	stream.Consume()

	// Params
	tok, err = stream.Current()
	if err != nil {
		return nil, err
	}
	if tok.Text != "(" {
		return nil, fmt.Errorf("expected ( for params")
	}
	stream.Consume()

	var params []string
	for {
		tok, err = stream.Current()
		if err != nil {
			return nil, err
		}
		if tok.Text == ")" {
			stream.Consume()
			break
		}
		if tok.Kind != TokenIdentifier {
			return nil, fmt.Errorf("expected param name")
		}
		params = append(params, tok.Text)
		stream.Consume()
	}

	// Body
	bodyVal, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	body, ok := bodyVal.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected block for func body, got %T", bodyVal)
	}

	uf := UserFunc{
		FuncName:      name,
		Params:        params,
		Body:          body,
		DefinitionEnv: env,
	}

	env.Define(name, uf)
	return uf, nil
}
