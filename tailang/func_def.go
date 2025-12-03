package tailang

import (
	"fmt"
	"reflect"
)

type FuncDef struct{}

var _ Function = FuncDef{}

func (f FuncDef) FunctionName() string {
	return "func"
}

func (f FuncDef) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	// Name
	tok, err := stream.Current()
	if err != nil {
		return nil, err
	}

	var name string
	isAnonymous := false

	if tok.Text == "(" {
		isAnonymous = true
		name = "anonymous"
	} else {
		if tok.Kind != TokenIdentifier && tok.Kind != TokenUnquotedString {
			return nil, fmt.Errorf("expected func name")
		}
		name = tok.Text
		stream.Consume()

		if IsKeyword(name) {
			return nil, fmt.Errorf("cannot define keyword: %s", name)
		}

		env.mu.RLock()
		_, exists := env.Vars[name]
		env.mu.RUnlock()
		if exists {
			return nil, fmt.Errorf("variable %s already defined", name)
		}
	}

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
	seenParams := make(map[string]bool)
	for {
		tok, err = stream.Current()
		if err != nil {
			return nil, err
		}
		if tok.Text == ")" {
			stream.Consume()
			break
		}
		if tok.Kind != TokenIdentifier && tok.Kind != TokenUnquotedString {
			return nil, fmt.Errorf("expected param name")
		}
		if seenParams[tok.Text] {
			return nil, fmt.Errorf("duplicate parameter: %s", tok.Text)
		}
		seenParams[tok.Text] = true
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

	if !isAnonymous {
		env.Define(name, uf)
	}
	return uf, nil
}
